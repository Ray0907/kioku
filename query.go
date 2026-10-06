package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var termsRE = regexp.MustCompile(`(-?)"([^"]+)"|(-?)(\S+)`)

type term struct {
	Word     string
	Phrase   bool
	Negative bool
}

func terms(q string) []term {
	var out []term
	for _, m := range termsRE.FindAllStringSubmatch(q, -1) {
		t := m[2] + m[4]
		neg := m[1]+m[3] == "-" && !strings.HasPrefix(t, "-")
		if m[1]+m[3] == "-" && !neg {
			t = "-" + t
		}
		out = append(out, term{t, m[2] != "", neg})
	}
	return out
}
func toFTS(q string) string {
	var pos, neg []string
	for _, t := range terms(q) {
		w := `"` + strings.ReplaceAll(t.Word, `"`, ` `) + `"`
		if !t.Phrase {
			w += "*"
		}
		if t.Negative {
			neg = append(neg, w)
		} else {
			pos = append(pos, w)
		}
	}
	if len(pos) == 0 {
		return ""
	}
	out := strings.Join(pos, " AND ")
	for _, n := range neg {
		out += " NOT " + n
	}
	return out
}

type hit struct {
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
	Project   string `json:"project"`
	CWD       string `json:"cwd"`
	TS        string `json:"ts"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Snippet   string `json:"snippet"`
	ResumeCmd string `json:"resume_cmd"`
	Path      string `json:"path"`
	UID       string `json:"-"`
	Index     int    `json:"-"`
	ID        int64  `json:"-"`
}

// Structured arguments let native clients launch without parsing shell text.
func resumeArgv(h, id, path, cwd string) []string {
	switch h {
	case "claude":
		return []string{"claude", "--resume", id}
	case "codex":
		return []string{"codex", "resume", id}
	case "opencode":
		return []string{"opencode", "--session", id}
	case "cursor":
		return []string{"cursor-agent", "--resume", id}
	case "grok":
		return []string{"cd", cwd}
	default:
		return []string{"pi", "--session", path}
	}
}
func resumeCmd(h, id, path, cwd string) string {
	args := resumeArgv(h, id, path, cwd)
	for i, arg := range args {
		if arg == "" || strings.IndexFunc(arg, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:@%+=,-", r))
		}) >= 0 {
			args[i] = shellQuote(arg)
		}
	}
	return strings.Join(args, " ")
}
func shortLatin(q string) bool {
	found := false
	for _, t := range terms(q) {
		if t.Negative {
			continue
		}
		if found || t.Phrase || len(t.Word) < 1 || len(t.Word) > 2 {
			return false
		}
		for _, r := range t.Word {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
		}
		found = true
	}
	return found
}

// At 90% coverage every token's FTS5 BM25 IDF is clamped near zero.
// Check the full MATCH too: frequent individual words can form a rare phrase.
func commonQuery(ctx context.Context, db *sql.DB, q, match string) (bool, error) {
	words := map[string]bool{}
	needsCount, positives := false, 0
	for _, t := range terms(q) {
		if t.Negative {
			needsCount = true
			continue
		}
		positives++
		parts := strings.Fields(t.Word)
		if positives > 1 || len(parts) > 1 {
			needsCount = true
		}
		for _, word := range parts {
			for _, r := range word {
				if r < 'a' || r > 'z' {
					if r < 'A' || r > 'Z' {
						return false, nil
					}
				}
			}
			words[strings.ToLower(word)] = true
		}
	}
	if len(words) == 0 {
		return false, nil
	}
	var total int64
	if e := db.QueryRowContext(ctx, `SELECT count(*) FROM messages`).Scan(&total); e != nil {
		return false, e
	}
	if total < 300 {
		return false, nil
	}
	for word := range words {
		var docs int64
		e := db.QueryRowContext(ctx, `SELECT doc FROM messages_vocab WHERE term=?`, word).Scan(&docs)
		if e == sql.ErrNoRows {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		if docs*10 < total*9 {
			return false, nil
		}
	}
	if !needsCount {
		return true, nil
	} // One term's prefix includes its frequent exact token.
	var matched int64
	if e := db.QueryRowContext(ctx, `SELECT count(*) FROM messages_fts WHERE messages_fts MATCH ?`, match).Scan(&matched); e != nil {
		return false, e
	}
	return matched*10 >= total*9, nil
}

func matchingProjects(ctx context.Context, db *sql.DB, filters []string) ([]string, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	rows, e := db.QueryContext(ctx, `SELECT DISTINCT project FROM sessions`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			return nil, e
		}
		for _, filter := range filters {
			if strings.EqualFold(name, filter) {
				names = append(names, name)
				break
			}
		}
	}
	return names, rows.Err()
}
func projectPredicate(filters, names []string) (string, []any) {
	if len(filters) == 0 {
		return "", nil
	}
	if len(names) == 0 {
		return `AND 0 `, nil
	}
	values := make([]any, len(names))
	for i, name := range names {
		values[i] = name
	}
	return `AND s.project IN (` + strings.TrimSuffix(strings.Repeat("?,", len(names)), ",") + `) `, values
}

func selfFilter(includeSelf bool) string {
	if includeSelf {
		return ""
	}
	return `AND m.self=0 `
}

// Pick by timestamp, then verify exactly against FTS in rowid ranges. Rowids
// are only seeks into FTS, never a recency limit or a ranking signal.
func newestMatches(ctx context.Context, db *sql.DB, match, harness string, projects, names []string, tool, includeSelf bool, want int) ([]int64, error) {
	limit := 400
	for {
		// ponytail: the ts index scans past the other role; add partial role/ts indexes only if tool-heavy corpora make this slow.
		query := `SELECT m.id FROM messages m INDEXED BY messages_ts `
		args := []any{}
		if len(projects) > 0 {
			query = `SELECT m.id FROM sessions s CROSS JOIN messages m INDEXED BY messages_session ON m.session_uid=s.uid WHERE `
		} else if harness != "" && harness != "all" {
			query += `CROSS JOIN sessions s ON s.uid=m.session_uid WHERE `
		} else {
			query += `WHERE `
		}
		if harness != "" && harness != "all" {
			query += `s.harness=? AND `
			args = append(args, harness)
		}
		if tool {
			query += `m.role='tool' `
		} else {
			query += `m.role!='tool' `
		}
		clause, values := projectPredicate(projects, names)
		query += clause + selfFilter(includeSelf) + `ORDER BY m.ts DESC,m.id DESC LIMIT ?`
		args = append(args, values...)
		args = append(args, limit)
		rows, e := db.QueryContext(ctx, query, args...)
		if e != nil {
			return nil, e
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				break
			}
			ids = append(ids, id)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(ids) == 0 {
			return ids, nil
		}

		sorted := append([]int64(nil), ids...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		type gap struct {
			at   int
			size int64
		}
		// Split at the 15 largest gaps: at most 16 FTS seeks, minimal rowid span.
		gaps := []gap{}
		for i := 1; i < len(sorted); i++ {
			if d := sorted[i] - sorted[i-1]; d > 1 {
				gaps = append(gaps, gap{i, d})
			}
		}
		sort.Slice(gaps, func(i, j int) bool { return gaps[i].size > gaps[j].size })
		cuts := map[int]bool{}
		for _, g := range gaps[:min(15, len(gaps))] {
			cuts[g.at] = true
		}
		candidate := make(map[int64]bool, len(ids))
		for _, id := range ids {
			candidate[id] = true
		}
		matched := map[int64]bool{}
		lo := sorted[0]
		for i := 1; i <= len(sorted); i++ {
			if i < len(sorted) && !cuts[i] {
				continue
			}
			hits, e := db.QueryContext(ctx, `SELECT rowid FROM messages_fts WHERE messages_fts MATCH ? AND rowid BETWEEN ? AND ?`, match, lo, sorted[i-1])
			if e != nil {
				return nil, e
			}
			for hits.Next() {
				var id int64
				if e = hits.Scan(&id); e != nil {
					break
				}
				if candidate[id] {
					matched[id] = true
				}
			}
			if e == nil {
				e = hits.Err()
			}
			hits.Close()
			if e != nil {
				return nil, e
			}
			if i < len(sorted) {
				lo = sorted[i]
			}
		}
		selected := []int64{}
		for _, id := range ids {
			if matched[id] {
				selected = append(selected, id)
				if len(selected) == want {
					break
				}
			}
		}
		if len(selected) == want || len(ids) < limit {
			return selected, nil
		}
		limit *= 2
	}
}

func search(ctx context.Context, db *sql.DB, q, harness string, projects []string, limit int, allTime, includeSelf bool) ([]hit, error) {
	match := toFTS(q)
	names, err := matchingProjects(ctx, db, projects)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	var sqlq string
	args := []any{}
	out := []hit{}
	fetch := func(query string, values ...any) error {
		rows, e := db.QueryContext(ctx, query, values...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x hit
			if e = rows.Scan(&x.UID, &x.Harness, &x.SessionID, &x.Project, &x.CWD, &x.Path, &x.ID, &x.Index, &x.TS, &x.Role, &x.Text); e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	}
	common := false
	if match != "" && !shortLatin(q) {
		var e error
		common, e = commonQuery(ctx, db, q, match)
		if e != nil {
			return nil, e
		}
	}
	if match == "" {
		sqlq = `SELECT s.uid,s.harness,s.native_id,s.project,s.cwd,s.path,m.id,m.idx,m.ts,m.role,m.text FROM sessions s JOIN messages m ON s.uid=m.session_uid WHERE m.idx=(SELECT max(idx) FROM messages m WHERE session_uid=s.uid ` + selfFilter(includeSelf) + `) `
		if harness != "" && harness != "all" {
			sqlq += `AND s.harness=? `
			args = append(args, harness)
		}
		clause, values := projectPredicate(projects, names)
		sqlq += clause
		args = append(args, values...)
		sqlq += `ORDER BY m.ts DESC,m.id DESC LIMIT ?`
		args = append(args, limit)
	} else if common {
		ids, e := newestMatches(ctx, db, match, harness, projects, names, false, includeSelf, limit)
		if e != nil {
			return nil, e
		}
		if len(ids) < limit {
			tools, err := newestMatches(ctx, db, match, harness, projects, names, true, includeSelf, limit-len(ids))
			if err != nil {
				return nil, err
			}
			ids = append(ids, tools...)
		}
		if len(ids) == 0 {
			timing("query", started)
			return []hit{}, nil
		}
		sqlq = `SELECT s.uid,s.harness,s.native_id,s.project,s.cwd,s.path,m.id,m.idx,m.ts,m.role,m.text FROM messages m JOIN sessions s ON s.uid=m.session_uid WHERE m.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) ORDER BY (m.role='tool'),m.ts DESC,m.id DESC`
		for _, id := range ids {
			args = append(args, id)
		}
	} else {
		sqlq = `WITH ranked AS MATERIALIZED (SELECT m.id,m.ts,bm25(messages_fts) score FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid `
		if harness != "" && harness != "all" || len(projects) > 0 {
			sqlq += `JOIN sessions s ON s.uid=m.session_uid `
		}
		sqlq += `WHERE messages_fts MATCH ? ` + selfFilter(includeSelf)
		args = append(args, match)
		if shortLatin(q) && !allTime {
			var latest string
			if e := db.QueryRowContext(ctx, `SELECT coalesce(max(m.ts),'') FROM messages m WHERE 1 `+selfFilter(includeSelf)).Scan(&latest); e != nil {
				return nil, e
			}
			if t, e := time.Parse(time.RFC3339Nano, latest); e == nil {
				sqlq += `AND m.ts>=? `
				args = append(args, t.AddDate(0, 0, -7).UTC().Format(time.RFC3339Nano))
			}
		}
		if harness != "" && harness != "all" {
			sqlq += `AND s.harness=? `
			args = append(args, harness)
		}
		clause, values := projectPredicate(projects, names)
		sqlq += clause
		args = append(args, values...)
	}
	var e error
	if match != "" && !common {
		query := func(role string) string {
			return sqlq + `AND m.role` + role + ` ORDER BY score,m.ts DESC,m.id DESC LIMIT ?) SELECT s.uid,s.harness,s.native_id,s.project,s.cwd,s.path,m.id,m.idx,m.ts,m.role,m.text FROM ranked r JOIN messages m ON m.id=r.id JOIN sessions s ON s.uid=m.session_uid ORDER BY r.score,r.ts DESC,r.id DESC`
		}
		e = fetch(query(`!='tool'`), append(args, limit)...)
		if e == nil && len(out) < limit {
			e = fetch(query(`='tool'`), append(args, limit-len(out))...)
		}
	} else {
		e = fetch(sqlq, args...)
	}
	timing("query", started)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	started = time.Now()
	ts := terms(q)
	for i := range out {
		if i%16 == 0 {
			if e = ctx.Err(); e != nil {
				return nil, e
			}
		}
		out[i].Snippet = snippet(out[i].Text, ts)
		out[i].ResumeCmd = resumeCmd(out[i].Harness, out[i].SessionID, out[i].Path, out[i].CWD)
	}
	timing("snippet", started)
	return out, nil
}

func transcript(ctx context.Context, db *sql.DB, uid string) ([]message, error) {
	rows, e := db.QueryContext(ctx, "SELECT id,idx,ts,role,text FROM messages WHERE session_uid=? ORDER BY idx", uid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []message{}
	for rows.Next() {
		var m message
		if e = rows.Scan(&m.ID, &m.Index, &m.TS, &m.Role, &m.Text); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func snippet(text string, ts []term) string {
	positive := false
	for _, t := range ts {
		if !t.Negative {
			positive = true
			break
		}
	}
	if !positive {
		return displayInline(text)
	}
	// Only the excerpt is displayed; keep the full message in hit.Text.
	if strings.IndexByte(text, '\x1b') < 0 {
		for _, t := range ts {
			if t.Negative {
				continue
			}
			word := strings.ToLower(t.Word)
			at := strings.Index(strings.ToLower(text[:min(len(text), 4096)]), word)
			if at < 0 {
				at = strings.Index(strings.ToLower(text), word)
			}
			if at < 0 || at >= len(text) {
				continue
			}
			from, end := max(0, at-256), min(len(text), at+2048)
			for from > 0 && !utf8.RuneStart(text[from]) {
				from--
			}
			for end < len(text) && !utf8.RuneStart(text[end]) {
				end--
			}
			part := displayInline(text[from:end])
			if pos := strings.Index(strings.ToLower(part), word); pos >= 0 {
				s := snippetAt(part, pos)
				if from > 0 && !strings.HasPrefix(s, "…") {
					s = "…" + s
				}
				return clip(s, 320)
			}
		}
	}
	text = displayInline(text)
	for _, t := range ts {
		if t.Negative {
			continue
		}
		if at := strings.Index(strings.ToLower(text), strings.ToLower(t.Word)); at >= 0 {
			return clip(snippetAt(text, at), 320)
		}
		r := []rune(text)
		if found := positions(r, t.Word); len(found) > 0 {
			return clip(snippetAt(text, len(string(r[:found[0]]))), 320)
		}
	}
	return clip(text, 320)
}
func snippetAt(text string, at int) string {
	start := at
	for cells := 0; start > 0 && cells < 12; {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		start -= n
		cells += displayWidth(string(r))
	}
	if start > 0 && start < len(text) {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		cur, _ := utf8.DecodeRuneInString(text[start:])
		if unicode.IsLetter(prev) && !isCJK(cur) {
			for start > 0 {
				r, n := utf8.DecodeLastRuneInString(text[:start])
				if unicode.IsSpace(r) || isCJK(r) {
					break
				}
				start -= n
			}
		}
	}
	if start > 0 {
		return "…" + text[start:]
	}
	return text
}
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
func jsonLines(rows []hit) error {
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if e := enc.Encode(r); e != nil {
			return e
		}
	}
	return nil
}

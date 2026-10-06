package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

type pageKey struct {
	Mode, Query, Harness, Ref                  string
	Projects                                   []string
	Limit, Context                             int
	JSON, AllTime, All, Full, NoMouse, Rebuild bool
	IncludeSelf                                bool `json:",omitempty"`
}

func (k pageKey) fingerprint() string {
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}
func (k pageKey) cursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("1:" + k.fingerprint() + ":" + strconv.Itoa(offset)))
}
func (k pageKey) offset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	if len(token) > 128 {
		return 0, fmt.Errorf("invalid cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	parts := strings.Split(string(b), ":")
	if len(parts) != 3 || parts[0] != "1" {
		return 0, fmt.Errorf("invalid cursor")
	}
	if parts[1] != k.fingerprint() {
		return 0, fmt.Errorf("cursor does not match this query or flags")
	}
	n, e := strconv.Atoi(parts[2])
	if e != nil || n < 1 || n > int(^uint(0)>>1)-k.Limit {
		return 0, fmt.Errorf("invalid cursor offset")
	}
	return n, nil
}
func nextCursor(k pageKey, offset, shown, total int) string {
	if offset+shown < total && shown > 0 {
		return k.cursor(offset + shown)
	}
	return ""
}
func writeJSON(v any) error {
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
func shortID(uid string) string {
	// Native IDs can repeat across files; the source-qualified UID cannot.
	sum := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(sum[:6])
}
func compactSnippet(text, q string) string {
	ts := terms(q)
	s := snippet(text, ts)
	for _, t := range ts {
		if t.Negative {
			continue
		}
		at := strings.Index(strings.ToLower(s), strings.ToLower(t.Word))
		if at < 0 || displayWidth(s[:at]) <= 24 {
			continue
		}
		start := at
		for cells := 0; start > 0 && cells < 12; {
			r, size := utf8.DecodeLastRuneInString(s[:start])
			start -= size
			cells += displayWidth(string(r))
		}
		s = "…" + s[start:]
		break
	}
	return ansi.Truncate(s, 160, "…")
}

// UTF-16 ranges use the TUI's existing display matcher. Native clients only
// paint these ranges; all query interpretation remains in the Go engine.
type highlightRange struct {
	Location int `json:"location"`
	Length   int `json:"length"`
}

func highlightRanges(text, query string) []highlightRange {
	runes := []rune(text)
	offsets := make([]int, len(runes)+1)
	for i, r := range runes {
		offsets[i+1] = offsets[i] + len(utf16.Encode([]rune{r}))
	}
	var out []highlightRange
	for _, t := range terms(query) {
		if t.Negative {
			continue
		}
		length := len([]rune(t.Word))
		for _, at := range positions(runes, t.Word) {
			out = append(out, highlightRange{offsets[at], offsets[at+length] - offsets[at]})
		}
	}
	return out
}

type compactHit struct {
	Ref        string           `json:"ref"`
	Harness    string           `json:"harness"`
	Project    string           `json:"project"`
	Age        string           `json:"age"`
	Role       string           `json:"role"`
	Snippet    string           `json:"snippet"`
	Highlights []highlightRange `json:"highlights,omitempty"`
}
type hitPage struct {
	Shown         int          `json:"shown"`
	Total         int          `json:"total"`
	TotalSessions int          `json:"total_sessions"`
	OmittedOlder  int          `json:"omitted_older,omitempty"`
	Hits          []compactHit `json:"hits"`
	NextCursor    string       `json:"next_cursor,omitempty"`
}
type roleCounts struct {
	You  int `json:"you"`
	Asst int `json:"asst"`
	Tool int `json:"tool"`
}
type compactSession struct {
	Ref        string           `json:"ref"`
	Harness    string           `json:"harness"`
	Project    string           `json:"project"`
	Age        string           `json:"age"`
	Hits       int              `json:"hits"`
	Roles      roleCounts       `json:"roles"`
	BestRef    string           `json:"best_ref"`
	Best       string           `json:"best"`
	Topic      string           `json:"topic"`
	Highlights []highlightRange `json:"highlights,omitempty"`
}
type sessionPage struct {
	Shown        int              `json:"shown"`
	Total        int              `json:"total"`
	OmittedOlder int              `json:"omitted_older,omitempty"`
	Sessions     []compactSession `json:"sessions"`
	NextCursor   string           `json:"next_cursor,omitempty"`
}

func shellQuote(s string) string {
	if strings.ContainsAny(s, "$`\\\"!") || strings.IndexFunc(s, func(r rune) bool { return !strconv.IsPrint(r) }) >= 0 {
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	return strconv.Quote(s)
}
func renderHits(p hitPage, asJSON bool, q string) error {
	if asJSON {
		return writeJSON(p)
	}
	fmt.Fprintf(output, "%d/%d hits in %d sessions\n", p.Shown, p.Total, p.TotalSessions)
	for _, h := range p.Hits {
		fmt.Fprintf(output, "%s  %s %s %s  %s: %s\n", h.Ref, h.Harness, h.Project, h.Age, h.Role, h.Snippet)
	}
	if p.NextCursor != "" {
		fmt.Fprintln(output, "cursor: "+p.NextCursor)
	}
	if len(p.Hits) > 0 {
		fmt.Fprintf(output, "expand: kioku show %s --query %s\n", p.Hits[0].Ref, shellQuote(q))
	}
	if p.OmittedOlder > 0 {
		fmt.Fprintf(output, "%d older matches omitted (--all-time)\n", p.OmittedOlder)
	}
	return nil
}
func renderSessions(p sessionPage, asJSON bool) error {
	if asJSON {
		return writeJSON(p)
	}
	fmt.Fprintf(output, "%d/%d sessions\n", p.Shown, p.Total)
	for _, s := range p.Sessions {
		fmt.Fprintf(output, "%s  %s %s %s  %d hits (you %d · asst %d · tool %d)  best %s: %s\n    topic: %s\n", s.Ref, s.Harness, s.Project, s.Age, s.Hits, s.Roles.You, s.Roles.Asst, s.Roles.Tool, s.BestRef, s.Best, s.Topic)
	}
	if p.NextCursor != "" {
		fmt.Fprintln(output, "cursor: "+p.NextCursor)
	}
	fmt.Fprintln(output, "expand: kioku show <session-id>")
	if p.OmittedOlder > 0 {
		fmt.Fprintf(output, "%d older matches omitted (--all-time)\n", p.OmittedOlder)
	}
	return nil
}

// The same FTS predicate (including the short-prefix date bound) feeds counts and pages.
func matchSource(ctx context.Context, db *sql.DB, q, harness string, projects, names []string, allTime, includeSelf bool) (string, []any, int, error) {
	from := `FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid `
	if harness != "" && harness != "all" || len(projects) > 0 {
		from += `JOIN sessions s ON s.uid=m.session_uid `
	}
	from += `WHERE messages_fts MATCH ? ` + selfFilter(includeSelf)
	args := []any{toFTS(q)}
	if harness != "" && harness != "all" {
		from += `AND s.harness=? `
		args = append(args, harness)
	}
	clause, values := projectPredicate(projects, names)
	from += clause
	args = append(args, values...)
	omitted := 0
	if shortLatin(q) && !allTime {
		var latest string
		if e := db.QueryRowContext(ctx, `SELECT coalesce(max(m.ts),'') FROM messages m WHERE 1 `+selfFilter(includeSelf)).Scan(&latest); e != nil {
			return "", nil, 0, e
		}
		if t, e := time.Parse(time.RFC3339Nano, latest); e == nil {
			cutoff := t.AddDate(0, 0, -7).UTC().Format(time.RFC3339Nano)
			if e := db.QueryRowContext(ctx, `SELECT count(*) `+from+`AND m.ts<?`, append(args, cutoff)...).Scan(&omitted); e != nil {
				return "", nil, 0, e
			}
			from += `AND m.ts>=? `
			args = append(args, cutoff)
		}
	}
	return from, args, omitted, nil
}
func fetchPageHits(ctx context.Context, db *sql.DB, query string, args ...any) ([]hit, error) {
	rows, e := db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []hit{}
	for rows.Next() {
		var h hit
		if e = rows.Scan(&h.UID, &h.Harness, &h.SessionID, &h.Project, &h.CWD, &h.Path, &h.ID, &h.Index, &h.TS, &h.Role, &h.Text); e != nil {
			return nil, e
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

const hitColumns = `s.uid,s.harness,s.native_id,s.project,s.cwd,s.path,m.id,m.idx,m.ts,m.role,m.text`

func compactSearch(ctx context.Context, db *sql.DB, k pageKey, offset int) (hitPage, error) {
	q, harness, limit := k.Query, k.Harness, k.Limit
	p := hitPage{Hits: []compactHit{}}
	names, err := matchingProjects(ctx, db, k.Projects)
	if err != nil {
		return p, err
	}
	var matches []hit
	if toFTS(q) == "" {
		from := `FROM sessions s JOIN messages m ON s.uid=m.session_uid WHERE m.idx=(SELECT max(idx) FROM messages m WHERE session_uid=s.uid ` + selfFilter(k.IncludeSelf) + `) `
		args := []any{}
		if harness != "" && harness != "all" {
			from += `AND s.harness=? `
			args = append(args, harness)
		}
		clause, values := projectPredicate(k.Projects, names)
		from += clause
		args = append(args, values...)
		if e := db.QueryRowContext(ctx, `SELECT count(*) `+from, args...).Scan(&p.Total); e != nil {
			return p, e
		}
		p.TotalSessions = p.Total
		if offset > p.Total {
			return p, fmt.Errorf("cursor past end of results")
		}
		if offset < p.Total {
			var e error
			matches, e = fetchPageHits(ctx, db, `SELECT `+hitColumns+` `+from+`ORDER BY m.ts DESC,m.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
			if e != nil {
				return p, e
			}
		}
	} else {
		from, args, omitted, e := matchSource(ctx, db, q, harness, k.Projects, names, k.AllTime, k.IncludeSelf)
		if e != nil {
			return p, e
		}
		p.OmittedOlder = omitted
		var conversations int
		if e = db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(m.role!='tool'),0),count(DISTINCT m.session_uid) `+from, args...).Scan(&p.Total, &conversations, &p.TotalSessions); e != nil {
			return p, e
		}
		if offset > p.Total {
			return p, fmt.Errorf("cursor past end of results")
		}
		if offset < p.Total {
			common := false
			if !shortLatin(q) {
				common, e = commonQuery(ctx, db, q, toFTS(q))
				if e != nil {
					return p, e
				}
			}
			if common {
				ids := []int64{}
				if offset < conversations {
					found, err := newestMatches(ctx, db, toFTS(q), harness, k.Projects, names, false, k.IncludeSelf, min(conversations, offset+limit))
					if err != nil {
						return p, err
					}
					ids = append(ids, found[offset:]...)
				}
				if offset+limit > conversations && p.Total > conversations {
					toolOffset := max(0, offset-conversations)
					found, err := newestMatches(ctx, db, toFTS(q), harness, k.Projects, names, true, k.IncludeSelf, min(p.Total-conversations, offset+limit-conversations))
					if err != nil {
						return p, err
					}
					ids = append(ids, found[toolOffset:]...)
				}
				if len(ids) > 0 {
					values := make([]any, len(ids))
					for i, id := range ids {
						values[i] = id
					}
					matches, e = fetchPageHits(ctx, db, `SELECT `+hitColumns+` FROM messages m JOIN sessions s ON s.uid=m.session_uid WHERE m.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) ORDER BY (m.role='tool'),m.ts DESC,m.id DESC`, values...)
					if e != nil {
						return p, e
					}
				}
			} else {
				selectTier := func(role string, n, skip int) error {
					query := `WITH ranked AS MATERIALIZED (SELECT m.id,m.ts,bm25(messages_fts) score ` + from + `AND m.role` + role + ` ORDER BY score,m.ts DESC,m.id DESC LIMIT ? OFFSET ?) SELECT ` + hitColumns + ` FROM ranked r JOIN messages m ON m.id=r.id JOIN sessions s ON s.uid=m.session_uid ORDER BY r.score,r.ts DESC,r.id DESC`
					found, err := fetchPageHits(ctx, db, query, append(args, n, skip)...)
					matches = append(matches, found...)
					return err
				}
				if offset < conversations {
					if e = selectTier(`!='tool'`, min(limit, conversations-offset), offset); e != nil {
						return p, e
					}
				}
				if offset+limit > conversations {
					if e = selectTier(`='tool'`, limit-len(matches), max(0, offset-conversations)); e != nil {
						return p, e
					}
				}
			}
		}
	}
	for _, h := range matches {
		snippet := compactSnippet(h.Text, q)
		p.Hits = append(p.Hits, compactHit{shortID(h.UID) + ":" + strconv.Itoa(h.Index), h.Harness, displayInline(h.Project), age(h.TS), h.Role, snippet, highlightRanges(snippet, q)})
	}
	p.Shown = len(p.Hits)
	p.NextCursor = nextCursor(k, offset, p.Shown, p.Total)
	return p, nil
}

func sessionTopic(ctx context.Context, db *sql.DB, uid string) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT text FROM messages WHERE session_uid=? AND role='user' ORDER BY idx`, uid)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			return "", err
		}
		if injectedUserText(text) {
			continue
		}
		text = strings.Join(strings.Fields(displayInline(text)), " ")
		if text != "" {
			return clip(text, 70), nil
		}
	}
	return "(no user message)", rows.Err()
}

func compactSessions(ctx context.Context, db *sql.DB, k pageKey, offset int) (sessionPage, error) {
	q, harness, limit := k.Query, k.Harness, k.Limit
	p := sessionPage{Sessions: []compactSession{}}
	names, err := matchingProjects(ctx, db, k.Projects)
	if err != nil {
		return p, err
	}
	var query string
	args := []any{}
	if toFTS(q) == "" {
		from := `FROM sessions s JOIN messages m ON s.uid=m.session_uid WHERE m.idx=(SELECT max(idx) FROM messages m WHERE session_uid=s.uid ` + selfFilter(k.IncludeSelf) + `) `
		if harness != "" && harness != "all" {
			from += `AND s.harness=? `
			args = append(args, harness)
		}
		clause, values := projectPredicate(k.Projects, names)
		from += clause
		args = append(args, values...)
		if e := db.QueryRowContext(ctx, `SELECT count(*) `+from, args...).Scan(&p.Total); e != nil {
			return p, e
		}
		query = `SELECT s.uid,s.harness,s.project,m.ts,m.text,1,m.idx,(m.role='user'),(m.role='asst'),(m.role='tool') ` + from + `ORDER BY (m.role='tool'),m.ts DESC,m.id DESC LIMIT ? OFFSET ?`
	} else {
		from, values, omitted, e := matchSource(ctx, db, q, harness, k.Projects, names, k.AllTime, k.IncludeSelf)
		if e != nil {
			return p, e
		}
		p.OmittedOlder = omitted
		args = values
		if e = db.QueryRowContext(ctx, `SELECT count(DISTINCT m.session_uid) `+from, args...).Scan(&p.Total); e != nil {
			return p, e
		}
		common := false
		if !shortLatin(q) {
			common, e = commonQuery(ctx, db, q, toFTS(q))
			if e != nil {
				return p, e
			}
		}
		score := `bm25(messages_fts)`
		if common {
			score = `0`
		}
		query = `WITH matched AS MATERIALIZED (SELECT m.id,m.session_uid,m.ts,m.role,` + score + ` score ` + from + `), ranked AS (SELECT id,session_uid,ts,role,score,count(*) OVER (PARTITION BY session_uid) hits,sum(role='user') OVER (PARTITION BY session_uid) you,sum(role='asst') OVER (PARTITION BY session_uid) asst,sum(role='tool') OVER (PARTITION BY session_uid) tool,row_number() OVER (PARTITION BY session_uid ORDER BY (role='tool'),score,ts DESC,id DESC) rn FROM matched) SELECT s.uid,s.harness,s.project,m.ts,m.text,r.hits,m.idx,r.you,r.asst,r.tool FROM ranked r JOIN messages m ON m.id=r.id JOIN sessions s ON s.uid=m.session_uid WHERE r.rn=1 ORDER BY (r.you+r.asst=0),r.score,r.ts DESC,r.id DESC LIMIT ? OFFSET ?`
	}
	if offset > p.Total {
		return p, fmt.Errorf("cursor past end of results")
	}
	if offset < p.Total {
		rows, e := db.QueryContext(ctx, query, append(args, limit, offset)...)
		if e != nil {
			return p, e
		}
		for rows.Next() {
			var id, harness, project, ts, text string
			var hits, idx int
			var roles roleCounts
			if e = rows.Scan(&id, &harness, &project, &ts, &text, &hits, &idx, &roles.You, &roles.Asst, &roles.Tool); e != nil {
				break
			}
			topic, topicErr := sessionTopic(ctx, db, id)
			if topicErr != nil {
				e = topicErr
				break
			}
			p.Sessions = append(p.Sessions, compactSession{Ref: shortID(id), Harness: harness, Project: displayInline(project), Age: age(ts), Hits: hits, Roles: roles, BestRef: shortID(id) + ":" + strconv.Itoa(idx), Best: compactSnippet(text, q), Topic: topic, Highlights: highlightRanges(compactSnippet(text, q), q)})
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return p, e
		}
	}
	p.Shown = len(p.Sessions)
	p.NextCursor = nextCursor(k, offset, p.Shown, p.Total)
	return p, nil
}

// Keep the first visible query match in the middle of the selected message.
func showWindow(text, q string) string {
	s := displayInline(text)
	first := -1
	var runes []rune
	for _, t := range terms(q) {
		if t.Negative {
			continue
		}
		at := strings.Index(strings.ToLower(s), strings.ToLower(t.Word))
		if at < 0 {
			if runes == nil {
				runes = []rune(s)
			}
			if found := positions(runes, t.Word); len(found) > 0 {
				at = len(string(runes[:found[0]]))
			}
		}
		if at >= 0 && at < len(s) && (first < 0 || at < first) {
			first = at
		}
	}
	if first < 0 {
		return ansi.Truncate(s, 400, "…")
	}
	for first > 0 && !utf8.RuneStart(s[first]) {
		first--
	}
	start := first
	for cells := 0; start > 0 && cells < 160; {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		start -= size
		cells += displayWidth(string(r))
	}
	if start > 0 {
		return ansi.Truncate("…"+s[start:], 400, "…")
	}
	return ansi.Truncate(s, 400, "…")
}

type showMessage struct {
	Time       string           `json:"time"`
	Role       string           `json:"role"`
	Text       string           `json:"text"`
	Hit        bool             `json:"hit"`
	Full       bool             `json:"full,omitempty"`
	Highlights []highlightRange `json:"highlights,omitempty"`
	Matches    bool             `json:"matches"`
}
type showPage struct {
	Harness      string        `json:"harness"`
	Project      string        `json:"project"`
	Model        string        `json:"model,omitempty"`
	Topic        string        `json:"topic"`
	CWD          string        `json:"cwd"`
	Date         string        `json:"date"`
	ResumeCmd    string        `json:"resume_cmd"`
	ResumeArgv   []string      `json:"resume_argv"`
	Shown        int           `json:"shown"`
	Total        int           `json:"total"`
	Start        int           `json:"start"`
	End          int           `json:"end"`
	SessionTotal int           `json:"session_total"`
	HitIndex     *int          `json:"hit_index,omitempty"`
	Messages     []showMessage `json:"messages"`
	NextCursor   string        `json:"next_cursor,omitempty"`
}

func renderShow(p showPage, asJSON bool) error {
	if asJSON {
		return writeJSON(p)
	}
	fmt.Fprintf(output, "%s · %s · %s · %s\nresume: %s\ntopic: %s\nmessages %d–%d of %d", p.Harness, p.Project, displayInline(p.CWD), p.Date, displayInline(p.ResumeCmd), p.Topic, p.Start, p.End, p.SessionTotal)
	if p.HitIndex != nil {
		fmt.Fprintf(output, " · hit %d", *p.HitIndex)
	}
	fmt.Fprintln(output)
	for _, m := range p.Messages {
		mark := " "
		if m.Hit {
			mark = ">"
		}
		if m.Full {
			lines := strings.Split(displayTranscript(m.Text), "\n")
			fmt.Fprintf(output, "%s %s %s: %s\n", mark, m.Time, m.Role, lines[0])
			for _, line := range lines[1:] {
				fmt.Fprintf(output, "  %s\n", line)
			}
		} else {
			fmt.Fprintf(output, "%s %s %s: %s\n", mark, m.Time, m.Role, m.Text)
		}
	}
	if p.NextCursor != "" {
		fmt.Fprintln(output, "cursor: "+p.NextCursor)
	}
	return nil
}
func compactShow(ctx context.Context, db *sql.DB, k pageKey, offset int) (showPage, error) {
	p := showPage{Messages: []showMessage{}}
	ref := k.Ref
	selected := -1
	if i := strings.LastIndexByte(ref, ':'); i >= 0 {
		var e error
		selected, e = strconv.Atoi(ref[i+1:])
		if e != nil || selected < 0 {
			return p, fmt.Errorf("invalid message reference %q", ref)
		}
		ref = ref[:i]
	}
	if k.Full && selected < 0 {
		return p, fmt.Errorf("--full requires a message ref")
	}
	rows, e := db.QueryContext(ctx, `SELECT uid,native_id FROM sessions`)
	if e != nil {
		return p, e
	}
	var handles, exact, prefixes []string
	for rows.Next() {
		var uid, id string
		if e = rows.Scan(&uid, &id); e != nil {
			break
		}
		if shortID(uid) == ref {
			handles = append(handles, uid)
		}
		if id == ref {
			exact = append(exact, uid)
		} else if strings.HasPrefix(id, ref) {
			prefixes = append(prefixes, uid)
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return p, e
	}
	found := prefixes
	if len(exact) > 0 {
		found = exact
	}
	if len(handles) > 0 {
		found = handles
	}
	if len(found) == 0 {
		return p, fmt.Errorf("session %q not found", ref)
	}
	if len(found) > 1 {
		return p, fmt.Errorf("session %q is ambiguous; use a handle from search results", ref)
	}
	uid := found[0]
	var nativeID, path string
	if e = db.QueryRowContext(ctx, `SELECT harness,native_id,project,cwd,path,coalesce(model,'') FROM sessions WHERE uid=?`, uid).Scan(&p.Harness, &nativeID, &p.Project, &p.CWD, &path, &p.Model); e != nil {
		return p, e
	}
	p.Project = displayInline(p.Project)
	p.Topic, e = sessionTopic(ctx, db, uid)
	if e != nil {
		return p, e
	}
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE session_uid=?`, uid).Scan(&count); e != nil {
		return p, e
	}
	if count == 0 {
		return p, fmt.Errorf("session %q has no messages", ref)
	}
	if selected >= count {
		return p, fmt.Errorf("message %d not found in session %q", selected, ref)
	}
	start, end := 0, count
	if !k.All {
		if selected < 0 {
			selected = -1
			end = min(count, k.Context+1)
		} else {
			start = max(0, selected-k.Context)
			if k.Context < count {
				end = min(count, selected+k.Context+1)
			}
		}
	}
	p.Total = end - start
	p.SessionTotal = count
	if selected >= 0 && !k.All {
		n := selected
		p.HitIndex = &n
	}
	if offset > p.Total {
		return p, fmt.Errorf("cursor past end of session")
	}
	first := start
	if selected >= 0 {
		first = selected
	}
	var ts string
	if e = db.QueryRowContext(ctx, `SELECT ts FROM messages WHERE session_uid=? AND idx=?`, uid, first).Scan(&ts); e != nil {
		return p, e
	}
	if len(ts) >= 10 {
		p.Date = ts[:10]
	}
	p.ResumeCmd = resumeCmd(p.Harness, nativeID, path, p.CWD)
	p.ResumeArgv = resumeArgv(p.Harness, nativeID, path, p.CWD)
	if offset < p.Total {
		matched, args := "0", []any{}
		if match := toFTS(k.Query); match != "" {
			matched = `1 ` + selfFilter(k.IncludeSelf) + `AND EXISTS(SELECT 1 FROM messages_fts WHERE messages_fts.rowid=m.id AND messages_fts MATCH ?)`
			args = append(args, match)
		}
		args = append(args, uid, start+offset, end, k.Limit)
		msgs, err := db.QueryContext(ctx, `SELECT idx,ts,role,text,`+matched+` FROM messages m WHERE session_uid=? AND idx>=? AND idx<? ORDER BY idx LIMIT ?`, args...)
		if err != nil {
			return p, err
		}
		for msgs.Next() {
			var idx int
			var ts, role, text string
			var matches bool
			if err = msgs.Scan(&idx, &ts, &role, &text, &matches); err != nil {
				break
			}
			hm := "??:??"
			if t, parseErr := time.Parse(time.RFC3339Nano, ts); parseErr == nil {
				hm = t.Format("15:04")
			}
			fullHit := idx == selected && k.Full
			var visible string
			switch {
			case fullHit:
				visible = text
			case idx == selected && k.Query != "":
				visible = showWindow(text, k.Query)
			default:
				visible = ansi.Truncate(displayInline(text), 400, "…")
			}
			p.Messages = append(p.Messages, showMessage{Time: hm, Role: role, Text: visible, Hit: idx == selected, Full: fullHit, Highlights: highlightRanges(visible, k.Query), Matches: matches})
		}
		if err == nil {
			err = msgs.Err()
		}
		msgs.Close()
		if err != nil {
			return p, err
		}
	}
	p.Shown = len(p.Messages)
	if p.Shown > 0 {
		p.Start, p.End = start+offset, start+offset+p.Shown-1
	}
	p.NextCursor = nextCursor(k, offset, p.Shown, p.Total)
	return p, nil
}

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

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

// Message text is quoted so a pasted transcript cannot open headings or lists of its own.
func renderShowMD(p showPage) error {
	fmt.Fprintf(output, "### %s\n\n%s · %s · `%s` · %s · messages %d–%d of %d", p.Topic, p.Harness, p.Project, displayInline(p.CWD), p.Date, p.Start, p.End, p.SessionTotal)
	if p.HitIndex != nil {
		fmt.Fprintf(output, " · hit %d", *p.HitIndex)
	}
	fmt.Fprintf(output, "\n\nResume: `%s`\n", displayInline(p.ResumeCmd))
	for _, m := range p.Messages {
		hit := ""
		if m.Hit {
			hit = " · hit"
		}
		fmt.Fprintf(output, "\n**%s** %s%s\n", m.Role, m.Time, hit)
		for _, line := range strings.Split(displayTranscript(m.Text), "\n") {
			fmt.Fprintf(output, "> %s\n", line)
		}
	}
	if p.NextCursor != "" {
		fmt.Fprintf(output, "\nMore messages: --cursor %s\n", p.NextCursor)
	}
	return nil
}
func compactShow(ctx context.Context, db *sql.DB, k pageKey, offset int, noRedact ...bool) (showPage, error) {
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
	p.Topic, e = sessionTopic(ctx, db, uid, noRedact...)
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
	if !k.All && !k.Outline {
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
	// shortcut: the index has no reply IDs; group adjacent same-timestamp text blocks until it does.
	outlineFilter := ` AND m.idx IN (
		SELECT part.idx FROM messages part JOIN (
			SELECT min(idx) AS first_idx,max(idx) AS last_idx,role,
				lead(role) OVER (ORDER BY min(idx)) AS next_role
			FROM (
				SELECT idx,role,ts,CASE WHEN role='asst' THEN
					idx-row_number() OVER (PARTITION BY role,ts ORDER BY idx)
					ELSE idx END AS block
				FROM messages WHERE session_uid=?
			) WHERE role IN ('user','asst') GROUP BY role,ts,block
		) reply ON part.idx BETWEEN first_idx AND last_idx
		WHERE part.session_uid=? AND (reply.role='user' OR next_role='user' OR next_role IS NULL)
	)`
	if k.Outline {
		if e = db.QueryRowContext(ctx, `SELECT count(*) FROM messages m WHERE session_uid=?`+outlineFilter, uid, uid, uid).Scan(&p.Total); e != nil {
			return p, e
		}
	}
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
		query := `SELECT idx,ts,role,text,` + matched + ` FROM messages m WHERE session_uid=?`
		args = append(args, uid)
		if k.Outline {
			query += outlineFilter + ` ORDER BY idx LIMIT ? OFFSET ?`
			args = append(args, uid, uid, k.Limit, offset)
		} else {
			query += ` AND idx>=? AND idx<? ORDER BY idx LIMIT ?`
			args = append(args, start+offset, end, k.Limit)
		}
		msgs, err := db.QueryContext(ctx, query, args...)
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
			if len(noRedact) == 0 || !noRedact[0] {
				text = redact(text)
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
			if len(p.Messages) == 0 {
				p.Start = idx
			}
			p.End = idx
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
	p.NextCursor = nextCursor(k, offset, p.Shown, p.Total)
	return p, nil
}

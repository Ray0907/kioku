package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func rangeCorpus(t *testing.T) *sql.DB {
	t.Helper()
	db := ctxDB(t)
	addSession(t, db, "new", "claude", "/w/app", 24*time.Hour, "user", "login fix zz", "asst", "common reply")
	addSession(t, db, "old", "codex", "/w/web", 30*24*time.Hour, "user", "login old zz", "asst", "common old reply")
	return db
}

func uids(rows []hit) string {
	seen := []string{}
	for _, r := range rows {
		seen = append(seen, r.UID)
	}
	return strings.Join(seen, ",")
}

var lastTwentyDays = timeRange{Since: ctxNow.AddDate(0, 0, -20).Format(boundLayout)}

func TestSearchRangeEveryBranch(t *testing.T) {
	db := rangeCorpus(t)
	run := func(q string, rng timeRange) []hit {
		rows, e := search(context.Background(), db, q, "all", nil, 50, false, false, false, rng)
		if e != nil {
			t.Fatal(e)
		}
		return rows
	}
	if got := uids(run("login", timeRange{})); !strings.Contains(got, "old") {
		t.Fatalf("without a range both sessions match: %q", got)
	}
	if got := uids(run("login", lastTwentyDays)); got != "new" {
		t.Fatalf("ranked: %q", got)
	}
	if got := uids(run("", lastTwentyDays)); got != "new" {
		t.Fatalf("recent sessions: %q", got)
	}
	if got := run("login", timeRange{Until: ctxNow.AddDate(0, 0, -20).Format(boundLayout)}); uids(got) != "old" {
		t.Fatalf("until: %q", uids(got))
	}
	// A 2-letter prefix searches only the last 7 days unless a range says otherwise.
	if got := uids(run("zz", timeRange{})); got != "new" {
		t.Fatalf("short prefix default window: %q", got)
	}
	if got := uids(run("zz", timeRange{Since: ctxNow.AddDate(0, 0, -60).Format(boundLayout)})); !strings.Contains(got, "old") {
		t.Fatalf("an explicit range replaces the short-prefix window: %q", got)
	}
}

func TestSearchEmptyRangeLargeIndex(t *testing.T) {
	db := ctxDB(t)
	started := time.Now()
	_, err := db.Exec(`
BEGIN;
WITH RECURSIVE seq(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM seq WHERE n<1999)
INSERT INTO sessions(uid,harness,native_id,path,cwd,project,model,started,updated,msg_count)
SELECT printf('bulk-%04d',n), CASE n%2 WHEN 0 THEN 'claude' ELSE 'codex' END,
       printf('bulk-%04d',n), '/synthetic', '/w/app',
       CASE n%3 WHEN 0 THEN 'app' ELSE 'web' END, '', '', '', 200 FROM seq;
WITH RECURSIVE seq(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM seq WHERE n<1999),
     msg(idx) AS (VALUES(0) UNION ALL SELECT idx+1 FROM msg WHERE idx<199)
INSERT INTO messages(id,session_uid,idx,ts,role,text,self)
SELECT n*200+idx+1, printf('bulk-%04d',n), idx,
       CASE n%5
         WHEN 0 THEN '2026-10-01T12:00:00Z'
         WHEN 1 THEN CASE idx WHEN 199 THEN '2026-10-15T12:00:00Z' ELSE '2026-10-01T12:00:00Z' END
         WHEN 2 THEN CASE idx WHEN 199 THEN '2026-11-01T12:00:00Z' ELSE '2026-10-20T12:00:00Z' END
         WHEN 3 THEN '2026-10-20T12:00:00Z'
         ELSE '2026-11-05T12:00:00Z'
       END, 'user', 'synthetic', (n%7=0 AND idx=199)
FROM seq CROSS JOIN msg;
COMMIT;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("built 2,000 sessions / 400,000 messages in %s", time.Since(started))
	since, until := "2026-10-10 00:00:00", "2026-10-25 00:00:00"
	for _, tc := range []struct {
		name        string
		rng         timeRange
		harness     string
		projects    []string
		includeSelf bool
	}{
		{"bounded", timeRange{since, until}, "all", nil, false},
		{"since-only", timeRange{Since: since}, "all", nil, false},
		{"until-only", timeRange{Until: until}, "all", nil, false},
		{"harness", timeRange{since, until}, "codex", nil, false},
		{"project", timeRange{since, until}, "all", []string{"APP"}, false},
		{"combined", timeRange{since, until}, "claude", []string{"app"}, false},
		{"include-self", timeRange{since, until}, "all", nil, true},
		{"no-range", timeRange{}, "all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const budget = 1500 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			started := time.Now()
			rows, err := search(ctx, db, "", tc.harness, tc.projects, 2000, false, tc.includeSelf, false, tc.rng)
			elapsed := time.Since(started)
			if err != nil || elapsed >= budget {
				t.Fatalf("TUI empty-query search took %s (budget %s): %v", elapsed, budget, err)
			}
			t.Logf("TUI empty-query search: %s", elapsed)
			cli, err := compactSearch(context.Background(), db, pageKey{
				Harness: tc.harness, Projects: tc.projects, Limit: 2000,
				IncludeSelf: tc.includeSelf, Since: tc.rng.Since, Until: tc.rng.Until,
			}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 || len(rows) != cli.Total || len(rows) != len(cli.Hits) {
				t.Fatalf("TUI rows=%d, CLI total=%d shown=%d", len(rows), cli.Total, len(cli.Hits))
			}
			if tc.name == "include-self" && len(rows) != 800 {
				t.Fatalf("want 800 sessions whose last message is in range, got %d", len(rows))
			}
			for i, row := range rows {
				if ref := fmt.Sprintf("%s:%d", shortID(row.UID), row.Index); ref != cli.Hits[i].Ref {
					t.Fatalf("row %d: TUI %s, CLI %s", i, ref, cli.Hits[i].Ref)
				}
				if i > 0 && (rows[i-1].TS < row.TS || rows[i-1].TS == row.TS && rows[i-1].ID <= row.ID) {
					t.Fatalf("rows not ordered by ts DESC, id DESC: %+v then %+v", rows[i-1], row)
				}
			}
		})
	}
}

func TestSearchRangeOnCommonWordPath(t *testing.T) {
	db := ctxDB(t)
	for i, age := range []time.Duration{24 * time.Hour, 30 * 24 * time.Hour} {
		msgs := []string{}
		for j := 0; j < 160; j++ {
			msgs = append(msgs, "user", fmt.Sprintf("commontok number %d", j))
		}
		addSession(t, db, []string{"new", "old"}[i], "claude", "/w/app", age, msgs...)
	}
	rows, e := search(context.Background(), db, "commontok", "all", nil, 500, false, false, false, lastTwentyDays)
	if e != nil || len(rows) != 160 || uids(rows[:1]) != "new" {
		t.Fatalf("common path: %d rows err=%v", len(rows), e)
	}
	for _, r := range rows {
		if r.UID != "new" {
			t.Fatalf("out-of-range hit %q", r.UID)
		}
	}
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// settle runs a command and feeds the resulting message back until no command is left.
func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for i := 0; cmd != nil && i < 8; i++ {
		msg := cmd()
		if _, ok := msg.(debounce); !ok {
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, c := range b {
					m = settle(t, m, c)
				}
				return m
			}
		}
		next, c := m.Update(msg)
		m, cmd = next.(model), c
	}
	return m
}

func TestRangeKeyFlow(t *testing.T) {
	db := rangeCorpus(t)
	rows, _ := search(context.Background(), db, "login", "all", nil, 50, false, false, false, timeRange{})
	m := newModel(db, "login", "all", nil, rows, false, 50, false)
	m.focus, m.syncing = false, false
	if len(m.rows) != 2 {
		t.Fatalf("setup: %d rows", len(m.rows))
	}

	next, _ := m.Update(keyRunes("d"))
	m = next.(model)
	if m.prompt != promptRange {
		t.Fatal("d must open the range prompt")
	}
	if v := m.View(); !strings.Contains(v, "range") || !strings.Contains(v, "since..until") {
		t.Fatalf("prompt must say what to type:\n%s", v)
	}
	for _, r := range "2026-01-01..2026-01-02" {
		next, _ = m.Update(keyRunes(string(r)))
		m = next.(model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.prompt != promptNone || m.rng.active() {
		t.Fatal("esc cancels without setting a range")
	}

	next, _ = m.Update(keyRunes("d"))
	m = next.(model)
	for _, r := range "bogus" {
		next, _ = m.Update(keyRunes(string(r)))
		m = next.(model)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd != nil || m.rng.active() || m.prompt != promptNone || !strings.Contains(m.status, "invalid bogus") {
		t.Fatalf("a bad value keeps the old range and says why: status=%q cmd=%v", m.status, cmd != nil)
	}

	m.status = ""
	next, _ = m.Update(keyRunes("d"))
	m = next.(model)
	for _, r := range "2026-10-01.." {
		next, _ = m.Update(keyRunes(string(r)))
		m = next.(model)
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = settle(t, next.(model), cmd)
	if m.rng.Since == "" || m.rng.Until != "" {
		t.Fatalf("range not set: %+v", m.rng)
	}
	if uids(m.rows) != "new" {
		t.Fatalf("rows after setting the range: %q", uids(m.rows))
	}
	if v := m.View(); !strings.Contains(v, "from 2026-10-01") {
		t.Fatalf("header must show the range:\n%s", v)
	}

	next, cmd = m.Update(keyRunes("D"))
	m = settle(t, next.(model), cmd)
	if m.rng.active() || len(m.rows) != 2 {
		t.Fatalf("D must clear the range and re-run the search: %+v rows=%d", m.rng, len(m.rows))
	}
	if strings.Contains(m.View(), "from 2026") {
		t.Fatal("header still shows a cleared range")
	}

	m.rng = lastTwentyDays
	m.rows = nil
	if v := m.View(); !strings.Contains(v, "No message in this date range") {
		t.Fatalf("empty result in a range must say so:\n%s", v)
	}
}

func TestRangeKeyIsALetterInTheSearchBox(t *testing.T) {
	m := newModel(rangeCorpus(t), "", "all", nil, nil, false, 50, false)
	m.focus = true
	next, _ := m.Update(keyRunes("d"))
	if g := next.(model); g.q != "d" || g.prompt != promptNone {
		t.Fatalf("in the search box d types: q=%q prompt=%v", g.q, g.prompt)
	}
}

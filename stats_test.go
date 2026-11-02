package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Three sessions across two harnesses and two projects. Last messages land one minute before the stated age.
func statsCorpus(t *testing.T) *sql.DB {
	t.Helper()
	db := ctxDB(t)
	addSession(t, db, "a", "claude", "/w/app", 2*time.Hour, "user", "one", "asst", "two", "user", "three", "asst", "four")
	addSession(t, db, "b", "codex", "/w/app", 50*time.Hour, "user", "old question", "asst", "old answer")
	addSession(t, db, "c", "claude", "/w/web", 3*24*time.Hour, "user", "web question", "asst", "web answer")
	return db
}

func collect(t *testing.T, db *sql.DB, harness string, projects []string, rng timeRange) statsReport {
	t.Helper()
	r, e := collectStats(context.Background(), db, harness, projects, rng)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestStatsCounts(t *testing.T) {
	r := collect(t, statsCorpus(t), "all", nil, timeRange{})
	if r.Total.Sessions != 3 || r.Total.Messages != 8 || r.Total.LastActive != "2026-10-23T09:59:00Z" {
		t.Fatalf("total %+v", r.Total)
	}
	want := []statRow{{"claude", 2, 6, "2026-10-23T09:59:00Z"}, {"codex", 1, 2, "2026-10-21T09:59:00Z"}}
	if fmt.Sprint(r.Harness) != fmt.Sprint(want) {
		t.Fatalf("harness %+v", r.Harness)
	}
	want = []statRow{{"app", 2, 6, "2026-10-23T09:59:00Z"}, {"web", 1, 2, "2026-10-20T11:59:00Z"}}
	if fmt.Sprint(r.Project) != fmt.Sprint(want) {
		t.Fatalf("project %+v", r.Project)
	}
}

func TestStatsRangeCountsMessagesInsideIt(t *testing.T) {
	db := statsCorpus(t)
	// Session a has messages at 09:57, 09:58 (before) ... only those at or after the bound count.
	r := collect(t, db, "all", nil, timeRange{Since: "2026-10-23 09:58:00"})
	if r.Total.Sessions != 1 || r.Total.Messages != 2 {
		t.Fatalf("since: %+v", r.Total)
	}
	r = collect(t, db, "all", nil, timeRange{Until: "2026-10-22 00:00:00"})
	if r.Total.Sessions != 2 || r.Total.Messages != 4 || len(r.Harness) != 2 {
		t.Fatalf("until: %+v", r)
	}
	r = collect(t, db, "all", nil, timeRange{Since: "2030-01-01 00:00:00"})
	if r.Total.Sessions != 0 || r.Total.Messages != 0 || r.Total.LastActive != "" || len(r.Project) != 0 {
		t.Fatalf("empty: %+v", r)
	}
}

func TestStatsFiltersAndSelf(t *testing.T) {
	db := statsCorpus(t)
	if _, e := db.Exec(`INSERT INTO messages(session_uid,idx,ts,role,text,self) VALUES('a',9,'2026-10-23T11:30:00Z','tool','kioku search',1)`); e != nil {
		t.Fatal(e)
	}
	r := collect(t, db, "codex", nil, timeRange{})
	if r.Total.Sessions != 1 || len(r.Harness) != 1 || r.Harness[0].Value != "codex" {
		t.Fatalf("harness filter: %+v", r)
	}
	r = collect(t, db, "all", []string{"WEB"}, timeRange{})
	if r.Total.Sessions != 1 || len(r.Project) != 1 || r.Project[0].Value != "web" {
		t.Fatalf("project filter: %+v", r)
	}
	if all := collect(t, db, "all", nil, timeRange{}); all.Total.Messages != 8 || all.Total.LastActive != "2026-10-23T09:59:00Z" {
		t.Fatalf("self messages counted: %+v", all.Total)
	}
	if none := collect(t, db, "all", []string{"nope"}, timeRange{}); none.Total.Sessions != 0 {
		t.Fatalf("unknown project: %+v", none.Total)
	}
}

func TestStatsFoldsProjectsPastTwenty(t *testing.T) {
	db := ctxDB(t)
	for i := 0; i < 23; i++ {
		addSession(t, db, fmt.Sprintf("s%02d", i), "pi", fmt.Sprintf("/w/p%02d", i), time.Duration(i+1)*time.Hour, "user", "q", "asst", "a")
	}
	r := collect(t, db, "all", nil, timeRange{})
	if len(r.Project) != 21 {
		t.Fatalf("projects %d", len(r.Project))
	}
	other := r.Project[20]
	if other.Value != "(other)" || other.Sessions != 3 || other.Messages != 6 {
		t.Fatalf("other %+v", other)
	}
	if other.LastActive != "2026-10-22T14:59:00Z" {
		t.Fatalf("other last %q", other.LastActive)
	}
}

func TestStatsRender(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	defer func() { time.Local = old }()
	r := collect(t, statsCorpus(t), "all", nil, timeRange{})
	got := capture(t, func() error { return renderStats(r, false) })
	want := `3 sessions · 8 messages · last active 2026-10-23 09:59

HARNESS  SESSIONS  MESSAGES  LAST ACTIVE
claude   2         6         2026-10-23 09:59
codex    1         2         2026-10-21 09:59

PROJECT  SESSIONS  MESSAGES  LAST ACTIVE
app      2         6         2026-10-23 09:59
web      1         2         2026-10-20 11:59
`
	if got != want {
		t.Fatalf("text:\n%s", got)
	}
	js := capture(t, func() error { return renderStats(r, true) })
	for _, s := range []string{`"total":{"sessions":3,"messages":8,"last_active":"2026-10-23T09:59:00Z"}`, `"harness":[{"value":"claude","sessions":2,"messages":6,"last_active":"2026-10-23T09:59:00Z"}`, `"project":[`} {
		if !strings.Contains(js, s) {
			t.Fatalf("json missing %s:\n%s", s, js)
		}
	}
	empty := capture(t, func() error {
		return renderStats(collect(t, statsCorpus(t), "all", nil, timeRange{Since: "2030-01-01 00:00:00"}), false)
	})
	if empty != "0 sessions · 0 messages\n" {
		t.Fatalf("empty: %q", empty)
	}
}

func TestStatsArguments(t *testing.T) {
	doctorHome(t)
	for _, args := range [][]string{
		{"stats", "extra"},
		{"stats", "--harness", "nope"},
		{"stats", "--since", "bogus"},
		{"stats", "--since", "2026-02-01", "--until", "2026-01-01"},
	} {
		if out, e := runArgs(t, args...); e == nil || out != "" {
			t.Errorf("%v: want an error and no output, got %q %v", args, out, e)
		}
	}
	if out, e := runArgs(t, "stats", "--json", "-p", "x", "--harness", "claude", "--since", "7d", "--until", "today"); e != nil || out == "" {
		t.Errorf("valid arguments: %q %v", out, e)
	}
}

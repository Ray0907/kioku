package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestTUIHeaderTotals(t *testing.T) {
	db := ctxDB(t)
	for i := 0; i < 160; i++ {
		addSession(t, db, fmt.Sprintf("s%d", i), "claude", "/w/app", time.Duration(i+1)*time.Hour,
			"user", "needle", "assistant", "needle detail")
	}
	for _, tc := range []struct {
		name, query, want string
		limit             int
	}{
		{"truncated", "needle", "300 of 320 messages · 160 sessions", 300},
		{"untruncated", "needle", "320 messages · 160 sessions", 400},
		{"exact limit", "needle", "320 messages · 160 sessions", 320},
		{"browse", "", "100 recent sessions", 100},
		{"negative only browse", "-needle", "100 recent sessions", 100},
		{"empty", "absent", "0 messages · 0 sessions", 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{db: db, q: tc.query, harness: "all", limit: tc.limit, width: 120, height: 24, pal: light}
			x := queryCmd(context.Background(), db, m.q, m.harness, nil, m.limit, m.revision, false, false, false, timeRange{})().(resultMsg)
			if x.err != nil {
				t.Fatal(x.err)
			}
			next, _ := m.Update(x)
			got := strings.SplitN(ansi.Strip(next.(model).View()), "\n", 2)[0]
			if !strings.Contains(got, tc.want) || tc.name != "truncated" && strings.Contains(got, " of ") {
				t.Fatalf("want header %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func TestTUIHeaderTotalsFollowQuery(t *testing.T) {
	db := rangeCorpus(t)
	addSession(t, db, "tools", "claude", "/w/app", time.Hour, "tool", "login tool")
	if _, err := db.Exec(`UPDATE messages SET self=1 WHERE session_uid='tools'`); err != nil {
		t.Fatal(err)
	}
	m := model{db: db, q: "login", harness: "all", limit: 1, width: 120, height: 24, pal: light}
	query := func() resultMsg {
		t.Helper()
		x := queryCmd(context.Background(), db, m.q, m.harness, m.projects, m.limit, m.revision, m.allTime, m.includeSelf, false, m.rng)().(resultMsg)
		if x.err != nil {
			t.Fatal(x.err)
		}
		return x
	}
	apply := func(x resultMsg, want string) {
		t.Helper()
		next, _ := m.Update(x)
		m = next.(model)
		if got := ansi.Strip(m.View()); !strings.Contains(got, want) {
			t.Fatalf("want header %q, got:\n%s", want, got)
		}
	}
	stale := query()
	apply(stale, "1 of 2 messages · 2 sessions")
	m.revision++
	m.includeSelf = true
	apply(query(), "1 of 3 messages · 3 sessions")
	m.revision++
	m.rng = lastTwentyDays
	apply(query(), "1 of 2 messages · 2 sessions")
	m.revision++
	m.projects = []string{"WEB"}
	apply(query(), "0 messages · 0 sessions")
	m.revision++
	m.rng = timeRange{}
	apply(query(), "1 messages · 1 sessions")
	m.revision++
	m.projects = nil
	m.harness = "codex"
	apply(query(), "1 messages · 1 sessions")
	m.revision++
	m.q = "absent"
	apply(query(), "0 messages · 0 sessions")
	apply(stale, "0 messages · 0 sessions")
}

func TestTUIInitialCountsPendingSync(t *testing.T) {
	db := rangeCorpus(t)
	addSession(t, db, "recent", "pi", "/w/app", time.Hour, "user", "zz recent")
	rows, err := search(context.Background(), db, "zz", "all", nil, 1, true, false, false, timeRange{})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(db, "zz", "all", nil, rows, false, 1, false)
	m.allTime = true
	if got := strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0]; !strings.Contains(got, "1 messages · 1 sessions shown") {
		t.Fatalf("startup presented loaded rows as totals: %s", got)
	}
	next, cmd := m.Update(syncEnvelope{x: syncMsg{done: true}})
	m = next.(model)
	x := cmd().(resultMsg)
	if x.err != nil {
		t.Fatal(x.err)
	}
	next, _ = m.Update(x)
	if got := ansi.Strip(next.(model).View()); !strings.Contains(got, "1 of 3 messages · 3 sessions") {
		t.Fatalf("post-sync count ignored --all-time: %s", got)
	}
}

func TestTUIHeaderCommonShortPrefixTotals(t *testing.T) {
	db := ctxDB(t)
	msgs := make([]string, 0, 602)
	for i := 0; i < 301; i++ {
		msgs = append(msgs, "user", "zz")
	}
	addSession(t, db, "old", "claude", "/w/app", 30*24*time.Hour, msgs...)
	addSession(t, db, "new", "pi", "/w/app", time.Hour, msgs...)
	for _, tc := range []struct {
		name    string
		allTime bool
		rng     timeRange
		want    string
	}{
		{"default window", false, timeRange{}, "300 of 301 messages · 1 sessions"},
		{"all time", true, timeRange{}, "300 of 602 messages · 2 sessions"},
		{"explicit range", false, lastTwentyDays, "300 of 301 messages · 1 sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := model{q: "zz", limit: 300, width: 120, height: 24, pal: light}
			x := queryCmd(context.Background(), db, m.q, "all", nil, m.limit, 0, tc.allTime, false, false, tc.rng)().(resultMsg)
			if x.err != nil {
				t.Fatal(x.err)
			}
			next, _ := m.Update(x)
			if got := ansi.Strip(next.(model).View()); !strings.Contains(got, tc.want) {
				t.Fatalf("want header %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func TestTUIHeaderPerformance(t *testing.T) {
	path := os.Getenv("KIOKU_B3_PERF_DB")
	if path == "" {
		t.Skip("set KIOKU_B3_PERF_DB to the synthetic 200k-message e2e index")
	}
	t.Setenv("KIOKU_INDEX", path)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n < 200000 {
		t.Fatalf("want 200k fixture: messages=%d err=%v", n, err)
	}
	queries := []string{"perfneedle", "synthetic", "neutral", `"large fixture"`, `"synthetic message"`, "-perfneedle", "large", "fixture", "message", "neutral 42", "neutral 17", "魚池", "紅茶", "日本語", "한국어", "perfneed", `"neutral synthetic"`, "-neutral", `"fixture message"`, "absenttoken"}
	var latencies, counts []time.Duration
	for _, q := range queries {
		start := time.Now()
		x := queryCmd(context.Background(), db, q, "all", nil, 300, 0, false, false, false, timeRange{})().(resultMsg)
		latencies = append(latencies, time.Since(start))
		if x.err != nil {
			t.Fatalf("%q: %v", q, x.err)
		}
		start = time.Now()
		_, _, err := searchTotals(context.Background(), db, pageKey{Query: q, Harness: "all", Limit: 300}, len(x.rows))
		counts = append(counts, time.Since(start))
		if err != nil {
			t.Fatalf("count %q: %v", q, err)
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sort.Slice(counts, func(i, j int) bool { return counts[i] < counts[j] })
	p95 := latencies[18]
	t.Logf("messages=%d queries=%d TUI search+count p95=%s count-only p95=%s", n, len(queries), p95, counts[18])
	if p95 > 200*time.Millisecond {
		t.Fatalf("TUI search+count p95=%s > 200ms", p95)
	}
}

func TestTUIHeaderShortPrefixTotals(t *testing.T) {
	db := rangeCorpus(t)
	addSession(t, db, "recent", "pi", "/w/app", time.Hour, "user", "zz recent")
	for _, tc := range []struct {
		allTime bool
		rng     timeRange
		want    string
	}{
		{false, timeRange{}, "1 of 2 messages · 2 sessions"},
		{true, timeRange{}, "1 of 3 messages · 3 sessions"},
		{false, lastTwentyDays, "1 of 2 messages · 2 sessions"},
	} {
		m := model{db: db, q: "zz", limit: 1, width: 120, height: 24, pal: light}
		x := queryCmd(context.Background(), db, m.q, "all", nil, 1, 0, tc.allTime, false, false, tc.rng)().(resultMsg)
		if x.err != nil {
			t.Fatal(x.err)
		}
		next, _ := m.Update(x)
		if got := ansi.Strip(next.(model).View()); !strings.Contains(got, tc.want) {
			t.Fatalf("allTime=%v range=%v: want %q, got:\n%s", tc.allTime, tc.rng, tc.want, got)
		}
	}
}

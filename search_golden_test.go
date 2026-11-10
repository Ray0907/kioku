package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestSearchTUIAndCLIOrder(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "old", "claude", "/w/app", 30*24*time.Hour, "user", "ubiquitous needle 魚池")
	addSession(t, db, "conversation", "codex", "/w/app", time.Hour,
		"user", "ubiquitous needle 魚池", "asst", "ubiquitous needle needle 魚池")
	addSession(t, db, "tool", "codex", "/w/other", 0, "tool", "ubiquitous needle needle needle 魚池")
	addSession(t, db, "self", "codex", "/w/app", 0, "user", "ubiquitous needle 魚池")
	addSession(t, db, "automated", "codex", "/w/app", 0, "asst", "ubiquitous needle 魚池")
	var filler []string
	for i := 0; i < 300; i++ {
		filler = append(filler, "tool", "ubiquitous filler")
	}
	addSession(t, db, "filler", "claude", "/w/other", 15*24*time.Hour, filler...)
	if _, err := db.Exec(`UPDATE messages SET self=1 WHERE session_uid='self'; UPDATE sessions SET automated=1 WHERE uid='automated'`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	common, err := commonQuery(ctx, db, "ubiquitous", toFTS("ubiquitous"))
	if err != nil || !common {
		t.Fatalf("fixture must exercise common-word recency: common=%v err=%v", common, err)
	}
	for _, q := range []string{"needle", "n", "ne", "ubiquitous", "魚池", "needle -absent", "-needle", "", "absent"} {
		for _, flags := range []pageKey{
			{}, {Harness: "codex"}, {Projects: []string{"app"}}, {AllTime: true},
			{IncludeSelf: true}, {IncludeAutomated: true},
			{Since: ctxNow.Add(-2 * time.Hour).Format(time.RFC3339Nano), Until: ctxNow.Format(time.RFC3339Nano)},
		} {
			for _, limit := range []int{2, 300} {
				k := flags
				k.Query, k.Limit = q, limit
				t.Run(fmt.Sprintf("%s/%+v", q, k), func(t *testing.T) {
					rows, err := search(ctx, db, q, k.Harness, k.Projects, limit, k.AllTime, k.IncludeSelf, k.IncludeAutomated, k.rng())
					if err != nil {
						t.Fatal(err)
					}
					page, err := compactSearch(ctx, db, k, 0)
					if err != nil {
						t.Fatal(err)
					}
					var tui, cli []string
					for _, h := range rows {
						tui = append(tui, fmt.Sprintf("%s:%d", shortID(h.UID), h.Index))
					}
					for _, h := range page.Hits {
						cli = append(cli, h.Ref)
					}
					if !reflect.DeepEqual(tui, cli) {
						t.Fatalf("ordered message refs differ: TUI=%v CLI=%v", tui, cli)
					}
				})
			}
		}
	}
}

// Opt-in comparison against frozen, pre-refactor TUI results on both e2e indexes.
func TestTUISearchGolden(t *testing.T) {
	dir := os.Getenv("KIOKU_SEARCH_GOLDEN_DIR")
	if dir == "" {
		t.Skip("set KIOKU_SEARCH_GOLDEN_DIR to the frozen synthetic.db and perf.db fixtures")
	}
	type result struct {
		Query, Harness string
		Range          timeRange
		Hits           []struct {
			UID string
			ID  int64
		}
	}
	for _, fixture := range []string{"synthetic", "perf"} {
		t.Run(fixture, func(t *testing.T) {
			t.Setenv("KIOKU_INDEX", filepath.Join(dir, fixture+".db"))
			db, err := openDB()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var results []result
			var latencies []time.Duration
			for _, q := range []string{"snapshot", "perfneedle", "s", "sy", "p", "pe", "synthetic", "neutral", "message", "魚池", "-perfneedle", ""} {
				for _, rng := range []timeRange{{}, {Since: "2026-09-27T00:00:00Z", Until: "2026-09-28T00:00:00Z"}} {
					for _, harness := range []string{"all", "codex"} {
						started := time.Now()
						x := queryCmd(context.Background(), db, q, harness, nil, 300, 0, false, false, false, rng)().(resultMsg)
						elapsed := time.Since(started)
						latencies = append(latencies, elapsed)
						if elapsed > 100*time.Millisecond {
							t.Logf("slow query=%q harness=%s range=%+v latency=%s", q, harness, rng, elapsed)
						}
						if x.err != nil {
							t.Fatalf("query=%q harness=%s range=%+v: %v", q, harness, rng, x.err)
						}
						r := result{Query: q, Harness: harness, Range: rng}
						for _, h := range x.rows {
							r.Hits = append(r.Hits, struct {
								UID string
								ID  int64
							}{h.UID, h.ID})
						}
						results = append(results, r)
					}
				}
			}
			got, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join(dir, fixture+"-before.json")
			if os.Getenv("KIOKU_SEARCH_GOLDEN_CAPTURE") == "1" {
				// Never overwrite the pre-change evidence, even on an accidental rerun.
				f, err := os.OpenFile(golden, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := f.Write(got)
				closeErr := f.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("save golden: write=%v close=%v", writeErr, closeErr)
				}
				return
			}
			if err := os.WriteFile(filepath.Join(dir, fixture+"-after.json"), got, 0600); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("TUI results changed; compare %s with %s-after.json; do not update the golden", golden, fixture)
			}
			t.Logf("%d query/range/harness cases: zero ordered UID/message-ID differences", len(results))
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			p95 := latencies[(len(latencies)*95+99)/100-1]
			t.Logf("TUI queryCmd (300-hit limit, including totals/snippets): p95=%s", p95)
			if fixture == "perf" && p95 > 200*time.Millisecond {
				t.Fatalf("200k fixture TUI p95 %s exceeds 200ms", p95)
			}
		})
	}
}

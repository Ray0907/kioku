package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func automatedFixture(t *testing.T) {
	t.Helper()
	doctorHome(t)
	for id, prompt := range map[string]string{
		"normal":     "b7needle fix the login page",
		"quote":      "b7needle someone said Review this change yesterday",
		"review":     "Review this change b7needle",
		"guidelines": "# Review Guidelines\nb7needle",
	} {
		appendAutomatedMessage(t, id, "user", prompt)
	}
	if out, err := runArgs(t, "index", "--rebuild"); err != nil {
		t.Fatalf("index: %s: %v", out, err)
	}
}

func appendAutomatedMessage(t *testing.T, id, role, text string) {
	t.Helper()
	home := os.Getenv("HOME")
	path := filepath.Join(home, ".claude", "projects", id, id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	content := []map[string]any{{"type": "text", "text": text}}
	if role == "tool" {
		role = "user"
		content = []map[string]any{{"type": "tool_result", "tool_use_id": "synthetic", "content": text}}
	}
	record := map[string]any{"type": role, "sessionId": id, "cwd": filepath.Join(home, "work", id), "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": role, "content": content}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(data, '\n'))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v, close: %v", err, closeErr)
	}
}

func TestAutomatedCLI(t *testing.T) {
	automatedFixture(t)
	for _, query := range []string{"b7needle", "b7", ""} {
		for _, sessions := range []bool{false, true} {
			for _, include := range []bool{false, true} {
				args := []string{"--json", "--facets", query}
				want := 2
				if sessions {
					args = append(args, "--sessions")
				}
				if include {
					args = append(args, "--include-automated")
					want = 4
				}
				out, err := runArgs(t, args...)
				if err != nil {
					t.Errorf("%v: %v", args, err)
					continue
				}
				checkAutomatedPage(t, out, sessions, want)
			}
		}
	}
	out, err := runArgs(t, "show", "review", "--json")
	if err != nil || !strings.Contains(out, "Review this change b7needle") {
		t.Fatalf("automated show: %s: %v", out, err)
	}
}

func checkAutomatedPage(t *testing.T, out string, sessions bool, want int) {
	t.Helper()
	var p struct {
		Total         int              `json:"total"`
		Shown         int              `json:"shown"`
		TotalSessions int              `json:"total_sessions"`
		Hits          []compactHit     `json:"hits"`
		Sessions      []compactSession `json:"sessions"`
		Facets        *facets          `json:"facets"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("JSON: %s: %v", out, err)
	}
	if p.Total != want || p.Shown != want || !sessions && (len(p.Hits) != want || p.TotalSessions != want) || sessions && len(p.Sessions) != want {
		t.Errorf("want %d results: %s", want, out)
	}
	if want == 2 && (strings.Contains(out, "Review Guidelines") || strings.Contains(out, "Review this change b7needle")) {
		t.Errorf("automated text leaked: %s", out)
	}
	if p.Facets == nil {
		t.Fatal("missing facets")
	}
	for name, counts := range map[string][]facetCount{"project": p.Facets.Project, "harness": p.Facets.Harness, "week": p.Facets.Week} {
		if name == "week" && len(counts) == 0 {
			continue
		}
		total := 0
		for _, c := range counts {
			total += c.Count
		}
		if total != want {
			t.Errorf("%s facets total %d, want %d", name, total, want)
		}
	}
	data, _ := json.Marshal(p.Facets)
	if want == 2 && (strings.Contains(string(data), "review") || strings.Contains(string(data), "guidelines")) {
		t.Errorf("automated facets leaked: %s", data)
	}
}

func TestAutomatedMCP(t *testing.T) {
	automatedFixture(t)
	for _, tool := range []string{"kioku_search", "kioku_sessions"} {
		for _, query := range []string{"b7needle", ""} {
			for _, include := range []bool{false, true} {
				args := map[string]any{"query": query, "facets": true}
				want := 2
				if include {
					args["include_automated"] = true
					want = 4
				}
				params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
				out, err := mcpCall(params)
				if err != nil {
					t.Errorf("%s: %v", params, err)
					continue
				}
				checkAutomatedPage(t, out, tool == "kioku_sessions", want)
			}
		}
	}
}

func TestAutomatedTUI(t *testing.T) {
	automatedFixture(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"b7needle", ""} {
		m := model{db: db, q: q, harness: "all", limit: 1, width: 120, height: 24, pal: light}
		x := queryCmd(context.Background(), db, q, "all", nil, 1, 0, false, false, false, timeRange{})().(resultMsg)
		if x.err != nil {
			t.Fatal(x.err)
		}
		next, _ := m.Update(x)
		screen := ansi.Strip(next.(model).View())
		if strings.Contains(screen, "Review Guidelines") || strings.Contains(screen, "Review this change b7needle") || q != "" && !strings.Contains(screen, "1 of 2 messages · 2 sessions") {
			t.Errorf("automated TUI results: %s", screen)
		}
	}
}

func TestAutomatedCommonAndAppend(t *testing.T) {
	automatedFixture(t)
	for _, id := range []string{"normal", "quote", "review", "guidelines"} {
		for i := 0; i < 80; i++ {
			role := "user"
			if i%2 == 0 {
				role = "tool"
			}
			appendAutomatedMessage(t, id, role, "commonprobe")
		}
	}
	// A later review prompt must not reclassify a normal session.
	appendAutomatedMessage(t, "normal", "user", "Review this change laterprobe")
	// A file can acquire its first user message during an incremental append.
	appendAutomatedMessage(t, "late", "assistant", "waiting")
	if out, err := runArgs(t, "index"); err != nil {
		t.Fatalf("sync: %s: %v", out, err)
	}
	appendAutomatedMessage(t, "late", "user", "Review this change lateprobe")
	for _, include := range []bool{false, true} {
		args := []string{"--json", "--facets", "--limit", "500", "commonprobe"}
		want := 160
		if include {
			args = append(args, "--include-automated")
			want = 320
		}
		out, err := runArgs(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		var p hitPage
		if err = json.Unmarshal([]byte(out), &p); err != nil || p.Total != want || p.Shown != want || p.TotalSessions != want/80 {
			t.Fatalf("common-word hits: %s: %v", out, err)
		}
		db, err := openDB()
		if err != nil {
			t.Fatal(err)
		}
		m := model{db: db, q: "commonprobe", harness: "all", limit: 1, width: 120, height: 24, pal: light, includeAutomated: include}
		x := queryCmd(context.Background(), db, m.q, "all", nil, 1, 0, false, false, include, timeRange{})().(resultMsg)
		db.Close()
		if x.err != nil {
			t.Fatal(x.err)
		}
		next, _ := m.Update(x)
		wantHeader := "1 of 160 messages · 2 sessions"
		if include {
			wantHeader = "1 of 320 messages · 4 sessions"
		}
		if screen := ansi.Strip(next.(model).View()); !strings.Contains(screen, wantHeader) {
			t.Errorf("want %s: %s", wantHeader, screen)
		}
	}
	for query, want := range map[string]int{"laterprobe": 1, "lateprobe": 0} {
		out, err := runArgs(t, "--json", query)
		var p hitPage
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal([]byte(out), &p); err != nil || p.Total != want {
			t.Fatalf("first user classification: %s: %v", out, err)
		}
	}
}

func TestAutomatedMigrationSourceFailure(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, "opencode.db")
	t.Setenv("KIOKU_OPENCODE_DB", path)
	source, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err = source.Exec(`
CREATE TABLE session_v2(id TEXT PRIMARY KEY, parent_id TEXT, directory TEXT, time_created INTEGER, time_updated INTEGER);
CREATE TABLE session_message(id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT);
INSERT INTO session_v2 VALUES('review',NULL,'/work/review',1000,1000);
INSERT INTO session_message VALUES('prompt','review','user',1,1000,1000,'{"text":"Review this change migrationprobe"}');`); err != nil {
		t.Fatal(err)
	}
	if out, err := runArgs(t, "index", "--rebuild"); err != nil {
		t.Fatalf("index: %s: %v", out, err)
	}
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`ALTER TABLE sessions DROP COLUMN automated; UPDATE meta SET schema_version=3`); err != nil {
		t.Fatal(err)
	}
	// Hide the source schema without changing session timestamps.
	if _, err = source.Exec(`ALTER TABLE session_v2 RENAME TO unavailable`); err != nil {
		t.Fatal(err)
	}
	out, err := runArgs(t, "--json", "migrationprobe")
	if err != nil {
		t.Fatalf("migration with an unavailable source: %s: %v", out, err)
	}
	var during hitPage
	if err = json.Unmarshal([]byte(out), &during); err != nil || during.Total != 0 {
		t.Fatalf("retained automated session leaked during source failure: %s: %v", out, err)
	}
	var version, count, automated int
	if err = db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*),automated FROM sessions`).Scan(&count, &automated); err != nil {
		t.Fatal(err)
	}
	if version != 5 || count != 1 || automated != 1 {
		t.Fatalf("migration did not classify retained session: version=%d count=%d automated=%d", version, count, automated)
	}
	if _, err = source.Exec(`ALTER TABLE unavailable RENAME TO session_v2`); err != nil {
		t.Fatal(err)
	}
	for _, include := range []bool{false, true} {
		args, want := []string{"--json", "migrationprobe"}, 0
		if include {
			args, want = append(args, "--include-automated"), 1
		}
		out, err := runArgs(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		var page hitPage
		if err = json.Unmarshal([]byte(out), &page); err != nil || page.Total != want {
			t.Fatalf("recovered search: %s: %v", out, err)
		}
	}
	if err = db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT automated FROM sessions`).Scan(&automated); err != nil {
		t.Fatal(err)
	}
	if version != 5 || automated != 1 {
		t.Fatalf("recovery did not finish classification: version=%d automated=%d", version, automated)
	}
}

func TestAutomatedSchemaRebuildAndVacuum(t *testing.T) {
	automatedFixture(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-B7 schema, including a cached source that would otherwise be skipped.
	rows, err := db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		t.Fatal(err)
	}
	hasColumn := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		hasColumn = hasColumn || name == "automated"
	}
	rows.Close()
	if hasColumn {
		if _, err = db.Exec(`ALTER TABLE sessions DROP COLUMN automated`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`UPDATE meta SET schema_version=3`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if out, err := runArgs(t, "--json", "b7needle"); err != nil {
		t.Fatal(err)
	} else {
		var p hitPage
		if err = json.Unmarshal([]byte(out), &p); err != nil || p.Total != 2 {
			t.Fatalf("old index not rebuilt: %s: %v", out, err)
		}
	}
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE waste(data); INSERT INTO waste VALUES(zeroblob(1048576)); DROP TABLE waste`); err != nil {
		t.Fatal(err)
	}
	freePages := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := freePages()
	if before == 0 {
		t.Fatal("fixture has no free pages")
	}
	if _, err = syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	if got := freePages(); got != before {
		t.Fatalf("incremental sync reclaimed pages: %d -> %d", before, got)
	}
	if _, err = syncIndex(db, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := freePages(); got != 0 {
		t.Fatalf("rebuild left %d free pages", got)
	}
}

func TestTokenizerChangeRebuildsFTSIndex(t *testing.T) {
	automatedFixture(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE meta SET schema_version=4`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	out, err := runArgs(t, "--json", "b7needle")
	if err != nil {
		t.Fatal(err)
	}
	var p hitPage
	if err = json.Unmarshal([]byte(out), &p); err != nil || p.Total != 2 {
		t.Fatalf("index from the previous tokenizer not rebuilt: %s: %v", out, err)
	}
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 5 {
		t.Fatalf("schema_version=%d err=%v, want 5", version, err)
	}
	if _, err = db.Exec(`INSERT INTO messages_fts(messages_fts, rank) VALUES('integrity-check', 1)`); err != nil {
		t.Fatalf("fts index inconsistent with messages: %v", err)
	}
}

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func hereDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func hereGit(t *testing.T, path string, file bool) {
	t.Helper()
	if file {
		if err := os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: synthetic-worktree\n"), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		hereDir(t, filepath.Join(path, ".git"))
	}
}

func hereDB(t *testing.T) *sql.DB {
	t.Helper()
	db := ctxDB(t)
	// Finish the initial schema migration before inserting synthetic indexed rows.
	if _, err := syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	return db
}

func hereFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	doctorHome(t)
	root := hereDir(t, filepath.Join(t.TempDir(), "scope-lab"))
	hereGit(t, root, false)
	hereDir(t, filepath.Join(root, "child"))
	db := hereDB(t)
	addSession(t, db, "scope-root", "claude", root, time.Hour, "user", "scopeprobe scope", "asst", "scopeprobe scope")
	addSession(t, db, "scope-child", "codex", filepath.Join(root, "child"), 2*time.Hour, "user", "scopeprobe scope", "tool", "scopeprobe scope")
	addSession(t, db, "scope-old", "claude", root, 30*24*time.Hour, "user", "scopeprobe scope")
	addSession(t, db, "scope-sibling", "claude", root+"-sibling", 0, "user", "scopeprobe scope", "asst", "scopeprobe scope")
	addSession(t, db, "scope-elsewhere", "pi", "/invented/elsewhere/scope-lab", 30*24*time.Hour, "user", "scopeprobe scope")
	if _, err := db.Exec(`UPDATE sessions SET model='synthetic-opus' WHERE uid!='scope-sibling'`); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "child"))
	return db, root
}

func TestHereRootResolution(t *testing.T) {
	for _, kind := range []string{"git-directory", "git-file", "nearest-git", "no-git"} {
		t.Run(kind, func(t *testing.T) {
			doctorHome(t)
			parent := hereDir(t, filepath.Join(t.TempDir(), "resolution-lab"))
			nested := hereDir(t, filepath.Join(parent, "nested", "leaf"))
			root := parent
			switch kind {
			case "git-directory":
				hereGit(t, parent, false)
			case "git-file":
				hereGit(t, parent, true)
			case "nearest-git":
				hereGit(t, parent, false)
				root = filepath.Dir(nested)
				hereGit(t, root, true)
			case "no-git":
				root = nested
			}
			db := hereDB(t)
			addSession(t, db, "resolution-inside", "claude", root, 0, "user", "resolutionprobe")
			addSession(t, db, "resolution-child", "claude", filepath.Join(root, "descendant"), 0, "user", "resolutionprobe")
			addSession(t, db, "resolution-outside", "claude", filepath.Dir(root), 0, "user", "resolutionprobe")
			t.Chdir(nested)
			p := roleCLI(t, "--here", "resolutionprobe")
			if p.Total != 2 || p.TotalSessions != 2 {
				t.Fatalf("resolved wrong root: %+v", p)
			}
		})
	}
	for _, place := range []string{"home", "filesystem-root"} {
		t.Run(place, func(t *testing.T) {
			home := doctorHome(t)
			dir := home
			if place == "filesystem-root" {
				dir = string(filepath.Separator)
			}
			t.Chdir(dir)
			for _, args := range [][]string{{"--here", "--json", "scopeprobe"}, {"stats", "--here", "--json"}} {
				out, err := runArgs(t, args...)
				if err == nil || !strings.Contains(err.Error(), "--here") || strings.Contains(err.Error(), "\n") || out != "" {
					t.Fatalf("must give a one-line usage error before opening index: out=%q err=%v", out, err)
				}
			}
		})
	}
}

func TestHereSearchCountsFacetsSessionsStats(t *testing.T) {
	_, root := hereFixture(t)
	for _, tc := range []struct {
		query                    string
		total, sessions, omitted int
	}{
		{"scopeprobe", 5, 3, 0}, {"sc", 4, 2, 1}, {"", 3, 3, 0},
	} {
		t.Run("query="+tc.query, func(t *testing.T) {
			p := roleCLI(t, "--here", "--facets", "--limit", "100", tc.query)
			if p.Total != tc.total || p.TotalSessions != tc.sessions || p.Shown != tc.total || p.OmittedOlder != tc.omitted {
				t.Fatalf("hits: %+v", p)
			}
			for _, h := range p.Hits {
				if !strings.HasPrefix(h.Ref, shortID("scope-root")+":") && !strings.HasPrefix(h.Ref, shortID("scope-child")+":") && !strings.HasPrefix(h.Ref, shortID("scope-old")+":") {
					t.Fatalf("out-of-scope hit: %+v", h)
				}
			}
			check := func(f *facets, want int, weeks bool) {
				t.Helper()
				if f == nil {
					t.Fatal("missing facets")
				}
				groups := [][]facetCount{f.Project, f.Harness}
				if weeks {
					groups = append(groups, f.Week)
				}
				for _, group := range groups {
					sum := 0
					for _, row := range group {
						sum += row.Count
					}
					if sum != want {
						t.Fatalf("facets=%+v want %d", f, want)
					}
				}
			}
			check(p.Facets, tc.total, true)
			s := modelSessions(t, "--here", "--facets", "--limit", "100", tc.query)
			if s.Total != tc.sessions || s.Shown != tc.sessions || s.OmittedOlder != tc.omitted {
				t.Fatalf("sessions: %+v", s)
			}
			check(s.Facets, tc.sessions, tc.query == "")
			hits := 0
			for _, row := range s.Sessions {
				hits += row.Hits
			}
			if hits != tc.total {
				t.Fatalf("session hits=%d want %d", hits, tc.total)
			}
		})
	}
	p := roleCLI(t, "--here", "--max-per-session", "1", "--facets", "scopeprobe")
	if p.Total != 3 || p.TotalSessions != 3 || p.Shown != 3 {
		t.Fatalf("cap: %+v", p)
	}
	p = roleCLI(t, "--here", "-p", filepath.Base(root), "--harness", "claude", "--model", "OPUS", "--role", "asst", "scopeprobe")
	if p.Total != 1 || p.TotalSessions != 1 {
		t.Fatalf("AND filters: %+v", p)
	}
	p = roleCLI(t, "--here", "--since", "2026-10-20", "scopeprobe")
	if p.Total != 4 {
		t.Fatalf("date AND scope: %+v", p)
	}
	p = roleCLI(t, "--here", "--all-time", "sc")
	if p.Total != 5 || p.OmittedOlder != 0 {
		t.Fatalf("all time: %+v", p)
	}
	out, err := runArgs(t, "--here", "scopeprobe", "--limit", "100")
	if err != nil || !strings.HasPrefix(out, "5/5 hits in 3 sessions\n") {
		t.Fatalf("text page: %q %v", out, err)
	}
	for _, tc := range []struct {
		args               []string
		sessions, messages int
	}{
		{nil, 3, 5}, {[]string{"--harness", "codex", "-p", "child", "--model", "OPUS"}, 1, 2}, {[]string{"--since", "2026-10-20"}, 2, 4},
	} {
		out, err := runArgs(t, append([]string{"stats", "--here", "--json"}, tc.args...)...)
		var r statsReport
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		if r.Total.Sessions != tc.sessions || r.Total.Messages != tc.messages {
			t.Fatalf("stats: %+v", r)
		}
		for _, group := range [][]statRow{r.Project, r.Harness} {
			sum := 0
			for _, row := range group {
				sum += row.Messages
			}
			if sum != tc.messages {
				t.Fatalf("stats groups: %+v", r)
			}
		}
	}
}

func TestHereLiteralPathsAndAliases(t *testing.T) {
	for _, name := range []string{"repo-a", "repo_%", "資料庫"} {
		t.Run(name, func(t *testing.T) {
			doctorHome(t)
			root := hereDir(t, filepath.Join(t.TempDir(), name))
			hereGit(t, root, false)
			db := hereDB(t)
			addSession(t, db, "literal-root", "claude", root, 0, "user", "literalprobe")
			addSession(t, db, "literal-child", "claude", root+"/child", 0, "user", "literalprobe")
			addSession(t, db, "literal-prefix-sibling", "claude", root+"b", 0, "user", "literalprobe")
			addSession(t, db, "literal-wildcard-decoy", "claude", filepath.Join(filepath.Dir(root), "repo-XX", "child"), 0, "user", "literalprobe")
			for _, suffix := range []string{"", "/", "/./"} {
				args, _ := json.Marshal(map[string]any{"name": "kioku_search", "arguments": map[string]any{"query": "literalprobe", "cwd": root + suffix}})
				out, err := mcpCall(args)
				if err != nil {
					t.Fatal(err)
				}
				var p hitPage
				if err = json.Unmarshal([]byte(out), &p); err != nil || p.Total != 2 {
					t.Fatalf("unclean/literal path: %+v %v", p, err)
				}
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		doctorHome(t)
		real := hereDir(t, filepath.Join(t.TempDir(), "physical-lab"))
		hereGit(t, real, false)
		alias := filepath.Join(t.TempDir(), "logical-lab")
		if err := os.Symlink(real, alias); err != nil {
			t.Fatal(err)
		}
		db := hereDB(t)
		physical, err := filepath.EvalSymlinks(real)
		if err != nil {
			t.Fatal(err)
		}
		addSession(t, db, "alias-logical", "claude", alias, 0, "user", "aliasprobe")
		addSession(t, db, "alias-physical", "claude", physical+"/child", 0, "user", "aliasprobe")
		t.Chdir(alias)
		t.Setenv("PWD", alias)
		p := roleCLI(t, "--here", "aliasprobe")
		if p.Total != 2 {
			t.Fatalf("given and EvalSymlinks paths must both match: %+v", p)
		}
		args, _ := json.Marshal(map[string]any{"name": "kioku_search", "arguments": map[string]any{"query": "aliasprobe", "cwd": alias}})
		out, err := mcpCall(args)
		if err != nil {
			t.Fatal(err)
		}
		var m hitPage
		if err = json.Unmarshal([]byte(out), &m); err != nil || !reflect.DeepEqual(p, m) {
			t.Fatalf("MCP alias: %+v %v", m, err)
		}
	})
}

func TestHereCursorsAndMCP(t *testing.T) {
	_, root := hereFixture(t)
	p := roleCLI(t, "--here", "--limit", "1", "scopeprobe")
	if p.NextCursor == "" {
		t.Fatal("fixture needs a next page")
	}
	next := roleCLI(t, "--here", "--limit", "1", "--cursor", p.NextCursor, "scopeprobe")
	if next.Shown != 1 || next.Hits[0].Ref == p.Hits[0].Ref {
		t.Fatalf("paging: %+v", next)
	}
	if _, err := runArgs(t, "--json", "--limit", "1", "--cursor", p.NextCursor, "scopeprobe"); err == nil {
		t.Fatal("unscoped cursor accepted")
	}
	other := hereDir(t, filepath.Join(t.TempDir(), "other-lab"))
	hereGit(t, other, false)
	t.Chdir(other)
	if _, err := runArgs(t, "--here", "--json", "--limit", "1", "--cursor", p.NextCursor, "scopeprobe"); err == nil {
		t.Fatal("cross-root cursor accepted")
	}
	t.Chdir(root)
	for _, tool := range []string{"kioku_search", "kioku_sessions"} {
		args := []string{"--here", "--json", "--limit", "1", "scopeprobe"}
		if tool == "kioku_sessions" {
			args = append(args, "--sessions")
		}
		cli, err := runArgs(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{"query": "scopeprobe", "cwd": root + "/child/./", "limit": 1}})
		mcp, err := mcpCall(raw)
		if err != nil || mcp != cli {
			t.Fatalf("MCP and CLI differ: error=%v\nCLI %s\nMCP %s", err, cli, mcp)
		}
		// The next CLI page can be requested through MCP with the exact same scope.
		if tool == "kioku_search" {
			raw, _ = json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{"query": "scopeprobe", "cwd": root, "limit": 1, "cursor": p.NextCursor}})
			mcp, err = mcpCall(raw)
			var got hitPage
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(mcp), &got); err != nil || !reflect.DeepEqual(got, next) {
				t.Fatalf("shared cursor page: %+v %v", got, err)
			}
		}
		unscoped, err := runArgs(t, "--json", "--limit", "1", "scopeprobe")
		if tool == "kioku_sessions" {
			unscoped, err = runArgs(t, "--json", "--sessions", "--limit", "1", "scopeprobe")
		}
		raw, _ = json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{"query": "scopeprobe", "limit": 1}})
		mcp, mcpErr := mcpCall(raw)
		if err != nil || mcpErr != nil || mcp != unscoped {
			t.Fatalf("no cwd must stay unscoped: %v %v", err, mcpErr)
		}
	}
	for _, cwd := range []any{"relative", "", nil, 42, os.Getenv("HOME"), "/", filepath.Join(root, "missing")} {
		raw, _ := json.Marshal(map[string]any{"name": "kioku_search", "arguments": map[string]any{"query": "scopeprobe", "cwd": cwd}})
		if _, err := mcpCall(raw); err == nil {
			t.Fatalf("invalid cwd accepted: %v", cwd)
		}
	}
	for _, args := range [][]string{{"show", "invented-ref", "--here"}, {"index", "--here"}} {
		if _, err := runArgs(t, args...); err == nil {
			t.Fatal("--here must be rejected outside search/stats")
		}
	}
}

func TestHereCommonWordPath(t *testing.T) {
	db, root := hereFixture(t)
	var msgs []string
	for i := 0; i < 320; i++ {
		msgs = append(msgs, "user", "commonhere")
	}
	addSession(t, db, "common-inside", "claude", root, 0, msgs...)
	addSession(t, db, "common-outside", "claude", root+"b", 0, msgs...)
	common, err := commonQuery(context.Background(), db, "commonhere", toFTS("commonhere"))
	if err != nil || !common {
		t.Fatalf("must exercise common-word fast path: %v %v", common, err)
	}
	p := roleCLI(t, "--here", "--limit", "350", "commonhere")
	if p.Total != 320 || p.TotalSessions != 1 || p.Shown != 320 {
		t.Fatalf("common hits: %+v", p)
	}
	for _, h := range p.Hits {
		if !strings.HasPrefix(h.Ref, shortID("common-inside")+":") {
			t.Fatalf("common path leaked: %+v", h)
		}
	}
}

func TestHereTUITogglePreservesState(t *testing.T) {
	db, root := hereFixture(t)
	rows, err := search(context.Background(), db, "scopeprobe", "all", nil, 100, false, false, false, lastTwentyDays)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(db, "scopeprobe", "all", nil, rows, false, 100, false)
	m.focus, m.syncing, m.rng = false, false, lastTwentyDays
	m.pens = []pen{{"probe", 1}}
	for i, h := range rows {
		if h.UID == "scope-child" && h.Role == "tool" {
			m.sel = i
		}
	}
	load := m.requestLoad()
	m = settle(t, m, load)
	selected := m.rows[m.sel].ID
	header := strings.Split(m.View(), "\n")[0]
	if strings.Contains(header, filepath.Base(root)) {
		t.Fatal("scope basename shown while off")
	}
	for _, on := range []bool{true, false} {
		updated, cmd := m.Update(keyRunes("p"))
		if cmd == nil {
			t.Fatal("p must re-run the search")
		}
		m = settle(t, updated.(model), cmd)
		wantRows := 6
		if on {
			wantRows = 4
		}
		if len(m.rows) != wantRows || m.rows[m.sel].ID != selected || m.q != "scopeprobe" || m.focus || m.rng != lastTwentyDays || !reflect.DeepEqual(m.pens, []pen{{"probe", 1}}) || len(m.hitIndex) == 0 {
			t.Fatalf("toggle lost state: rows=%d sel=%d q=%q focus=%v range=%+v pens=%v", len(m.rows), m.sel, m.q, m.focus, m.rng, m.pens)
		}
		h := strings.Split(m.View(), "\n")[0]
		if strings.Contains(h, filepath.Base(root)) != on {
			t.Fatalf("header scope on=%v: %s", on, h)
		}
		if !on && h != header {
			t.Fatalf("off header changed:\n%s\n%s", header, h)
		}
	}
	m.focus = true
	updated, _ := m.Update(keyRunes("p"))
	typed := updated.(model)
	if typed.q != "scopeprobep" || !typed.focus {
		t.Fatal("input must keep typed p")
	}
	m.focus = false
	m.help = true
	if !strings.Contains(m.View(), "p        ") {
		t.Fatal("help must document p")
	}
	t.Chdir(os.Getenv("HOME"))
	m.help = false
	updated, cmd := m.Update(keyRunes("p"))
	invalid := updated.(model)
	if cmd != nil || !strings.Contains(invalid.status, "--here") || invalid.sel != m.sel || invalid.q != m.q || invalid.focus != m.focus {
		t.Fatal("invalid root must show usage without changing state")
	}
}

func TestHereTUIWideBasename(t *testing.T) {
	doctorHome(t)
	root := hereDir(t, filepath.Join(t.TempDir(), "專案資料庫"))
	hereGit(t, root, false)
	t.Chdir(root)
	m := newModel(ctxDB(t), "", "all", nil, nil, false, 100, false)
	m.focus, m.syncing = false, false
	updated, cmd := m.Update(keyRunes("p"))
	m = settle(t, updated.(model), cmd)
	for _, width := range []int{25, 40, 80, 120} {
		m.width = width
		header := strings.Split(m.View(), "\n")[0]
		if displayWidth(header) != width {
			t.Fatalf("CJK header width=%d want=%d: %q", displayWidth(header), width, header)
		}
		if width >= 80 && !strings.Contains(header, filepath.Base(root)) {
			t.Fatalf("missing CJK basename: %s", header)
		}
	}
}

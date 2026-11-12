package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func modelFixture(t *testing.T) {
	t.Helper()
	home := doctorHome(t)
	t.Setenv("KIOKU_INDEX", filepath.Join(home, "model-index.db"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	for n, s := range []struct {
		uid, model, harness, project string
		messages                     int
		old                          bool
	}{
		{"model-alpha", "claude-OPUS-4", "pi", "model-lab", 6, false},
		{"model-beta", "prefix-opus-suffix", "claude", "model-lab", 3, false},
		{"model-decoy", "sonnet", "pi", "model-decoys", 6, false},
		{"model-empty", "", "pi", "model-decoys", 3, false},
		{"model-old", "Opus", "pi", "model-archive", 1, true},
		{"model-old-decoy", "sonnet", "pi", "model-decoys", 1, true},
		{"model-percent", "literal%model", "pi", "model-symbols", 1, false},
		{"model-underscore", "literal_model", "pi", "model-symbols", 1, false},
		{"model-quote", "literal' OR 1=1 --", "pi", "model-symbols", 1, false},
		{"model-backslash", `literal\model`, "pi", "model-symbols", 1, false},
	} {
		if _, err := db.Exec(`INSERT INTO sessions(uid,harness,native_id,path,cwd,project,model) VALUES(?,?,?,'/synthetic/model-session','/synthetic/model-workspace',?,?)`, s.uid, s.harness, s.uid, s.project, s.model); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().UTC().Add(time.Duration(n) * time.Second)
		if s.old {
			stamp = stamp.AddDate(0, 0, -30)
		}
		for i := 0; i < s.messages; i++ {
			if _, err := db.Exec(`INSERT INTO messages(session_uid,idx,ts,role,text) VALUES(?,?,?,?,?)`, s.uid, i, stamp.Format(time.RFC3339Nano), []string{"user", "asst", "tool"}[i%3], fmt.Sprintf("modelprobe model --model message %d", i)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func modelSessions(t *testing.T, args ...string) sessionPage {
	t.Helper()
	out, err := runArgs(t, append([]string{"--json", "--sessions"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	var p sessionPage
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func modelCheckHits(t *testing.T, p hitPage) {
	t.Helper()
	for _, h := range p.Hits {
		valid := false
		for _, uid := range []string{"model-alpha", "model-beta", "model-old"} {
			valid = valid || strings.HasPrefix(h.Ref, shortID(uid)+":")
		}
		if !valid {
			t.Fatalf("nonmatching model hit: %+v", h)
		}
	}
}

func TestModelSearchCountsAndCase(t *testing.T) {
	modelFixture(t)
	for _, value := range []string{"opus", "OPUS", "pUs"} {
		p := roleCLI(t, "--model", value, "modelprobe", "--limit", "100")
		if p.Total != 10 || p.TotalSessions != 3 || p.Shown != 10 {
			t.Fatalf("model %q: %+v", value, p)
		}
		modelCheckHits(t, p)
	}
	out, err := runArgs(t, "modelprobe", "--model=opus", "--limit", "100")
	if err != nil || !strings.HasPrefix(out, "10/10 hits in 3 sessions\n") || strings.Contains(out, "model-decoys") {
		t.Fatalf("text search: %q, %v", out, err)
	}
	p := roleCLI(t, "modelprobe", "--model", "absent")
	if p.Total != 0 || p.TotalSessions != 0 || p.Shown != 0 {
		t.Fatalf("no matches: %+v", p)
	}
	p = roleCLI(t, "modelprobe", "--model", "opus", "-p", "model-lab", "--harness", "pi", "--role", "user,asst")
	if p.Total != 4 || p.TotalSessions != 1 || p.Shown != 4 {
		t.Fatalf("combined filters: %+v", p)
	}
}

func TestModelSessionsFacetsAndOmitted(t *testing.T) {
	modelFixture(t)
	for _, query := range []string{"modelprobe", "mo", ""} {
		p := roleCLI(t, query, "--model", "opus", "--facets", "--limit", "100")
		want, sessions, omitted := 10, 3, 0
		if query == "mo" {
			want, sessions, omitted = 9, 2, 1
		} else if query == "" {
			want = 3
		}
		if p.Total != want || p.TotalSessions != sessions || p.Shown != want || p.OmittedOlder != omitted || p.Facets == nil {
			t.Fatalf("query %q: %+v", query, p)
		}
		modelCheckHits(t, p)
		for _, group := range [][]facetCount{p.Facets.Project, p.Facets.Harness, p.Facets.Week} {
			sum := 0
			for _, f := range group {
				sum += f.Count
			}
			if sum != want {
				t.Fatalf("query %q facets: %+v, want sum %d", query, p.Facets, want)
			}
		}
		s := modelSessions(t, query, "--model", "opus", "--facets", "--limit", "100")
		if s.Total != sessions || s.Shown != sessions || s.OmittedOlder != omitted || s.Facets == nil {
			t.Fatalf("query %q sessions: %+v", query, s)
		}
		hits := 0
		for _, session := range s.Sessions {
			hits += session.Hits
			if session.Roles.You+session.Roles.Asst+session.Roles.Tool != session.Hits || !strings.HasPrefix(session.Project, "model-") || session.Project == "model-decoys" {
				t.Fatalf("session counts: %+v", session)
			}
		}
		if hits != want {
			t.Fatalf("session hits=%d, want %d", hits, want)
		}
		for _, group := range [][]facetCount{s.Facets.Project, s.Facets.Harness} {
			sum := 0
			for _, f := range group {
				sum += f.Count
			}
			if sum != sessions {
				t.Fatalf("session facets: %+v", s.Facets)
			}
		}
	}
	p := roleCLI(t, "mo", "--model", "opus", "--all-time", "--limit", "100")
	if p.Total != 10 || p.OmittedOlder != 0 {
		t.Fatalf("all-time: %+v", p)
	}
}

func TestModelPagingMCPAndCursor(t *testing.T) {
	modelFixture(t)
	for _, sessions := range []bool{false, true} {
		for _, query := range []string{"modelprobe", "mo", ""} {
			cursor := ""
			seen := map[string]bool{}
			want := 10
			if query == "mo" {
				want = 9
			}
			if sessions || query == "" {
				want = 3
				if query == "mo" {
					want = 2
				}
			}
			for page := 0; page < want; page++ {
				args := []string{"--json", query, "--model", "opus", "--limit", "2"}
				name := "kioku_search"
				if sessions {
					args = append(args, "--sessions")
					name = "kioku_sessions"
				}
				mcpArgs := map[string]any{"query": query, "model": "opus", "limit": 2}
				if cursor != "" {
					args = append(args, "--cursor", cursor)
					mcpArgs["cursor"] = cursor
				}
				cli, err := runArgs(t, args...)
				if err != nil {
					t.Fatal(err)
				}
				params, _ := json.Marshal(map[string]any{"name": name, "arguments": mcpArgs})
				mcp, err := mcpCall(params)
				if err != nil || mcp != cli {
					t.Fatalf("CLI/MCP %s %q: %v\nCLI %s\nMCP %s", name, query, err, cli, mcp)
				}
				var p hitPage
				if err := json.Unmarshal([]byte(cli), &p); err != nil {
					t.Fatal(err)
				}
				refs := []string{}
				if sessions {
					var s sessionPage
					if err := json.Unmarshal([]byte(cli), &s); err != nil {
						t.Fatal(err)
					}
					for _, session := range s.Sessions {
						refs = append(refs, session.Ref)
					}
				} else {
					modelCheckHits(t, p)
					for _, h := range p.Hits {
						refs = append(refs, h.Ref)
					}
				}
				if p.Total != want || p.Shown != min(2, want-len(seen)) || len(refs) != p.Shown {
					t.Fatalf("short or wrong page %s %q: %s", name, query, cli)
				}
				for _, ref := range refs {
					if seen[ref] {
						t.Fatalf("duplicate ref %s", ref)
					}
					seen[ref] = true
				}
				if page == 0 && p.NextCursor != "" {
					bad := append(append([]string{}, args...), "--model", "sonnet", "--cursor", p.NextCursor)
					if _, err := runArgs(t, bad...); err == nil || !strings.Contains(err.Error(), "cursor does not match") {
						t.Fatalf("different model cursor accepted: %v", err)
					}
					mcpArgs["model"], mcpArgs["cursor"] = "sonnet", p.NextCursor
					params, _ = json.Marshal(map[string]any{"name": name, "arguments": mcpArgs})
					if _, err := mcpCall(params); err == nil || !strings.Contains(err.Error(), "cursor does not match") {
						t.Fatalf("MCP different model cursor accepted: %v", err)
					}
				}
				cursor = p.NextCursor
				if cursor == "" {
					break
				}
			}
			if len(seen) != want || cursor != "" {
				t.Fatalf("paging sessions=%v %q: %d/%d, cursor remains=%v", sessions, query, len(seen), want, cursor != "")
			}
		}
	}
}

func TestModelLiteralsValidationAndParsing(t *testing.T) {
	modelFixture(t)
	for value, uid := range map[string]string{"%": "model-percent", "_": "model-underscore", "' OR 1=1 --": "model-quote", `\`: "model-backslash"} {
		p := roleCLI(t, "modelprobe", "--model", value)
		if p.Total != 1 || p.TotalSessions != 1 || len(p.Hits) != 1 || !strings.HasPrefix(p.Hits[0].Ref, shortID(uid)+":") {
			t.Fatalf("literal %q: %+v", value, p)
		}
	}
	p := roleCLI(t, "modelprobe", "--model", "' OR 1=1; DROP TABLE sessions;--")
	if p.Total != 0 {
		t.Fatalf("SQL injection matched rows: %+v", p)
	}
	for _, args := range [][]string{{"--model"}, {"--model", ""}, {"--model="}, {"stats", "--model"}, {"stats", "--model="}, {"show", "model-alpha:0", "--model", "opus"}, {"index", "--model", "opus"}} {
		if _, err := runArgs(t, args...); err == nil {
			t.Fatalf("invalid model flags accepted: %v", args)
		}
	}
	for _, tool := range mcpTools() {
		if tool.Name != "kioku_search" && tool.Name != "kioku_sessions" {
			continue
		}
		property := tool.InputSchema.Properties["model"]
		if property["type"] != "string" || property["minLength"] != 1 {
			t.Fatalf("model schema on %s: %+v", tool.Name, property)
		}
		for _, value := range []any{"", 12, true, nil} {
			params, _ := json.Marshal(map[string]any{"name": tool.Name, "arguments": map[string]any{"query": "modelprobe", "model": value}})
			if _, err := mcpCall(params); err == nil {
				t.Fatalf("MCP %s accepted model %#v", tool.Name, value)
			}
		}
	}
	for _, args := range [][]string{{"model"}, {"--", "--model"}} {
		p := roleCLI(t, args...)
		if p.Total != 24 {
			t.Fatalf("query text parsed as flag: %v: %+v", args, p)
		}
	}
}

func TestModelCapAndCommonWord(t *testing.T) {
	modelFixture(t)
	check := func() {
		t.Helper()
		for _, cap := range []int{1, 2} {
			p := roleCLI(t, "modelprobe", "--model", "opus", "--max-per-session", fmt.Sprint(cap), "--facets", "--limit", "100")
			want := 2*cap + 1
			if p.Total != want || p.TotalSessions != 3 || p.Shown != want || p.Facets == nil {
				t.Fatalf("cap %d: %+v", cap, p)
			}
			modelCheckHits(t, p)
			mcp := roleMCP(t, map[string]any{"query": "modelprobe", "model": "opus", "max_per_session": cap, "facets": true, "limit": 100})
			if !reflect.DeepEqual(p, mcp) {
				t.Fatalf("capped CLI/MCP differ: CLI=%+v MCP=%+v", p, mcp)
			}
			counts := map[string]int{}
			for _, h := range p.Hits {
				uid, _, _ := strings.Cut(h.Ref, ":")
				counts[uid]++
				if counts[uid] > cap {
					t.Fatalf("cap exceeded: %+v", p)
				}
			}
			sum := 0
			for _, f := range p.Facets.Project {
				sum += f.Count
			}
			if sum != want {
				t.Fatalf("capped facets: %+v", p.Facets)
			}
		}
		p := roleCLI(t, "mo", "--model", "opus", "--max-per-session", "1")
		if p.Total != 2 || p.TotalSessions != 2 || p.OmittedOlder != 1 {
			t.Fatalf("capped short prefix: %+v", p)
		}
	}
	check()
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`WITH RECURSIVE n(x) AS (VALUES(6) UNION ALL SELECT x+1 FROM n WHERE x<325)
INSERT INTO messages(session_uid,idx,ts,role,text) SELECT 'model-decoy',x,?,'user','modelprobe' FROM n`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	common, err := commonQuery(context.Background(), db, "modelprobe", toFTS("modelprobe"))
	if err != nil || !common {
		t.Fatalf("common-word fixture: %v, %v", common, err)
	}
	check()
	for _, role := range []string{"", "user,asst", "tool"} {
		cursor := ""
		seen := map[string]bool{}
		want := 10
		if role == "user,asst" {
			want = 7
		} else if role == "tool" {
			want = 3
		}
		for page := 0; page < want; page++ {
			args := []string{"modelprobe", "--model", "opus", "--limit", "2"}
			mcpArgs := map[string]any{"query": "modelprobe", "model": "opus", "limit": 2}
			if role != "" {
				args = append(args, "--role", role)
				mcpArgs["role"] = role
			}
			if cursor != "" {
				args = append(args, "--cursor", cursor)
				mcpArgs["cursor"] = cursor
			}
			p := roleCLI(t, args...)
			if !reflect.DeepEqual(p, roleMCP(t, mcpArgs)) || p.Total != want || p.Shown != min(2, want-len(seen)) {
				t.Fatalf("common page role %q: %+v", role, p)
			}
			modelCheckHits(t, p)
			for _, h := range p.Hits {
				if seen[h.Ref] {
					t.Fatalf("duplicate common hit %s", h.Ref)
				}
				seen[h.Ref] = true
			}
			cursor = p.NextCursor
			if cursor == "" {
				break
			}
		}
		if len(seen) != want || cursor != "" {
			t.Fatalf("common paging: %d/%d", len(seen), want)
		}
	}
}

func TestModelStats(t *testing.T) {
	modelFixture(t)
	for _, value := range []string{"opus", "OPUS", "pUs"} {
		out, err := runArgs(t, "stats", "--json", "--model", value)
		if err != nil {
			t.Fatal(err)
		}
		var r statsReport
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		if r.Total.Sessions != 3 || r.Total.Messages != 10 {
			t.Fatalf("stats %q: %+v", value, r)
		}
		for _, group := range [][]statRow{r.Harness, r.Project} {
			sessions, messages := 0, 0
			for _, row := range group {
				sessions += row.Sessions
				messages += row.Messages
			}
			if sessions != 3 || messages != 10 {
				t.Fatalf("stats groups: %+v", r)
			}
		}
	}
	out, err := runArgs(t, "stats", "--model=opus", "-p", "model-lab", "--harness", "pi", "--since", "7d")
	if err != nil || !strings.HasPrefix(out, "1 sessions · 6 messages") {
		t.Fatalf("filtered text stats: %q, %v", out, err)
	}
	for _, value := range []string{"%", "_", "' OR 1=1 --", `\`, "absent"} {
		out, err := runArgs(t, "stats", "--json", "--model", value)
		if err != nil {
			t.Fatal(err)
		}
		var r statsReport
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		want := 1
		if value == "absent" {
			want = 0
		}
		if r.Total.Messages != want || r.Total.Sessions != want {
			t.Fatalf("stats literal %q: %+v", value, r)
		}
	}
}

func TestModelAbsentPreservesValidationOutput(t *testing.T) {
	const want = "--since, --until, --facets, --max-per-session and --role apply to search and --sessions"
	for _, args := range [][]string{{"show", "model-alpha:0", "--role", "user"}, {"index", "--facets"}} {
		if _, err := runArgs(t, args...); err == nil || err.Error() != want {
			t.Fatalf("unfiltered validation changed: %v: %v", args, err)
		}
	}
}

func TestModelAbsentPreservesCursorFingerprint(t *testing.T) {
	// This is the pre-model page-key serialization; existing cursors must remain valid.
	const before = `{"Mode":"hits","Query":"modelprobe","Harness":"all","Ref":"","Projects":null,"Limit":2,"Context":3,"JSON":true,"AllTime":false,"All":false,"Full":false,"NoMouse":false,"Rebuild":false}`
	k := pageKey{Mode: "hits", Query: "modelprobe", Harness: "all", Limit: 2, Context: 3, JSON: true}
	sum := sha256.Sum256([]byte(before))
	if got, want := k.fingerprint(), hex.EncodeToString(sum[:16]); got != want {
		t.Fatalf("unfiltered cursor fingerprint changed: got %s want %s", got, want)
	}
}

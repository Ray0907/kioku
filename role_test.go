package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func roleFixture(t *testing.T) {
	t.Helper()
	doctorHome(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	for n, name := range []string{"alpha", "beta"} {
		_, err = db.Exec(`INSERT INTO sessions(uid,harness,native_id,path,cwd,project) VALUES(?, 'pi', ?, '/synthetic', '/w/roles', 'roles')`, name, name)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 6-n*3; i++ {
			_, err = db.Exec(`INSERT INTO messages(session_uid,idx,ts,role,text) VALUES(?,?,?,?,?)`, name, i, time.Now().UTC().Format(time.RFC3339), []string{"user", "asst", "tool"}[i%3], fmt.Sprintf("roleprobe message %d", i))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func roleCLI(t *testing.T, args ...string) hitPage {
	t.Helper()
	out, err := runArgs(t, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	var p hitPage
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return p
}

func roleMCP(t *testing.T, args map[string]any) hitPage {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": "kioku_search", "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	out, err := mcpCall(params)
	if err != nil {
		t.Fatal(err)
	}
	var p hitPage
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return p
}

func TestRoleCLIAndMCPPaging(t *testing.T) {
	roleFixture(t)
	for _, query := range []string{"roleprobe", "ro", ""} {
		for _, role := range []string{"user", "asst", "tool", "user,asst", "user,tool", "user,asst,tool"} {
			t.Run(query+"/"+role, func(t *testing.T) {
				want := 3 * len(strings.Split(role, ","))
				if query == "" {
					want = 2 // Browse returns the newest matching message per session.
				}
				seen := map[string]bool{}
				cursor := ""
				for page := 0; page <= want; page++ {
					args := []string{query, "--role", role, "--limit", "1"}
					mcpArgs := map[string]any{"query": query, "role": role, "limit": 1}
					if cursor != "" {
						args = append(args, "--cursor", cursor)
						mcpArgs["cursor"] = cursor
					}
					p := roleCLI(t, args...)
					mcp := roleMCP(t, mcpArgs)
					a, _ := json.Marshal(p)
					b, _ := json.Marshal(mcp)
					if string(a) != string(b) {
						t.Fatalf("CLI/MCP differ:\n%s\n%s", a, b)
					}
					if p.Total != want || len(p.Hits) != 1 {
						t.Fatalf("total=%d hits=%d, want %d and 1", p.Total, len(p.Hits), want)
					}
					h := p.Hits[0]
					if !strings.Contains(","+role+",", ","+h.Role+",") || seen[h.Ref] {
						t.Fatalf("wrong role or repeated hit: %+v", h)
					}
					seen[h.Ref] = true
					cursor = p.NextCursor
					if cursor == "" {
						break
					}
				}
				if len(seen) != want || cursor != "" {
					t.Fatalf("paging: got %d unique hits, want %d, cursor=%q", len(seen), want, cursor)
				}
			})
		}
	}
}

func TestRoleCountsFacetsAndCap(t *testing.T) {
	roleFixture(t)
	for _, query := range []string{"roleprobe", "ro"} {
		p := roleCLI(t, query, "--role=user,asst", "--facets", "--since", "2000-01-01", "--until", "2100-01-01")
		if p.Total != 6 || p.TotalSessions != 2 || p.Facets == nil || len(p.Facets.Project) != 1 || p.Facets.Project[0].Count != 6 {
			t.Fatalf("filtered counts/facets: %+v", p)
		}
		cap := roleCLI(t, query, "--role=tool", "--max-per-session", "1")
		if cap.Total != 2 || len(cap.Hits) != 2 {
			t.Fatalf("filtered cap: %+v", cap)
		}
		out, err := runArgs(t, "--json", "--sessions", query, "--role", "asst")
		if err != nil {
			t.Fatal(err)
		}
		var sessions sessionPage
		if err := json.Unmarshal([]byte(out), &sessions); err != nil {
			t.Fatal(err)
		}
		if sessions.Total != 2 || len(sessions.Sessions) != 2 || sessions.Sessions[0].Hits+sessions.Sessions[1].Hits != 3 {
			t.Fatalf("session counts: %+v", sessions)
		}
		for _, s := range sessions.Sessions {
			if s.Roles.You != 0 || s.Roles.Tool != 0 || s.Roles.Asst != s.Hits {
				t.Fatalf("session role counts: %+v", s)
			}
		}
	}
	p := roleCLI(t, "roleprobe", "--role", "user", "--limit", "1")
	if _, err := runArgs(t, "--json", "roleprobe", "--role", "tool", "--limit", "1", "--cursor", p.NextCursor); err == nil {
		t.Fatal("cursor accepted a different role filter")
	}
}

func TestRoleSessionBrowsePaging(t *testing.T) {
	roleFixture(t)
	for _, role := range []string{"user", "asst", "tool"} {
		cursor := ""
		seen := map[string]bool{}
		for i := 0; i < 2; i++ {
			args := []string{"--json", "--sessions", "--role", role, "--limit", "1"}
			if cursor != "" {
				args = append(args, "--cursor", cursor)
			}
			out, err := runArgs(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			var p sessionPage
			if err := json.Unmarshal([]byte(out), &p); err != nil {
				t.Fatal(err)
			}
			if p.Total != 2 || len(p.Sessions) != 1 {
				t.Fatalf("browse %s: %+v", role, p)
			}
			s := p.Sessions[0]
			counts := map[string]int{"user": s.Roles.You, "asst": s.Roles.Asst, "tool": s.Roles.Tool}
			if counts[role] != 1 || seen[s.Ref] {
				t.Fatalf("browse %s: wrong role or repeated session: %+v", role, s)
			}
			seen[s.Ref] = true
			cursor = p.NextCursor
			if (i == 0) != (cursor != "") {
				t.Fatalf("browse cursor on page %d: %q", i, cursor)
			}
		}
	}
}

func TestRoleCommonWord(t *testing.T) {
	roleFixture(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`WITH RECURSIVE n(x) AS (VALUES(6) UNION ALL SELECT x+1 FROM n WHERE x<305)
INSERT INTO messages(session_uid,idx,ts,role,text) SELECT 'alpha',x,?,'tool','roleprobe' FROM n`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	common, err := commonQuery(context.Background(), db, "roleprobe", toFTS("roleprobe"))
	if err != nil || !common {
		t.Fatalf("fixture must exercise common-word path: %v, %v", common, err)
	}
	for _, role := range []string{"user", "asst", "user,asst", "tool"} {
		p := roleCLI(t, "roleprobe", "--role", role, "--limit", "2", "-p", "roles", "--harness", "pi")
		want := 3 * len(strings.Split(role, ","))
		if role == "tool" {
			want += 300
		}
		if p.Total != want || len(p.Hits) != 2 || p.NextCursor == "" {
			t.Fatalf("common-word %s: %+v", role, p)
		}
		for _, h := range p.Hits {
			if !strings.Contains(","+role+",", ","+h.Role+",") {
				t.Fatalf("common-word %s leaked hit: %+v", role, h)
			}
		}
		mcp := roleMCP(t, map[string]any{"query": "roleprobe", "role": role, "limit": 2, "project": "roles", "harness": "pi"})
		if mcp.Total != p.Total || mcp.Hits[0].Ref != p.Hits[0].Ref {
			t.Fatalf("common-word MCP differs: %+v", mcp)
		}
	}
}

func TestRoleValidationAndSchema(t *testing.T) {
	roleFixture(t)
	for _, role := range []string{"", "assistant", "USER", "user,", ",tool", "user,,asst", "user,nope", "tool'); DROP TABLE messages;--"} {
		t.Run(role, func(t *testing.T) {
			_, cliErr := runArgs(t, "--json", "roleprobe", "--role", role)
			params, _ := json.Marshal(map[string]any{"name": "kioku_search", "arguments": map[string]any{"query": "roleprobe", "role": role}})
			_, mcpErr := mcpCall(params)
			for _, err := range []error{cliErr, mcpErr} {
				if err == nil || !strings.Contains(err.Error(), "user") || !strings.Contains(err.Error(), "asst") || !strings.Contains(err.Error(), "tool") {
					t.Errorf("role %q: want error with valid list, got %v", role, err)
				}
			}
		})
	}
	if _, err := runArgs(t, "roleprobe", "--role"); err == nil {
		t.Error("missing role value accepted")
	}
	for _, args := range [][]string{{"show", "alpha:0", "--role", "user"}, {"index", "--role", "user"}} {
		if _, err := runArgs(t, args...); err == nil {
			t.Errorf("role silently ignored: %v", args)
		}
	}
	p := roleCLI(t, "--role=tool", "roleprobe")
	if p.Total != 3 {
		t.Fatalf("role before query: %+v", p)
	}
	for _, tool := range mcpTools() {
		if tool.Name == "kioku_search" && tool.InputSchema.Properties["role"] == nil {
			t.Error("role absent from search schema")
		}
	}
	// Bad role input must not damage the index.
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p2, err := compactSearch(context.Background(), db, pageKey{Query: "roleprobe", Limit: 10}, 0)
	if err != nil || p2.Total != 9 {
		t.Fatalf("unfiltered search after invalid input: %+v, %v", p2, err)
	}
}

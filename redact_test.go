package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

// All credentials are invented at runtime, never copied from a transcript.
func redactSamples() []struct{ name, text, want string } {
	samples := []struct{ name, text, want string }{}
	add := func(name, text string) {
		samples = append(samples, struct{ name, text, want string }{name, text, "****"})
	}
	for _, label := range []string{"PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY", "DSA PRIVATE KEY", "OPENSSH PRIVATE KEY", "ENCRYPTED PRIVATE KEY"} {
		begin := "-----BEGIN " + label + "-----\n"
		end := "-----END " + label + "-----"
		add("pem/"+label, begin+"invented body\n"+end)
		add("pem-crlf/"+label, strings.ReplaceAll(begin+"invented body\n"+end, "\n", "\r\n"))
	}
	for _, prefix := range []string{"sk-", "sk-ant-", "sk-proj-"} {
		add(prefix, prefix+strings.Repeat("aB3_", 8)+"-tail")
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"} {
		add(prefix, prefix+strings.Repeat("aB3", 16))
	}
	for _, prefix := range []string{"AKIA", "ASIA"} {
		add(prefix, prefix+strings.Repeat("A3", 8))
	}
	for _, prefix := range []string{"xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-"} {
		add(prefix, prefix+strings.Repeat("123-", 6)+"invented")
	}
	add("google", "AIza"+strings.Repeat("a", 35))
	for _, prefix := range []string{"sk_live_", "rk_live_"} {
		add(prefix, prefix+strings.Repeat("Ab3", 12))
	}
	add("jwt", "eyJ"+strings.Repeat("a", 8)+"."+strings.Repeat("b", 16)+"."+strings.Repeat("c", 16))
	for _, prefix := range []string{"Authorization: Bearer ", "authorization:\tbeAREr\t", "AUTHORIZATION: bEaReR "} {
		samples = append(samples, struct{ name, text, want string }{"bearer/" + prefix, prefix + strings.Repeat("aB3_", 10) + ".x/~+=", prefix + "****"})
	}
	return samples
}

func TestRedactPatterns(t *testing.T) {
	for _, tc := range redactSamples() {
		t.Run(tc.name, func(t *testing.T) {
			text := "before [" + tc.text + "] after"
			if got := redact(text); got != "before ["+tc.want+"] after" {
				t.Fatal("credential or surrounding text was not handled correctly")
			}
			if got := redact(text + "; " + text); got != "before ["+tc.want+"] after; before ["+tc.want+"] after" {
				t.Fatal("multiple credentials were not handled correctly")
			}
			prefix := strings.Repeat("前", 70) + " "
			if compactSnippet(prefix+tc.text+" after", "") != compactSnippet(prefix+tc.want+" after", "") {
				t.Fatal("snippet was cut before redaction")
			}
		})
	}
}

func TestRedactMinimumTokenLengths(t *testing.T) {
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-"} {
		if redact("before "+prefix+strings.Repeat("a", 20)+" after") != "before **** after" {
			t.Errorf("minimum-length token with prefix %q was not redacted", prefix)
		}
	}
}

func TestRedactTruncatedPEM(t *testing.T) {
	begin := "-----BEGIN " + "RSA PRIVATE KEY-----\n"
	wrongEnd := "-----END " + "EC PRIVATE KEY-----\n"
	end := "-----END " + "RSA PRIVATE KEY-----"
	for _, text := range []string{begin + "invented body", begin + "invented body\n-----EN", begin + "body\n" + wrongEnd + "still private"} {
		if redact("before\n"+text) != "before\n****" {
			t.Error("truncated PEM was not redacted through end of text")
		}
	}
	if redact("before\n"+begin+"body\n"+wrongEnd+"still private\n"+end+"\nafter") != "before\n****\nafter" {
		t.Error("PEM must end at its matching END line")
	}
}

func TestRedactNearMissesAndNoAllocation(t *testing.T) {
	texts := []string{
		"", "ordinary 中文🙂 prose and code\nwith tabs\tand spaces  ",
		"sk-" + "short", "skiing", "identifier_AKIAFOO_suffix",
		"prefix" + "AKIA" + strings.Repeat("A3", 8) + "suffix",
		"1.22.333", strings.Repeat("a1", 20), "12345678-1234-1234-1234-123456789abc",
		"data:image/png;base64," + strings.Repeat("abcd", 12) + ".abcd.abcd",
		"sk-" + strings.Repeat("a", 19),
		"task-management-system-design", "risk-assessment-framework-v2",
		"laughs_total", "sighs_count", "boxoxb-1",
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-"} {
		texts = append(texts, prefix+strings.Repeat("a", 19))
	}
	for _, sample := range redactSamples() {
		if strings.HasPrefix(sample.name, "pem") {
			continue
		}
		texts = append(texts, "identifier"+sample.text, "identifier_"+sample.text)
	}
	for i, text := range texts {
		got := redact(text)
		if got != text || unsafe.StringData(got) != unsafe.StringData(text) {
			t.Errorf("near-miss %d changed or copied", i)
		}
	}
	text := strings.Repeat("ordinary 中文🙂 prose: no authorization header\n", 100)
	if allocs := testing.AllocsPerRun(20, func() { _ = redact(text) }); allocs != 0 {
		t.Errorf("credential-free text allocated: %g", allocs)
	}
}

func TestRedactLargeToolOutput(t *testing.T) {
	text := strings.Repeat("ordinary tool output line\n", (1<<20)/26+1)
	_ = redact(text)
	start := time.Now()
	got := redact(text)
	elapsed := time.Since(start)
	if got != text || elapsed >= 100*time.Millisecond {
		t.Fatalf("1 MB credential-free output: unchanged=%v, duration=%s (want <100ms)", got == text, elapsed)
	}
	t.Logf("1 MB credential-free output: %s", elapsed)
}

func redactFixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	doctorHome(t)
	db := ctxDB(t)
	if _, err := syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "project")
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	key := "sk-" + "uniquematch" + strings.Repeat("aB3", 210) + "tail"
	text := "marker 中文🙂 " + key + " needle\nlastline"
	addSession(t, db, "redact-fixture", "claude", dir, time.Hour,
		"user", text, "tool", text, "asst", text)
	now := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE sessions SET started=?,updated=?; UPDATE messages SET ts=?`, now, now, now); err != nil {
		t.Fatal(err)
	}
	return db, key, text
}

func TestRedactCLIOutputsAndOptOut(t *testing.T) {
	_, key, _ := redactFixture(t)
	for _, args := range [][]string{
		{"marker"}, {"--json", "marker"}, {"--sessions", "marker"}, {"--sessions", "--json", "marker"},
		{"show", "redact-fixture:0"}, {"show", "redact-fixture:0", "--json"},
		{"show", "redact-fixture:0", "--full"}, {"show", "redact-fixture:0", "--full", "--json"},
		{"show", "redact-fixture:0", "--format", "md"}, {"show", "redact-fixture:0", "--full", "--format", "md"},
		{"show", "redact-fixture", "--outline"}, {"show", "redact-fixture", "--outline", "--json"},
		{"show", "redact-fixture", "--outline", "--format", "md"}, {"context"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			got, err := runArgs(t, args...)
			if err != nil || !strings.Contains(got, "****") || strings.Contains(got, key[:30]) || strings.Contains(got, key[len(key)-30:]) {
				t.Fatalf("output must redact before truncation (error=%v)", err)
			}
			raw, err := runArgs(t, append(append([]string{}, args...), "--no-redact")...)
			if err != nil || raw == got || strings.Contains(raw, "****") {
				t.Fatalf("--no-redact must restore original output (error=%v)", err)
			}
			// Legacy compact snippets can drop an entire long word; full output must keep it.
			if strings.Contains(strings.Join(args, " "), "--full") && !strings.Contains(raw, key) {
				t.Fatal("--no-redact --full lost the original credential")
			}
			// The flag is per invocation, not sticky across calls in the same process.
			again, err := runArgs(t, args...)
			if err != nil || again != got {
				t.Fatal("opt-out leaked into the next invocation")
			}
		})
	}
}

func TestRedactMCPOutputsAndNoOptOut(t *testing.T) {
	_, key, _ := redactFixture(t)
	for _, tool := range mcpTools() {
		t.Run(tool.Name, func(t *testing.T) {
			args := map[string]any{}
			switch tool.Name {
			case "kioku_search", "kioku_sessions":
				args["query"] = "marker"
			case "kioku_show":
				args["ref"], args["full"] = "redact-fixture:0", true
			}
			call := func() (string, error) {
				params, err := json.Marshal(map[string]any{"name": tool.Name, "arguments": args})
				if err != nil {
					t.Fatal(err)
				}
				return mcpCall(params)
			}
			got, err := call()
			if err != nil || !strings.Contains(got, "****") || strings.Contains(got, key[:30]) {
				t.Fatalf("MCP result not redacted (error=%v)", err)
			}
			if tool.Name == "kioku_show" {
				delete(args, "full")
				args["outline"] = true
				got, err = call()
				if err != nil || !strings.Contains(got, "****") || strings.Contains(got, key[:30]) {
					t.Fatal("MCP outline not redacted")
				}
			}
			for _, flag := range []string{"no_redact", "no-redact", "NoRedact"} {
				args[flag] = true
				if _, err := call(); err == nil {
					t.Errorf("MCP accepted %s", flag)
				}
				delete(args, flag)
			}
			if tool.InputSchema.AdditionalProperties {
				t.Error("MCP allows extra arguments")
			}
		})
	}
	params, _ := json.Marshal(map[string]any{"name": "kioku_show", "arguments": map[string]any{"ref": "missing-" + key}})
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params)})
	response, _ := json.Marshal(mcpHandle(request))
	if strings.Contains(string(response), key) || !strings.Contains(string(response), "****") {
		t.Error("MCP tool error exposed a credential")
	}
}

func TestRedactHighlightsAndCredentialOnlyMatches(t *testing.T) {
	db, _, _ := redactFixture(t)
	check := func(text string, highlights []highlightRange) {
		t.Helper()
		if !strings.Contains(text, "中文🙂 **** needle") || len(highlights) != 1 {
			t.Fatal("missing redacted CJK text or highlight")
		}
		u := utf16.Encode([]rune(text))
		h := highlights[0]
		if h.Location < 0 || h.Length < 0 || h.Location+h.Length > len(u) || string(utf16.Decode(u[h.Location:h.Location+h.Length])) != "needle" {
			t.Fatal("UTF-16 highlight does not point at needle")
		}
	}
	for _, mode := range []string{"hits", "sessions", "show"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"--json", "needle"}
			if mode == "sessions" {
				args = []string{"--sessions", "--json", "needle"}
			}
			if mode == "show" {
				args = []string{"show", "redact-fixture:0", "--json", "--full", "--query", "needle"}
			}
			got, err := runArgs(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "hits":
				var p hitPage
				if err := json.Unmarshal([]byte(got), &p); err != nil {
					t.Fatal(err)
				}
				if len(p.Hits) == 0 {
					t.Fatal("no hits")
				}
				for _, h := range p.Hits {
					check(h.Snippet, h.Highlights)
				}
			case "sessions":
				var p sessionPage
				if err := json.Unmarshal([]byte(got), &p); err != nil {
					t.Fatal(err)
				}
				if len(p.Sessions) == 0 {
					t.Fatal("no sessions")
				}
				for _, s := range p.Sessions {
					check(s.Best, s.Highlights)
				}
			case "show":
				var p showPage
				if err := json.Unmarshal([]byte(got), &p); err != nil {
					t.Fatal(err)
				}
				if len(p.Messages) == 0 {
					t.Fatal("no messages")
				}
				for _, m := range p.Messages {
					check(m.Text, m.Highlights)
				}
			}
		})
	}
	p, err := compactSearch(context.Background(), db, pageKey{Query: "uniquematch", Limit: 10}, 0)
	if err != nil || p.Total != 3 || len(p.Hits) != 3 {
		t.Fatalf("indexed credential-only matches lost: total=%d err=%v", p.Total, err)
	}
	for _, h := range p.Hits {
		if !strings.Contains(h.Snippet, "****") || len(h.Highlights) != 0 {
			t.Error("credential-only match should be returned without a highlight")
		}
	}
}

func TestRedactIndexTUIAndStatsUntouched(t *testing.T) {
	db, key, text := redactFixture(t)
	if _, err := runArgs(t, "--json", "marker"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT text FROM messages WHERE idx=0`).Scan(&stored); err != nil || stored != text {
		t.Fatal("index text changed")
	}
	rows, err := search(context.Background(), db, "marker", "all", nil, 10, false, false, false, timeRange{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("TUI search: %v", err)
	}
	for _, h := range rows {
		if !strings.Contains(h.Text, key) {
			t.Error("TUI lost original credential")
		}
	}
	if _, err := db.Exec(`UPDATE sessions SET project=?`, key); err != nil {
		t.Fatal(err)
	}
	stats, err := runArgs(t, "stats", "--json")
	if err != nil || !strings.Contains(stats, key) {
		t.Fatal("stats was redacted")
	}
}

func TestRedactDefaultCursorUnchanged(t *testing.T) {
	db, _, _ := redactFixture(t)
	k := pageKey{Mode: "hits", Query: "marker", Harness: "all", Limit: 1, Context: 3, JSON: true}
	// Frozen pre-change fingerprint: default output must keep existing cursors valid.
	const want = "ce6ac124d3a583b21189f251f2a24c79"
	if k.fingerprint() != want {
		t.Fatalf("default fingerprint changed: %s", k.fingerprint())
	}
	p, err := compactSearch(context.Background(), db, k, 0)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(p.NextCursor)
	if err != nil || string(decoded) != "1:"+want+":1" {
		t.Fatal("default cursor changed")
	}
}

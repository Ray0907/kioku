package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHarnessRegistryDrivesInterfaces(t *testing.T) {
	doctorHome(t)
	original := harnesses
	harnesses = append([]harnessSpec(nil), harnesses...)
	harnesses[len(harnesses)-1] = harnessSpec{
		name: "fixture", label: "fixture", env: "KIOKU_FIXTURE_DIR", dir: ".fixture",
		mark: "f", resume: []string{"fixture-agent", "--resume", "{id}"},
	}
	defer func() { harnesses = original }()

	help, _ := warningRun(t, "--help")
	if !strings.Contains(help, "|fixture,") || !strings.Contains(help, "KIOKU_FIXTURE_DIR") || strings.Contains(help, "|agy,") {
		t.Error("CLI help does not follow the harness table")
	}
	oldArgs, oldOutput := os.Args, output
	defer func() { os.Args, output = oldArgs, oldOutput }()
	output = &bytes.Buffer{}
	for _, args := range [][]string{{"--harness", "agy", "--json"}, {"stats", "--harness", "agy"}} {
		os.Args = append([]string{"kioku"}, args...)
		if err := run(); err == nil || err.Error() != `invalid harness "agy"` {
			t.Errorf("%v: got %v, want invalid harness", args, err)
		}
	}

	var wire bytes.Buffer
	if err := serveMCP(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n"), &wire); err != nil {
		t.Fatal(err)
	}
	if strings.Count(wire.String(), `"all","claude","codex","pi","grok","opencode","cursor","fixture"`) != 2 {
		t.Error("MCP enums do not follow the harness table")
	}
	if _, err := mcpCall(json.RawMessage(`{"name":"kioku_search","arguments":{"query":"","harness":"agy"}}`)); err == nil || err.Error() != `invalid harness "agy"` {
		t.Errorf("MCP accepted a removed harness: %v", err)
	}

	m := newModel(nil, "", "all", nil, nil, false, 50, false)
	m.help, m.focus, m.width, m.height = true, false, 100, 35
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "f fixture") || strings.Contains(view, "a agy") {
		t.Error("TUI help does not follow the harness table")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := next.(model).harness; got != "fixture" {
		t.Errorf("shift+tab = %q, want fixture", got)
	}
	if got := agentMark("fixture"); got != "f" {
		t.Errorf("agent mark = %q, want f", got)
	}
	if got := resumeCmd("fixture", "session", "/session.jsonl", "/work"); got != "fixture-agent --resume session" {
		t.Errorf("resume = %q", got)
	}
}

func TestHarnessResumeCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		cmd  string
	}{
		{"claude", []string{"claude", "--resume", "session 'id"}, `claude --resume "session 'id"`},
		{"codex", []string{"codex", "resume", "session 'id"}, `codex resume "session 'id"`},
		{"pi", []string{"pi", "--session", "/session path.jsonl"}, `pi --session "/session path.jsonl"`},
		{"grok", []string{"cd", "/work dir"}, `cd "/work dir"`},
		{"opencode", []string{"opencode", "--session", "session 'id"}, `opencode --session "session 'id"`},
		{"cursor", []string{"cursor-agent", "--resume", "session 'id"}, `cursor-agent --resume "session 'id"`},
		{"agy", []string{"agy", "--conversation", "session 'id"}, `agy --conversation "session 'id"`},
		{"unknown", []string{"pi", "--session", "/session path.jsonl"}, `pi --session "/session path.jsonl"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resumeArgv(tc.name, "session 'id", "/session path.jsonl", "/work dir"); !reflect.DeepEqual(got, tc.argv) {
				t.Errorf("argv = %q, want %q", got, tc.argv)
			}
			if got := resumeCmd(tc.name, "session 'id", "/session path.jsonl", "/work dir"); got != tc.cmd {
				t.Errorf("command = %q, want %q", got, tc.cmd)
			}
			if got := resumeArgv(tc.name, "session 'id", "/session path.jsonl", "/work dir"); !reflect.DeepEqual(got, tc.argv) {
				t.Error("shell quoting mutated the resume arguments")
			}
		})
	}
}

package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"
)

func lookModel(focus bool) model {
	m := model{width: 80, height: 24, cursor: 0, focus: focus, pal: dark, q: "fts5", hitIndex: map[int]bool{1: true}}
	m.rows = []hit{
		{UID: "a", Harness: "claude", Project: "kioku", Snippet: "first", TS: "2026-11-04T14:02:00Z"},
		{UID: "b", Harness: "codex", Project: "other", Snippet: "second", TS: "2026-11-03T14:02:00Z"},
	}
	m.messages = []message{
		{Index: 1, Role: "user", Text: "fts5 question", TS: "2026-11-04T14:02:00Z"},
		{Index: 2, Role: "assistant", Text: "an answer", TS: "2026-11-04T14:03:00Z"},
	}
	return m
}

// listLine returns the plain list row of the first result.
func listLine(v string) string {
	for _, l := range strings.Split(ansi.Strip(v), "\n") {
		if strings.Contains(l, "first") {
			return l
		}
	}
	return ""
}

func TestSelectedRowMarkerFollowsFocus(t *testing.T) {
	if l := listLine(lookModel(false).View()); !strings.Contains(l, "▎") {
		t.Fatalf("list focus marks the selected row with ▎: %q", l)
	}
	l := listLine(lookModel(true).View())
	if strings.Contains(l, "▎") || !strings.Contains(l, "›") {
		t.Fatalf("query focus demotes the selection to ›: %q", l)
	}
}

func TestTranscriptSpeakerRules(t *testing.T) {
	v := ansi.Strip(lookModel(false).View())
	var you, asst string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "fts5 question") {
			you = l
		}
		if strings.Contains(l, "an answer") {
			asst = l
		}
	}
	if !strings.Contains(you, "┃") || !strings.Contains(asst, "│") {
		t.Fatalf("you uses ┃, assistant uses │:\n%q\n%q", you, asst)
	}
}

func TestMessagesSeparatedByBlankLine(t *testing.T) {
	lines := strings.Split(ansi.Strip(lookModel(false).View()), "\n")
	for i, l := range lines {
		if strings.Contains(l, "fts5 question") {
			if strings.TrimSpace(lines[i+1]) != "" {
				t.Fatalf("a blank line follows each message: %q", lines[i+1])
			}
			return
		}
	}
	t.Fatal("message not rendered")
}

func TestFooterKeysPadded(t *testing.T) {
	v := ansi.Strip(lookModel(false).View())
	if !strings.Contains(v, " enter  resume") {
		t.Fatalf("key caps carry a space on each side:\n%s", v)
	}
}

func TestTidySnippet(t *testing.T) {
	for in, want := range map[string]string{
		"- 網址:`http://x`  - 帳號:admin":    "網址:`http://x` - 帳號:admin",
		"## Issue #118   **需求：** 1. 把舊頁": "Issue #118 需求： 1. 把舊頁",
		"| 項目 | 數值 | |---|---|":          "項目 · 數值",
		"plain text stays":               "plain text stays",
		"> quoted":                       "quoted",
	} {
		if got := tidySnippet(in); got != want {
			t.Errorf("tidySnippet(%q) = %q, want %q", in, got, want)
		}
	}
}

func toolModel() model {
	m := lookModel(false)
	m.q = ""
	m.hitIndex = map[int]bool{}
	m.messages = []message{
		{Index: 1, Role: "user", Text: "run it", TS: "2026-11-04T14:02:00Z"},
		{Index: 2, Role: "tool", Text: "Bash · ls\nline two\nline three", TS: "2026-11-04T14:03:00Z"},
		{Index: 3, Role: "assistant", Text: "done", TS: "2026-11-04T14:04:00Z"},
	}
	return m
}

func TestToolMessagesCollapseUntilToggled(t *testing.T) {
	v := ansi.Strip(toolModel().View())
	if !strings.Contains(v, "Bash · ls") || strings.Contains(v, "line two") || !strings.Contains(v, "+2 lines") {
		t.Fatalf("tool message folds to one line with a count:\n%s", v)
	}
	next, _ := toolModel().Update(keyRunes("t"))
	if v := ansi.Strip(next.(model).View()); !strings.Contains(v, "line three") {
		t.Fatalf("t opens tool output:\n%s", v)
	}
}

func TestToolHitStaysOpen(t *testing.T) {
	m := toolModel()
	m.q = "line"
	m.hitIndex = map[int]bool{2: true}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "line two") {
		t.Fatalf("a tool message that matches the query is never folded:\n%s", v)
	}
}

func TestSpaceKeyTypesIntoQueryAndPrompts(t *testing.T) {
	type_ := func(m model, keys ...tea.KeyMsg) model {
		for _, k := range keys {
			next, _ := m.Update(k)
			m = next.(model)
		}
		return m
	}
	space := tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	m := lookModel(false)
	m.focus, m.q = true, ""
	m = type_(m, keyRunes("abc"), space, keyRunes("def"))
	if m.q != "abc def" {
		t.Fatalf("space belongs in the query: %q", m.q)
	}
	p := lookModel(false)
	p.prompt = promptHighlight
	p = type_(p, keyRunes("a"), space, keyRunes("b"))
	if p.status != "a b" {
		t.Fatalf("space belongs in the prompt: %q", p.status)
	}
}

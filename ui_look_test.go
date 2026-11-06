package main

import (
	"strings"
	"testing"

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

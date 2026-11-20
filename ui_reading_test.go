package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTopicInBrowseListAndTitleBar(t *testing.T) {
	m := lookModel(false)
	m.q = ""
	m.hitIndex = map[int]bool{}
	m.rows = []hit{
		{UID: "s1", Harness: "claude", Project: "kioku", Snippet: "Done!", TS: "2026-11-18T14:02:00Z"},
	}
	m.topics = map[string]string{
		"s1": "單字「茶」與 CJK 排除查詢支援",
	}
	m.messages = []message{
		{Index: 0, Role: "user", Text: "單字「茶」與 CJK 排除查詢支援", TS: "2026-11-18T14:00:00Z"},
		{Index: 1, Role: "assistant", Text: "Done!", TS: "2026-11-18T14:02:00Z"},
	}
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	// List row shows the topic instead of the raw last message "Done!".
	listRow := lines[m.headerRows()]
	if !strings.Contains(listRow, "單字「茶」") || strings.Contains(listRow, "Done!") {
		t.Fatalf("browse list row should show session topic, got: %q", listRow)
	}
	// Title bar also displays the topic.
	titleBar := lines[m.layout().titleBar.y]
	if !strings.Contains(titleBar, "單字「茶」") {
		t.Fatalf("transcript title bar should show session topic, got: %q", titleBar)
	}
}

func TestOutlineModeHidesIntermediateStepsAndKeepsHits(t *testing.T) {
	m := lookModel(false)
	m.width, m.height = 100, 30
	m.q = ""
	m.hitIndex = map[int]bool{}
	m.outline = true
	m.cursor = 0
	m.messages = []message{
		{Index: 0, Role: "user", Text: "fix FTS5 speed", TS: "2026-11-18T10:00:00Z"},
		{Index: 1, Role: "assistant", Text: "intermediate narration step", TS: "2026-11-18T10:01:00Z"},
		{Index: 2, Role: "tool", Text: "go test -bench=.", TS: "2026-11-18T10:02:00Z"},
		{Index: 3, Role: "assistant", Text: "final speed conclusion", TS: "2026-11-18T10:03:00Z"},
	}

	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "fix FTS5 speed") || !strings.Contains(v, "final speed conclusion") {
		t.Fatalf("outline must keep user prompt and final reply:\n%s", v)
	}
	if strings.Contains(v, "intermediate narration step") || strings.Contains(v, "go test -bench=.") {
		t.Fatalf("outline must fold intermediate steps and tools:\n%s", v)
	}
	if !strings.Contains(v, "2 steps") || !strings.Contains(v, "outline") {
		t.Fatalf("outline must indicate folded steps and outline badge:\n%s", v)
	}

	// Pressing O toggles off outline and reveals all steps.
	next, _ := m.Update(keyRunes("O"))
	off := next.(model)
	if off.outline {
		t.Fatal("O did not toggle outline off")
	}
	fullV := ansi.Strip(off.View().Content)
	if !strings.Contains(fullV, "intermediate narration step") {
		t.Fatalf("toggling outline off must reveal intermediate steps:\n%s", fullV)
	}

	// Even in outline mode, an intermediate message that matches the search query stays visible.
	m.q = "narration"
	m.hitIndex = map[int]bool{1: true}
	hitV := ansi.Strip(m.View().Content)
	if !strings.Contains(hitV, "intermediate narration step") {
		t.Fatalf("outline must never hide a search hit:\n%s", hitV)
	}
}

func TestReadingMeasureCapsWideTerminal(t *testing.T) {
	m := lookModel(false)
	m.width, m.height = 180, 30
	m.full = true
	m.messages = []message{
		{Index: 0, Role: "user", Text: strings.Repeat("word ", 40), TS: "2026-11-18T10:00:00Z"},
	}
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	bodyStart := m.layout().transcript.y
	wrappedLines := 0
	for _, line := range lines[bodyStart:] {
		if !strings.Contains(line, "word word") {
			continue
		}
		wrappedLines++
		// Trim the right-edge 1-cell rail before measuring the text column.
		content := strings.TrimRight(strings.TrimSuffix(line, "│"), " ")
		const textCol = 19
		if w := displayWidth(content) - textCol; w > maxReadingWidth {
			t.Fatalf("transcript text width = %d > maxReadingWidth (%d): %q", w, maxReadingWidth, content)
		}
	}
	if wrappedLines < 2 {
		t.Fatalf("expected long paragraph to wrap on 180-col terminal, got %d line(s)", wrappedLines)
	}
}

func TestFullScreenReflowAnimation(t *testing.T) {
	m := lookModel(false)
	m.width, m.height = 100, 30
	startH := m.listHeight()
	if startH <= 2 {
		t.Fatalf("unexpected starting list height %d", startH)
	}

	next, cmd := m.Update(keyRunes("v"))
	m = next.(model)
	if !m.full {
		t.Fatal("v must set full=true")
	}
	if m.listHeight() >= startH {
		t.Fatalf("v must begin collapsing immediately: start=%d now=%d", startH, m.listHeight())
	}

	frames := 0
	for m.animActive && frames < 40 {
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != m.height {
			t.Fatalf("animation frame %d height = %d, want %d", frames, len(lines), m.height)
		}
		if cmd == nil {
			t.Fatal("active animation must return a tick command")
		}
		next, cmd = m.Update(animTickMsg{id: m.animTickID})
		m = next.(model)
		frames++
	}
	if m.animActive || m.listHeight() != 0 {
		t.Fatalf("animation did not settle to 0: active=%v listH=%d", m.animActive, m.listHeight())
	}
}

func TestHitJumpPulse(t *testing.T) {
	m := scrollModel()
	next, cmd := m.Update(keyRunes("n"))
	m = next.(model)
	if m.pulseTicks != pulseFrameCount || cmd == nil {
		t.Fatalf("n must trigger hit pulse: ticks=%d cmd=%v", m.pulseTicks, cmd != nil)
	}
	for i := 0; i < pulseFrameCount+2 && m.pulseTicks > 0; i++ {
		next, _ = m.Update(animTickMsg{id: m.animTickID})
		m = next.(model)
	}
	if m.pulseTicks != 0 {
		t.Fatalf("pulse did not decay to 0: %d", m.pulseTicks)
	}
}

func TestSplitterDragAndDoubleClickReset(t *testing.T) {
	m := lookModel(false)
	m.mouse = true
	m.width, m.height = 100, 30
	origH := m.listHeight()
	barY := m.layout().titleBar.y

	// Click title bar and drag down by 5 rows.
	next, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: barY})
	m = next.(model)
	if m.drag != dragSplit {
		t.Fatalf("clicking title bar must start dragSplit, got %v", m.drag)
	}
	next, _ = m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 10, Y: barY + 5})
	m = next.(model)
	if m.listHeight() <= origH {
		t.Fatalf("dragging title bar down should grow list: orig=%d now=%d", origH, m.listHeight())
	}
	next, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 10, Y: barY + 5})
	m = next.(model)
	if m.drag != dragNone {
		t.Fatal("mouse release must clear drag state")
	}

	// Double-click title bar resets to default ratio.
	newBarY := m.layout().titleBar.y
	next, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: newBarY})
	m = next.(model)
	if m.listRatio != 0 || m.listHeight() != origH {
		t.Fatalf("double-click title bar should reset ratio: ratio=%v listH=%d want=%d", m.listRatio, m.listHeight(), origH)
	}
}

func TestHitMapRailClickJumpsAndDragScrubs(t *testing.T) {
	m := scrollModel()
	m.width, m.height = 100, 26
	m.full = true
	m.messages = nil
	m.hitIndex = map[int]bool{0: true, 49: true}
	for i := 0; i < 50; i++ {
		m.messages = append(m.messages, message{
			Index: i, Role: "assistant",
			Text: fmt.Sprintf("B6LONG message %02d", i),
			TS:   "2026-11-18T10:00:00Z",
		})
	}
	m.cursor = 0
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "▪") {
		t.Fatalf("hit-map rail should mark non-cursor hit with ▪:\n%s", view)
	}

	// Clicking the bottom hit tick jumps straight to message 49 with pulse.
	rail := m.layout().rail
	next, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: rail.x + 1, Y: rail.y + rail.h - 1})
	m = next.(model)
	if m.cursor != 49 || m.pulseTicks == 0 || cmd == nil {
		t.Fatalf("clicking hit tick should jump to msg 49 with pulse: cursor=%d pulse=%d", m.cursor, m.pulseTicks)
	}

	// Clicking and dragging on a non-hit rail row scrubs the transcript.
	m.cursor = 0
	m.hitIndex = map[int]bool{}
	m.q = ""
	next, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: rail.x + 1, Y: rail.y + rail.h - 1})
	m = next.(model)
	if m.drag != dragRail {
		t.Fatalf("clicking empty rail must start dragRail, got %v", m.drag)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "B6LONG message 49") {
		t.Fatalf("scrubbing to bottom of rail should reveal last message:\n%s", ansi.Strip(m.View().Content))
	}
}

// In the outline an unselected message is a glance: it stops after contextLines lines, while the
// selected message is shown whole.
func TestOutlineCapsContextMessages(t *testing.T) {
	long := strings.Repeat("line\n", 9) + "last"
	m := lookModel(false)
	m.width, m.height = 100, 40
	m.q = ""
	m.hitIndex = map[int]bool{}
	m.outline = true
	m.cursor = 0
	m.messages = []message{
		{Index: 0, Role: "user", Text: "short prompt", TS: "2026-11-18T10:00:00Z"},
		{Index: 1, Role: "assistant", Text: long, TS: "2026-11-18T10:01:00Z"},
	}
	v := ansi.Strip(m.View().Content)
	if strings.Contains(v, "last") || !strings.Contains(v, "…") {
		t.Fatalf("an unselected message must be cut after %d lines:\n%s", contextLines, v)
	}
	m.cursor = 1
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "last") {
		t.Fatalf("the selected message must show whole:\n%s", v)
	}
}

// The title bar reports reading progress by characters, not by message count.
func TestTranscriptTitleShowsReadPercent(t *testing.T) {
	m := lookModel(false)
	m.width, m.height = 100, 30
	m.q = ""
	m.hitIndex = map[int]bool{}
	m.messages = []message{
		{Index: 0, Role: "user", Text: strings.Repeat("a", 100), TS: "2026-11-18T10:00:00Z"},
		{Index: 1, Role: "assistant", Text: strings.Repeat("b", 300), TS: "2026-11-18T10:01:00Z"},
	}
	for cursor, want := range map[int]string{0: "25%", 1: "100%"} {
		m.cursor = cursor
		v := ansi.Strip(m.View().Content)
		if !strings.Contains(v, want) || strings.Contains(v, "result ") {
			t.Fatalf("cursor %d: want %s and no result count:\n%s", cursor, want, v)
		}
	}
}

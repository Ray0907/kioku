package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPolishSearchFocusAndHelp(t *testing.T) {
	m := newModel(nil, "", "all", nil, nil, false, 50, false)
	m.width = 114
	v := m.View()
	line := strings.Split(ansi.Strip(v.Content), "\n")[0]
	if !strings.HasPrefix(line, "▎ kioku ▸ ") || !strings.Contains(line, `Search messages… (space AND · "phrase" · -exclude)`) || !strings.HasSuffix(line, "0 recent sessions  ") {
		t.Fatalf("focused query row: %q", line)
	}
	if v.Cursor == nil || v.Cursor.Position.X != displayWidth("▎ kioku ▸ ") {
		t.Fatalf("placeholder must leave the native cursor at the input start: %+v", v.Cursor)
	}
	m = press(m, "?", "中", "left", "?")
	if m.help || !m.focus || m.q != "??中" || m.caret() != 2 {
		t.Fatalf("? should type at the caret, not open help: help=%v query=%q caret=%d", m.help, m.q, m.caret())
	}
	m = press(m, "esc", "?")
	if !m.help || m.focus || m.q != "??中" || m.View().Cursor != nil {
		t.Fatal("? should open help without editing the query when results have focus")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "don't edit behind help"})
	m = next.(model)
	if m.q != "??中" {
		t.Fatal("help should shield the query from paste")
	}
	m = press(m, "esc")
	if m.help || m.focus {
		t.Fatal("closing help should keep focus on results")
	}
	line = strings.Split(ansi.Strip(m.View().Content), "\n")[0]
	if strings.Contains(line, "▎") || strings.Contains(line, "Search messages") {
		t.Fatalf("unfocused query row: %q", line)
	}
}

func TestPolishFilterChips(t *testing.T) {
	m := lookModel(false)
	m.width, m.mouse = 114, true
	m.harness, m.root = "agy", "/work/中文專案"
	m.projects = []string{"named"}
	m.rng = timeRange{Since: "2026-10-01"}
	m.pens = []pen{{"複核", 1}}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if strings.Contains(lines[0], "中文專案") || strings.Contains(lines[0], "2026-10-01") || !strings.Contains(lines[0], "2 messages · 2 sessions") {
		t.Fatalf("count should not carry filter labels: %q", lines[0])
	}
	for _, tc := range []struct {
		chip    string
		cleared func(model) bool
		query   bool
	}{
		{"✦ agy ×", func(got model) bool { return got.harness == "all" }, true},
		{"⌂ 中文專案 ×", func(got model) bool { return got.root == "" }, true},
		{"⌂ named ×", func(got model) bool { return len(got.projects) == 0 }, true},
		{"◷ from 2026-10-01 ×", func(got model) bool { return !got.rng.active() }, true},
		{"✎ 複核 ×", func(got model) bool { return len(got.pens) == 0 }, false},
	} {
		at := strings.Index(lines[1], tc.chip)
		if at < 0 {
			t.Errorf("missing chip %q: %q", tc.chip, lines[1])
			continue
		}
		x := displayWidth(lines[1][:at]+tc.chip) - 1 // click the ×, after any double-width cells
		next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
		got := next.(model)
		if !tc.cleared(got) || (cmd != nil) != tc.query || (got.revision > m.revision) != tc.query {
			t.Errorf("click %q did not clear the expected filter (requery = %v)", tc.chip, cmd != nil)
		}
	}
}

func TestPolishFilterChipClipping(t *testing.T) {
	m := lookModel(false)
	m.width, m.mouse = 40, true
	m.root = "/work/" + strings.Repeat("中文", 30)
	line := strings.Split(ansi.Strip(m.View().Content), "\n")[1]
	if !strings.Contains(line, "… ×") || displayWidth(line) != m.width {
		t.Fatalf("truncated chip should keep its clear affordance: %q", line)
	}
	at := strings.Index(line, "×")
	next, _ := m.Update(tea.MouseClickMsg{X: displayWidth(line[:at]), Y: 1, Button: tea.MouseLeft})
	if got := next.(model); got.root != "" || got.headerRows() != 2 {
		t.Fatal("clearing the last chip should remove its row")
	}
}

func TestPolishHiddenProjectFiltersRemainClearable(t *testing.T) {
	m := newModel(nil, "", "agy", []string{"one", "two", "three"}, nil, false, 50, false)
	m.focus, m.width = false, 25
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.Contains(lines[1], "✦ agy ×  +3") || displayWidth(lines[1]) != m.width {
		t.Fatalf("hidden filters should be counted: %q", lines[1])
	}
	m.width = 80
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "No sessions match the current filters.") || !strings.Contains(view, "Press p to remove the project scope.") {
		t.Fatalf("empty browse should explain how to recover:\n%s", view)
	}
	m.width = 25
	next, cmd := m.Update(keyRunes("p"))
	if got := next.(model); len(got.projects) != 0 || got.harness != "agy" || got.root != "" || cmd == nil {
		t.Fatal("p should clear named project filters and rerun, even without a mouse")
	}
}

func TestPolishHelpPointerIsModal(t *testing.T) {
	m := scrollModel()
	m.help, m.status = true, "status before opening help"
	for _, h := range []int{24, 25, 30, 31} {
		m.height = h
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		r := m.helpRect()
		if len(lines) != h || !strings.Contains(lines[r.y], "╭") {
			t.Errorf("height %d: help overlay and hitbox must match (rows=%d rect=%+v)", h, len(lines), r)
		}
	}
	m = scrollUpdate(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown, Y: m.layout().transcript.y})
	if m.transcriptOffset != 0 {
		t.Fatal("wheel input must not scroll the transcript behind help")
	}
}

func TestPolishResultGrouping(t *testing.T) {
	m := lookModel(false)
	m.width = 114
	m.rows = []hit{
		{UID: "a", Harness: "agy", Project: "long-projectname", Snippet: "first"},
		{UID: "a", Harness: "agy", Project: "long-projectname", Snippet: "second", Role: "tool"},
		{UID: "b", Harness: "hermes", Project: "long-projectname", Snippet: "third"},
	}
	for _, offset := range []int{0, 1} {
		m.offset, m.sel = offset, 1
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")[m.headerRows():]
		if offset == 0 && (!strings.Contains(lines[0], "✦ first") || !strings.Contains(lines[0], "long-projectname") || !strings.Contains(lines[1], "└ tool · second") || !strings.HasPrefix(strings.TrimSpace(strings.SplitN(lines[1], "second", 2)[1]), "·")) {
			t.Errorf("grouped tool result: %q\n%q", lines[0], lines[1])
		}
		if offset == 1 && !strings.Contains(lines[0], "✦ tool · second") {
			t.Errorf("first visible group row needs its agent and project: %q", lines[0])
		}
		if l := lines[2-offset]; !strings.Contains(l, "❖ third") || !strings.Contains(l, "long-projectname") {
			t.Errorf("a different session with the same project starts a new group: %q", l)
		}
	}
}

func TestPolishFooterAdaptsToHits(t *testing.T) {
	m := lookModel(false)
	m.width = 114
	m.rows[0].ResumeCmd = "claude --resume " + strings.Repeat("a", 40)
	for _, total := range []int{1, 2} {
		m.hitIndex[2] = total > 1
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		footer := lines[len(lines)-1]
		if strings.Contains(footer, "n/N") != (total > 1) || !strings.Contains(footer, "▸ claude --resume aaaaaaa…") || !strings.HasSuffix(footer, "  ") {
			t.Errorf("footer with %d hits: %q", total, footer)
		}
	}
}

func TestPolishTranscriptClickTargets(t *testing.T) {
	m := lookModel(false)
	m.width, m.height, m.mouse, m.outline = 114, 30, true, true
	m.messages[1].Text = "first assistant line\nsecond assistant line"
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	click := func(x, y int) model {
		next, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return next.(model)
	}
	bar := m.layout().titleBar
	at := strings.LastIndex(lines[bar.y], "outline")
	if at < 0 {
		t.Fatal("missing outline tag")
	}
	if got := click(displayWidth(lines[bar.y][:at]), bar.y); got.outline || got.drag != dragNone {
		t.Fatal("outline tag should toggle, not grab the divider")
	}
	if got := click(0, bar.y); got.drag != dragSplit || !got.outline {
		t.Fatal("rest of the divider should still be draggable")
	}
	for y, line := range lines {
		at = strings.Index(line, "second assistant line")
		if at >= 0 {
			if got := click(displayWidth(line[:at]), y); got.cursor != 1 {
				t.Fatalf("clicking a continuation should select its message, got %d", got.cursor)
			}
			return
		}
	}
	t.Fatal("fixture missing the continuation row")
}

func TestPolishHighlightBackgroundBeatsSelection(t *testing.T) {
	m := lookModel(false)
	m.pens = []pen{{"review", 0}}
	row := m.selectionRow(m.paint("fts5 review", true, true), 20)
	for _, tc := range []struct{ bg, word string }{{"48;2;74;42;42", "fts5"}, {"48;2;60;50;7", "review"}} {
		at := strings.Index(row, tc.word)
		if at < 0 || strings.LastIndex(row[:at], tc.bg) <= strings.LastIndex(row[:at], "48;2;41;54;71") {
			t.Errorf("highlight background lost under selection: %q", row)
		}
	}
}

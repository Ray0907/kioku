package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "alt+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	case "alt+right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	r := []rune(name)
	return tea.KeyPressMsg{Code: r[0], Text: name}
}

func press(m model, names ...string) model {
	for _, n := range names {
		next, _ := m.Update(keyMsg(n))
		m = next.(model)
	}
	return m
}

func queryModel(q string) model {
	m := newModel(nil, q, "all", nil, nil, false, 50, false)
	m.focus = true
	return m
}

func TestKeyNamesMatchBubbleTea(t *testing.T) {
	for _, n := range []string{"left", "right", "alt+left", "alt+right", "home", "end", "delete", "ctrl+a", "ctrl+e", "ctrl+b", "ctrl+f"} {
		if got := keyMsg(n).String(); got != n {
			t.Errorf("keyMsg(%q).String() = %q", n, got)
		}
	}
}

func TestQueryCaretEdits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		q     string
		keys  []string
		want  string
		wantP int
	}{
		{"insert mid", "helo", []string{"left", "l"}, "hello", 4},
		{"backspace mid", "hexllo", []string{"left", "left", "left", "backspace"}, "hello", 2},
		{"delete forward", "hexllo", []string{"home", "right", "right", "delete"}, "hello", 2},
		{"delete at end is noop", "abc", []string{"delete"}, "abc", 3},
		{"backspace at start is noop", "abc", []string{"home", "backspace"}, "abc", 0},
		{"home end", "abc", []string{"home", "x", "end", "y"}, "xabcy", 5},
		{"ctrl a e", "abc", []string{"ctrl+a", "x", "ctrl+e", "y"}, "xabcy", 5},
		{"ctrl b f", "abc", []string{"ctrl+b", "ctrl+b", "x", "ctrl+f", "y"}, "axbyc", 4},
		{"left clamps", "ab", []string{"left", "left", "left", "x"}, "xab", 1},
		{"right clamps", "ab", []string{"right", "right", "x"}, "abx", 3},
		{"ctrl+u to caret", "hello world", []string{"left", "left", "left", "left", "left", "ctrl+u"}, "world", 0},
		{"ctrl+w before caret", "foo bar baz", []string{"left", "left", "left", "left", "ctrl+w"}, "foo  baz", 4},
		{"cjk insert", "中文測試", []string{"left", "left", "字"}, "中文字測試", 3},
		{"cjk backspace", "中文字測試", []string{"left", "left", "backspace"}, "中文測試", 2},
		{"cjk ctrl+w mid", "中文測試 abc", []string{"left", "left", "left", "left", "ctrl+w"}, " abc", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := press(queryModel(tc.q), tc.keys...)
			if m.q != tc.want || m.caret() != tc.wantP {
				t.Fatalf("got %q caret %d, want %q caret %d", m.q, m.caret(), tc.want, tc.wantP)
			}
		})
	}
}

func TestQueryEditRefreshesButMoveDoesNot(t *testing.T) {
	m := queryModel("abc")
	next, cmd := m.Update(keyMsg("left"))
	if cmd != nil || next.(model).revision != m.revision {
		t.Error("caret move must not refresh results")
	}
	next, cmd = m.Update(keyMsg("delete"))
	if cmd == nil || next.(model).revision != m.revision+1 {
		t.Error("delete must refresh results")
	}
}

func TestQueryWordJumps(t *testing.T) {
	q := "foo bar 中文測試 baz"
	m := queryModel(q)
	var got []int
	for range 4 {
		m = press(m, "alt+left")
		got = append(got, m.caret())
	}
	if fmt.Sprint(got) != "[13 8 4 0]" {
		t.Fatalf("alt+left from end = %v", got)
	}
	got = nil
	for range 4 {
		m = press(m, "alt+right")
		got = append(got, m.caret())
	}
	if fmt.Sprint(got) != "[3 7 12 16]" {
		t.Fatalf("alt+right from start = %v", got)
	}
	m = press(m, "alt+right")
	if m.caret() != 16 {
		t.Fatal("alt+right past end must clamp")
	}
}

func TestQueryPasteInsertsAtCaret(t *testing.T) {
	m := press(queryModel("ac"), "left")
	next, cmd := m.Update(tea.PasteMsg{Content: "b\n"})
	m = next.(model)
	// paste is flattened to one line: the newline becomes a space
	if m.q != "ab c" || m.caret() != 3 || cmd == nil {
		t.Fatalf("paste got %q caret %d", m.q, m.caret())
	}
}

func TestQueryCaretClampedWhenQueryReplaced(t *testing.T) {
	m := model{width: 80, height: 24, focus: true, pal: dark, q: "ab", qpos: 9}
	if m.caret() != 2 {
		t.Fatalf("caret = %d, want 2", m.caret())
	}
	m.q = ""
	if m.caret() != 0 {
		t.Fatal("caret must clamp to empty query")
	}
	m.View() // must not panic
	m = newModel(nil, "中文", "all", nil, nil, false, 50, false)
	if m.caret() != 2 {
		t.Fatalf("new model caret = %d, want end of query", m.caret())
	}
}

func queryPrefixWidth() int { return displayWidth("  kioku ▸ ") }

func TestViewCursorFollowsCaret(t *testing.T) {
	for _, tc := range []struct {
		q    string
		keys []string
		want int // cells of query text left of the caret
	}{
		{"hello", nil, 5},
		{"hello", []string{"left", "left"}, 3},
		{"中文測試", []string{"left"}, 6},
		{"中文測試", []string{"home"}, 0},
		{"a中b", []string{"left", "left"}, 1},
	} {
		m := press(queryModel(tc.q), tc.keys...)
		m.width, m.height = 80, 24
		cur := m.View().Cursor
		if cur == nil || cur.Position.X != queryPrefixWidth()+tc.want || cur.Position.Y != 0 {
			t.Errorf("%q %v: cursor %+v, want x %d", tc.q, tc.keys, cur, queryPrefixWidth()+tc.want)
		}
	}
}

func TestQueryWindowKeepsCaretVisible(t *testing.T) {
	long := strings.Repeat("abc中文 ", 12)
	r := []rune(long)
	for _, cells := range []int{8, 15, 30} {
		for pos := 0; pos <= len(r); pos++ {
			shown, x := queryWindow(string(r[:pos]), string(r[pos:]), cells)
			if displayWidth(shown) > cells {
				t.Fatalf("cells %d pos %d: shown %q is %d wide", cells, pos, shown, displayWidth(shown))
			}
			if x < 0 || x > cells-1 && pos < len(r) || x > cells {
				t.Fatalf("cells %d pos %d: caret x %d outside window", cells, pos, x)
			}
			// the glyph under the caret is the rune after pos, unless it was clipped to an ellipsis
			var before string
			for _, g := range shown {
				if displayWidth(before) >= x {
					break
				}
				before += string(g)
			}
			if displayWidth(before) != x {
				t.Fatalf("cells %d pos %d: caret x %d splits a wide glyph in %q", cells, pos, x, shown)
			}
			after := strings.TrimPrefix(shown, before)
			if pos < len(r) && after != "" && after != "…" && !strings.HasPrefix(after, string(r[pos])) {
				t.Fatalf("cells %d pos %d: under caret %q, want %q", cells, pos, after, string(r[pos]))
			}
		}
	}
	if s, x := queryWindow("short", "", 20); s != "short" || x != 5 {
		t.Fatalf("fits: %q %d", s, x)
	}
}

func TestViewOverflowCursorStaysInsideRow(t *testing.T) {
	m := queryModel(strings.Repeat("中文abc ", 20))
	m.width, m.height = 50, 24
	steps := append([]string{"home"}, strings.Split(strings.Repeat("right,", 50), ",")[:50]...)
	for _, keys := range [][]string{nil, {"home"}, steps} {
		mm := press(m, keys...)
		cur := mm.View().Cursor
		if cur == nil || cur.Position.X >= m.width {
			t.Fatalf("cursor %+v outside width %d", cur, m.width)
		}
	}
}

func TestQueryHistoryRecall(t *testing.T) {
	m := queryModel("")
	m.history = []string{"alpha", "beta"}
	m = press(m, "up")
	if m.q != "beta" || m.caret() != 4 {
		t.Fatalf("first up = %q", m.q)
	}
	m = press(m, "up")
	if m.q != "alpha" {
		t.Fatalf("second up = %q", m.q)
	}
	m = press(m, "up")
	if m.q != "alpha" {
		t.Fatalf("up at oldest = %q", m.q)
	}
	m = press(m, "down")
	if m.q != "beta" {
		t.Fatalf("down = %q", m.q)
	}
	m = press(m, "down")
	if m.q != "" || !m.focus {
		t.Fatalf("down past newest = %q focus %v, want empty query still focused", m.q, m.focus)
	}
	m = press(m, "down")
	if m.focus {
		t.Fatal("down on empty query not walking history must still leave the input")
	}
}

func TestQueryHistoryNeedsEmptyQuery(t *testing.T) {
	m := queryModel("typed")
	m.history = []string{"alpha"}
	m = press(m, "up")
	if m.q != "typed" {
		t.Fatalf("up with text recalled %q", m.q)
	}
	m = queryModel("")
	m.history = []string{"alpha"}
	m = press(m, "up", "x") // typing ends the walk; down then leaves the input
	if m.q != "alphax" {
		t.Fatalf("edit after recall = %q", m.q)
	}
	m = press(m, "down")
	if m.focus {
		t.Fatal("down after editing a recalled query must leave the input")
	}
	m = queryModel("")
	m = press(m, "up")
	if m.q != "" {
		t.Fatal("up with no history must do nothing")
	}
}

func TestQueryHistoryRemembersWhenLeavingInput(t *testing.T) {
	m := queryModel("one")
	m = press(m, "down") // leaves input
	m.focus = true
	m = press(m, "ctrl+u", "t", "w", "o", "esc")
	if fmt.Sprint(m.history) != "[one two]" {
		t.Fatalf("history = %v", m.history)
	}
}

func TestAddHistoryDedupesAndCaps(t *testing.T) {
	h := addHistory(nil, "  a ")
	h = addHistory(h, "a")
	h = addHistory(h, "")
	h = addHistory(h, "b")
	h = addHistory(h, "a")
	if fmt.Sprint(h) != "[a b a]" {
		t.Fatalf("history = %v", h)
	}
	for i := range 150 {
		h = addHistory(h, fmt.Sprint("q", i))
	}
	if len(h) != historyCap || h[len(h)-1] != "q149" || h[0] != "q50" {
		t.Fatalf("cap: len %d first %q last %q", len(h), h[0], h[len(h)-1])
	}
}

func TestHistoryFileNextToIndex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIOKU_INDEX", filepath.Join(dir, "sub", "index.db"))
	if got := loadHistory(); len(got) != 0 {
		t.Fatalf("missing file loaded %v", got)
	}
	want := []string{"中文 查詢", "hello world"}
	saveHistory(want)
	if _, err := os.Stat(filepath.Join(dir, "sub", "history")); err != nil {
		t.Fatalf("history file not next to index: %v", err)
	}
	if got := loadHistory(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("round trip = %v", got)
	}
}

func TestHelpMentionsQueryEditing(t *testing.T) {
	m := newModel(nil, "", "all", nil, nil, false, 50, false)
	m.width, m.height, m.help = 100, 40, true
	help := ansi.Strip(m.View().Content)
	for _, want := range []string{"alt+left", "ctrl+a", "ctrl+e", "ctrl+b", "delete", "history"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

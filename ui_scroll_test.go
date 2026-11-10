package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func scrollModel() model {
	m := lookModel(false)
	m.mouse = true
	m.q = "B6LONG"
	m.rows = []hit{{UID: "long", Index: 0, Snippet: "long result"}, {UID: "long", Index: 1, Snippet: "next result"}}
	m.loadedKey = m.loadKey(m.rows[0])
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("B6LONG line %03d", i))
	}
	m.messages = []message{{Index: 0, Role: "assistant", Text: strings.Join(lines, "\n")}, {Index: 1, Role: "user", Text: "B6LONG next message"}}
	m.hitIndex = map[int]bool{0: true, 1: true}
	return m
}

func scrollUpdate(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestTranscriptPageScroll(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			m := scrollModel()
			m.full = full
			if strings.Contains(ansi.Strip(m.View()), "B6LONG line 200") {
				t.Fatal("fixture already shows the last line")
			}
			for i := 0; i < 60; i++ {
				m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
			}
			end := ansi.Strip(m.View())
			if !strings.Contains(end, "B6LONG line 200") {
				t.Fatalf("PgDn never reaches the last line:\n%s", end)
			}
			m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
			if got := ansi.Strip(m.View()); got != end {
				t.Fatalf("scroll beyond the end changed the screen:\n%s", got)
			}
			for i := 0; i < 60; i++ {
				m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgUp})
			}
			start := ansi.Strip(m.View())
			if !strings.Contains(start, "B6LONG line 001") {
				t.Fatalf("PgUp never returns to the first line:\n%s", start)
			}
			m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgUp})
			if ansi.Strip(m.View()) != start {
				t.Fatal("scroll above the start changed the screen")
			}
		})
	}
}

func TestTranscriptScrollReset(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyRunes, Runes: []rune("N")},
	} {
		t.Run(key.String(), func(t *testing.T) {
			m := scrollModel()
			for i := 0; i < 15; i++ {
				m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
			}
			m = scrollUpdate(m, key)
			want := scrollModel()
			want = scrollUpdate(want, key)
			if got := ansi.Strip(m.View()); got != ansi.Strip(want.View()) || !strings.Contains(got, "B6LONG next message") {
				t.Fatalf("message change retains scroll position:\n%s", got)
			}
		})
	}
}

func TestTranscriptWheelScrollsLines(t *testing.T) {
	m := scrollModel()
	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelDown, Y: m.headerRows() + m.listHeight() + 3}
	m = scrollUpdate(m, wheel)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "B6LONG line 004") || strings.Contains(v, "B6LONG line 001") || m.cursor != 0 {
		t.Fatalf("wheel should scroll three lines, not messages:\n%s", v)
	}
	for i := 0; i < 80; i++ {
		m = scrollUpdate(m, wheel)
	}
	if !strings.Contains(ansi.Strip(m.View()), "B6LONG line 200") {
		t.Fatal("wheel cannot reach the last line")
	}
	wheel.Button = tea.MouseButtonWheelUp
	for i := 0; i < 80; i++ {
		m = scrollUpdate(m, wheel)
	}
	if !strings.Contains(ansi.Strip(m.View()), "B6LONG line 001") {
		t.Fatal("wheel cannot return to the first line")
	}
}

func TestTranscriptScrollResizeAndHighlight(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := scrollModel()
	for i := 0; i < 60; i++ {
		m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 45, Height: 18}, {Width: 120, Height: 40}, {Width: 55, Height: 12}, {Width: 80, Height: 24}} {
		m = scrollUpdate(m, size)
		if !strings.Contains(ansi.Strip(m.View()), "B6LONG") {
			t.Fatalf("resize left a blank transcript (%+v)", size)
		}
		for i := 0; i < 60; i++ {
			m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
		}
		v := m.View()
		if !strings.Contains(ansi.Strip(v), "B6LONG line 200") {
			t.Fatalf("cannot reach transcript end after resize (%+v):\n%s", size, ansi.Strip(v))
		}
		lines := strings.Split(v, "\n")
		if len(lines) != size.Height {
			t.Fatalf("resize: got %d rows, want %d", len(lines), size.Height)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("resize: line too wide: %q", line)
			}
			if strings.Contains(ansi.Strip(line), "B6LONG line 200") && !strings.Contains(line, lipgloss.ColorProfile().Color(m.pal.hit).Sequence(false)) {
				t.Fatal("scrolled search hit lost highlighting")
			}
		}
	}
}

func TestTranscriptScrollKeepsListWheelAndQueryKeys(t *testing.T) {
	m := scrollModel()
	for i := 0; i < 20; i++ {
		m.rows = append(m.rows, hit{UID: "other"})
	}
	m = scrollUpdate(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Y: m.headerRows()})
	if m.offset != 3 || m.sel != 0 || m.cursor != 0 || !strings.Contains(ansi.Strip(m.View()), "B6LONG line 001") {
		t.Fatal("list wheel behavior changed")
	}
	m.focus = true
	before := ansi.Strip(m.View())
	m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if ansi.Strip(m.View()) != before {
		t.Fatal("PgDn scrolls while editing the query")
	}
	m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.q != "" {
		t.Fatal("ctrl+u no longer clears the query")
	}
}

func TestTranscriptScrollAcrossMessages(t *testing.T) {
	m := scrollModel()
	m.full = true
	m.cursor = 30
	m.messages = nil
	for i := 0; i < 60; i++ {
		m.messages = append(m.messages, message{Index: i, Role: "assistant", Text: fmt.Sprintf("B6LONG message %02d", i)})
	}
	for i := 0; i < 30; i++ {
		m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(m.View()), "B6LONG message 59") {
		t.Fatal("cannot page past the initially rendered messages")
	}
	for i := 0; i < 30; i++ {
		m = scrollUpdate(m, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if !strings.Contains(ansi.Strip(m.View()), "B6LONG message 00") {
		t.Fatal("cannot page before the initially rendered messages")
	}
	m.full = false
	m = scrollUpdate(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: m.headerRows() + 1})
	want := m
	want.transcriptOffset = 0
	if ansi.Strip(m.View()) != ansi.Strip(want.View()) || m.cursor != 1 {
		t.Fatal("clicking a different result retains the scroll position")
	}
}

func TestTranscriptWheelUpFromLastMessage(t *testing.T) {
	m := scrollModel()
	m.full = true
	m.cursor = 59
	m.messages = nil
	for i := 0; i < 60; i++ {
		m.messages = append(m.messages, message{Index: i, Role: "assistant", Text: fmt.Sprintf("B6LONG message %02d", i)})
	}
	if !strings.Contains(ansi.Strip(m.View()), "B6LONG message 59") {
		t.Fatal("fixture does not show the last message")
	}
	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelUp, Y: m.headerRows() + 3}
	for i := 0; i < 80; i++ {
		m = scrollUpdate(m, wheel)
	}
	start := ansi.Strip(m.View())
	if !strings.Contains(start, "B6LONG message 00") || m.cursor != 59 {
		t.Fatalf("wheel-up from the last message cannot reach the start without changing selection:\n%s", start)
	}
	m = scrollUpdate(m, wheel)
	if ansi.Strip(m.View()) != start {
		t.Fatal("wheel-up beyond the start changed the screen")
	}
}

func TestTranscriptScrollHelp(t *testing.T) {
	m := scrollModel()
	m.width = 180
	for _, help := range []bool{false, true} {
		m.help = help
		view := ansi.Strip(m.View())
		for _, key := range []string{"pgup", "pgdown"} {
			if !strings.Contains(view, key) {
				t.Fatalf("help=%t missing %s:\n%s", help, key, view)
			}
		}
	}
}

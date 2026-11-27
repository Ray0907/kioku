package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpOverlayAlignsShortcutColumns(t *testing.T) {
	m := lookModel(false)
	m.width, m.height, m.help = 86, 26, true
	view := ansi.Strip(m.View().Content)
	for _, section := range []string{"Search", "Results & Filters", "Transcript"} {
		if !strings.Contains(view, "│ "+section+" ") {
			t.Errorf("missing help section %q:\n%s", section, view)
		}
	}
	var columns [4]int
	for n, cells := range [][4]string{
		{"space", "AND terms", "↓ / esc", "go to results"},
		{`"quotes"`, "exact phrase", "/ or ctrl+f", "focus search"},
		{"enter", "resume in cwd", "p", "project scope"},
		{"n / N", "next/prev hit", "O", "outline vs all"},
	} {
		found := false
		for _, line := range strings.Split(view, "\n") {
			if !strings.Contains(line, cells[1]) || !strings.Contains(line, cells[3]) {
				continue
			}
			found = true
			end := 0
			for col, text := range cells {
				at := strings.Index(line[end:], text)
				if at < 0 {
					t.Fatalf("missing or out-of-order cell %q in %q", text, line)
				}
				x := displayWidth(line[:end+at])
				if n == 0 {
					columns[col] = x
				} else if x != columns[col] {
					t.Errorf("%q at column %d, want %d", text, x, columns[col])
				}
				end += at + len(text)
			}
			break
		}
		if !found {
			t.Errorf("missing two-column shortcut row %q:\n%s", cells, view)
		}
	}
}

func TestHelpOverlayAgentLegendOrderAndColors(t *testing.T) {
	m := lookModel(false)
	m.width, m.help = 86, true
	m.pal.agents = make(map[string]agentColors)
	legend := [2][]string{
		{"Claude", "AgY", "Codex", "Hermes"},
		{"Pi", "Cursor", "OpenCode", "Grok"},
	}
	color := 101
	for _, agents := range legend {
		for _, name := range agents {
			m.pal.agents[strings.ToLower(name)] = agentColors{fg: fmt.Sprintf("#%02x3575", color)}
			color++
		}
	}
	lines := strings.Split(m.View().Content, "\n")
	for i, line := range lines {
		if !strings.Contains(ansi.Strip(line), "│ Agents ") {
			continue
		}
		if i+len(legend) > len(lines) {
			t.Fatal("agent legend cut off at bottom of screen")
		}
		color = 101
		for offset, agents := range legend {
			styled := lines[i+offset]
			plain := ansi.Strip(styled)
			for _, name := range agents {
				mark := agentMark(strings.ToLower(name))
				_, rest, ok := strings.Cut(plain, mark+" "+name)
				if !ok {
					t.Fatalf("missing or out-of-order legend entry %q: %q", name, plain)
				}
				plain = rest
				glyph := fmt.Sprintf(`\x1b\[[0-9;]*38;2;%d;53;117[0-9;]*m(?:\x1b\[[0-9;]*m)*%s`, color, regexp.QuoteMeta(mark))
				if !regexp.MustCompile(glyph).MatchString(styled) {
					t.Errorf("%s mark does not use its agent color: %q", name, styled)
				}
				color++
			}
		}
		return
	}
	t.Fatalf("no agent legend:\n%s", ansi.Strip(strings.Join(lines, "\n")))
}

func TestHelpOverlayLayoutFitsPopupInterior(t *testing.T) {
	for _, tc := range []struct {
		width, boxWidth int
		twoColumns      bool
	}{{120, 72, true}, {72, 68, true}, {71, 67, true}, {70, 66, false}, {60, 56, false}} {
		m := lookModel(false)
		m.width, m.height, m.help = tc.width, 28, true
		r := m.helpRect()
		if r.w != tc.boxWidth {
			t.Errorf("pane %d: popup width = %d, want %d", tc.width, r.w, tc.boxWidth)
		}
		for _, row := range m.helpRows(tc.width) {
			if displayWidth(row) > r.w-4 {
				t.Errorf("pane %d: help row exceeds the popup interior: %q", tc.width, ansi.Strip(row))
			}
		}
		view := ansi.Strip(m.View().Content)
		aligned := regexp.MustCompile(`space +AND terms +↓ / esc +go to results`).MatchString(view)
		fallback := strings.Contains(view, "space AND terms · ↓ / esc go to results")
		if aligned != tc.twoColumns || fallback == tc.twoColumns {
			t.Errorf("pane %d: layout aligned=%v fallback=%v, want two columns=%v", tc.width, aligned, fallback, tc.twoColumns)
		}
	}
}

func TestHelpOverlayMasksRailInPopupRows(t *testing.T) {
	for _, width := range []int{72, 74, 120} {
		m := scrollModel()
		m.width, m.height, m.full = width, 30, true
		m.messages = append(m.messages, message{Index: 2, Text: "end"})
		background := strings.Split(ansi.Strip(m.View().Content), "\n")
		m.help = true
		r := m.helpRect()
		popup := strings.Split(ansi.Strip(m.View().Content), "\n")
		covered := map[string]bool{}
		for y := r.y; y < r.y+r.h; y++ {
			covered[ansi.Cut(background[y], width-1, width)] = true
			outside := ansi.Cut(popup[y], 0, r.x) + ansi.Cut(popup[y], r.x+r.w, width)
			if strings.TrimSpace(outside) != "" {
				t.Errorf("pane %d row %d: background leaked past popup frame: %q", width, y, outside)
			}
		}
		if !covered["┃"] || !covered["▪"] {
			t.Fatalf("pane %d: fixture didn't place both rail glyphs under popup rows: %v", width, covered)
		}
	}
}

func TestHelpOverlayNarrowFallback(t *testing.T) {
	m := lookModel(false)
	m.width, m.help = 60, true
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	r := m.helpRect()
	view := strings.Join(lines[r.y+1:r.y+r.h-1], "\n")
	for _, text := range []string{
		"space AND terms · ↓ / esc go to results",
		"alt+left", "backspace", "ctrl+u", "ctrl+w", "shift+tab",
		"pgup", "pgdown", "wheel", "click/drag rail", "OpenCode", "Grok", "ctrl+c quit",
	} {
		if !strings.Contains(view, text) {
			t.Errorf("narrow popup missing %q:\n%s", text, view)
		}
	}
}

func TestHelpRectTracksAnimatedOverlay(t *testing.T) {
	for _, width := range []int{60, 86} {
		m := lookModel(false)
		m.width, m.help = width, true
		for frame := helpFrames; frame >= 0; frame-- {
			m.helpAnim = frame
			view := strings.Split(ansi.Strip(m.View().Content), "\n")
			r := m.helpRect()
			if len(view) != m.height {
				t.Fatalf("width %d frame %d: rendered %d rows, want %d", width, frame, len(view), m.height)
			}
			for _, l := range view {
				if displayWidth(l) != width {
					t.Fatalf("width %d frame %d: row has width %d: %q", width, frame, displayWidth(l), l)
				}
			}
			left, _, hasTop := strings.Cut(view[r.y], "╭")
			right, _, hasRight := strings.Cut(view[r.y], "╮")
			bottom, _, hasBottom := strings.Cut(view[r.y+r.h-1], "╰")
			if !hasTop || !hasRight || !hasBottom || displayWidth(left) != r.x ||
				displayWidth(right)+1 != r.x+r.w || displayWidth(bottom) != r.x {
				t.Errorf("width %d frame %d: hit box %+v doesn't match rendered borders:\n%s", width, frame, r, strings.Join(view, "\n"))
			}
		}
	}
}

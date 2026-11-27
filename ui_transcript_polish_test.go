package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTranscriptTitlePrioritizesMetadataAndHitPosition(t *testing.T) {
	m := lookModel(false)
	m.rows[0].CWD = "/work/My Project"
	m.topics = map[string]string{"a": strings.Repeat("topic words ", 12)}
	m.messages = []message{
		{Index: 91, Text: "first text"},
		{Index: 97, Text: "other text"},
		{Index: 104, Text: "third text"},
	}
	m.hitIndex = map[int]bool{91: true, 97: false, 104: true}
	m.outline = true
	m.cursor = 2
	metadata := "  " + agentMark("claude") + " claude · kioku · 2026-11-04"
	for _, w := range []int{100, 64} {
		m.width = w
		raw := m.pageLines(w)[0]
		got := ansi.Strip(raw)
		if !strings.HasPrefix(got, metadata) || !strings.HasSuffix(got, "hit 2/2 · 100% · outline  ") || displayWidth(got) != w {
			t.Errorf("width %d: metadata or right-aligned status missing: %q", w, got)
		}
		if strings.Contains(got, "topic words") != (w == 100) {
			t.Errorf("width %d: topic should appear only with spare room: %q", w, got)
		}
		if !strings.Contains(raw, "\x1b]8;;file:///work/My%20Project") || strings.Count(raw, "\x1b]8;") != 2 {
			t.Errorf("width %d: project directory link lost or unterminated: %q", w, raw)
		}
	}
	m.rows[0].Project = strings.Repeat("很長的專案", 10)
	if got := ansi.Strip(m.pageLines(m.width)[0]); !strings.Contains(got, "… · 2026-11-04") || !strings.HasSuffix(got, "hit 2/2 · 100% · outline  ") {
		t.Errorf("a long project must not crowd out the date or hit counter: %q", got)
	}
	m.cursor = 1
	if got := ansi.Strip(m.pageLines(m.width)[0]); !strings.HasSuffix(got, "2 hits · 66% · outline  ") {
		t.Errorf("non-hit cursor should show the session hit count: %q", got)
	}
	m.hitIndex = nil
	if got := ansi.Strip(m.pageLines(m.width)[0]); !strings.HasSuffix(got, "66% · outline  ") || strings.Contains(got, "hits") {
		t.Errorf("zero hits should leave only percent and outline: %q", got)
	}
}

func TestTranscriptWrappedGutterAndReadingBand(t *testing.T) {
	m := lookModel(false)
	m.width, m.height, m.full, m.q = 160, 30, true, ""
	m.messages = []message{
		{Index: 3, Role: "user", Text: strings.Repeat("word ", 24), TS: "2026-11-04T14:02:00Z"},
		{Index: 9, Role: "assistant", Text: strings.Repeat("reply ", 20), TS: "2026-11-04T14:03:00Z"},
	}
	const gutterCol, textCol, textWidth = 11, 19, 74
	for _, ticks := range []int{0, pulseFrameCount} {
		m.pulseTicks = ticks
		bg := m.pal.selbg
		if ticks > 0 {
			bg = m.pal.pulse
		}
		band := ansi.Style{}.BackgroundColor(lipgloss.Color(bg)).String()
		var selectedRows, assistantRows int
		for _, raw := range m.pageLines(m.width)[2:] {
			line := ansi.Strip(raw)
			selected := strings.Contains(line, "word")
			if !selected && !strings.Contains(line, "reply") {
				continue
			}
			cells := []rune(line)
			if content := strings.TrimSpace(string(cells[textCol : len(cells)-2])); displayWidth(content) > textWidth {
				t.Errorf("reading width exceeds %d: %q", textWidth, content)
			}
			if selected {
				selectedRows++
				if cells[0] != '▎' || cells[gutterCol] != '┃' {
					t.Errorf("selected line should keep both accent and role rule: %q", line)
				}
				end := strings.Index(raw, "\x1b[49m")
				if end < 0 || !strings.HasPrefix(raw, band) || displayWidth(raw[:end]) != textCol+textWidth+1 {
					t.Errorf("selection background should end at column %d before the rail: %q", textCol+textWidth+1, raw)
				}
			} else {
				assistantRows++
				if cells[0] != ' ' || cells[gutterCol] != '│' || strings.Contains(raw, band) {
					t.Errorf("unselected line should keep only the muted rule: %q", line)
				}
			}
		}
		if selectedRows != 2 || assistantRows != 2 {
			t.Fatalf("want two wrapped rows each, got user=%d assistant=%d", selectedRows, assistantRows)
		}
	}
}

func TestHitPositionFollowsTranscriptOrder(t *testing.T) {
	m := lookModel(false)
	m.messages = []message{{Index: 42}, {Index: 1}, {Index: 83}}
	m.hitIndex = map[int]bool{42: true, 1: false, 83: true, 99: true}
	for _, tc := range []struct{ cursor, position int }{{-1, 0}, {0, 1}, {1, 0}, {2, 2}, {3, 0}} {
		m.cursor = tc.cursor
		if pos, total := m.hitPosition(); pos != tc.position || total != 2 {
			t.Errorf("cursor %d: got (%d, %d), want (%d, 2)", tc.cursor, pos, total, tc.position)
		}
	}
	m.q, m.cursor = "-fts5", 0
	if pos, total := m.hitPosition(); pos != 0 || total != 0 {
		t.Fatalf("inactive search should ignore stale hits, got (%d, %d)", pos, total)
	}
}

func TestTranscriptOutlineRectMatchesBadge(t *testing.T) {
	m := lookModel(false)
	m.outline = true
	for _, w := range []int{25, 64, 100} {
		m.width = w
		r := m.outlineRect()
		lines := strings.Split(m.View().Content, "\n")
		if r.h != 1 || r.y != m.layout().titleBar.y || ansi.Strip(ansi.Cut(lines[r.y], r.x, r.x+r.w)) != "outline" {
			t.Fatalf("width %d: outline badge does not match rect %+v", w, r)
		}
	}
	m.outline = false
	if r := m.outlineRect(); r.w != 0 {
		t.Fatalf("hidden badge should not have a click target: %+v", r)
	}
}

func TestTranscriptVisibleMessageRowsFollowScrollingAndFolds(t *testing.T) {
	m := lookModel(false)
	m.height, m.full, m.outline, m.q = 12, true, true, ""
	m.messages = []message{
		{Index: 17, Role: "user", Text: strings.Repeat("first\n", 10) + "first"},
		{Index: 24, Role: "assistant", Text: "hidden", TS: "2026-11-04T14:02:00Z"},
		{Index: 33, Role: "assistant", Text: "last", TS: "2026-11-04T14:03:00Z"},
	}
	for _, w := range []int{35, 80} {
		m.width = w
		m.transcriptOffset = 999
		lines := m.pageLines(w)[2:]
		var foundFirst, foundFold, foundLast bool
		for n, idx := range m.rail.messageRows {
			body := ansi.Strip(lines[n])
			switch {
			case strings.Contains(body, "first"):
				foundFirst = idx == 0
			case strings.Contains(body, "1 steps"):
				foundFold = idx == -1
			case strings.Contains(body, "last"):
				foundLast = idx == 2
			}
		}
		if m.rail.start == 0 || !foundFirst || !foundFold || !foundLast {
			t.Fatalf("width %d: visible rows don't match scrolled messages: start=%d, rows=%v, lines=%q", w, m.rail.start, m.rail.messageRows, lines)
		}
	}
}

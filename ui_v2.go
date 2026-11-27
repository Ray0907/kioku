package main

import (
	"fmt"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// dirLink makes text a clickable file:// link to dir (OSC 8) when the terminal supports it.
func dirLink(style lipgloss.Style, dir string) lipgloss.Style {
	if dir == "" {
		return style
	}
	return style.Hyperlink((&url.URL{Scheme: "file", Path: dir}).String())
}

// windowTitle names the terminal tab after the current search, so several kioku panes stay apart.
func (m model) windowTitle() string {
	if toFTS(m.q) == "" {
		return "kioku"
	}
	n := max(len(m.rows), m.total)
	return fmt.Sprintf("kioku · %s · %d hits", displayInline(m.q), n)
}

// progressBar mirrors first-run indexing in the terminal's own progress indicator (tab, dock).
func (m model) progressBar() *tea.ProgressBar {
	if m.progress == "" {
		return nil
	}
	if m.pct <= 0 {
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, m.pct)
}

// onFocus pauses timed syncing while the window is in the background and catches up once the
// user comes back, so a session that ended meanwhile is searchable the moment they look.
// It does nothing when background syncing is off (the default).
func (m model) onFocus(in bool) (tea.Model, tea.Cmd) {
	m.blurred = !in
	if in && syncEvery > 0 && m.synced && !m.syncing {
		m.syncing = true
		return m, startSync(true)
	}
	return m, nil
}

// helpBounds is shared by the renderer and mouse hit-testing, including during the reveal.
func (m model) helpBounds(w, h, lines int) rect {
	shown := lines - lines*m.helpAnim/(helpFrames+1)
	boxW, boxH := helpBoxWidth(w), min(h, max(shown, 1)+2)
	return rect{x: (w - boxW) / 2, y: (h - boxH) / 2, w: boxW, h: boxH}
}

func (m model) helpRect() rect {
	return m.helpBounds(m.width, m.height, len(m.helpRows(m.width)))
}

// The popup grows open over helpFrames short ticks so it reads as arriving rather than blinking in.
const helpFrames = 3

type helpTickMsg struct{}

func helpTick() tea.Cmd {
	return tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg { return helpTickMsg{} })
}

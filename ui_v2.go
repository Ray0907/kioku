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

// helpBox places the help popup: centred, as large as its text, never larger than the screen.
func helpBox(inner, lines, w, h int) (x0, y0, boxW, boxH int) {
	boxW, boxH = min(w, inner+4), min(h, lines+2)
	return (w - boxW) / 2, max(0, (h-boxH)/2), boxW, boxH
}

// helpRect is the screen area the popup covers, for click handling.
func (m model) helpRect() rect {
	lines, inner := helpLines(), 0
	for _, l := range lines {
		inner = max(inner, displayWidth(l))
	}
	x0, y0, bw, bh := helpBox(inner, len(lines), m.width, m.height)
	return rect{x: x0, y: y0, w: bw, h: bh}
}

// The popup grows open over helpFrames short ticks so it reads as arriving rather than blinking in.
const helpFrames = 3

type helpTickMsg struct{}

func helpTick() tea.Cmd {
	return tea.Tick(35*time.Millisecond, func(time.Time) tea.Msg { return helpTickMsg{} })
}

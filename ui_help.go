package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// helpLines lists every handled key, grouped by where it applies. It must fit a 24-row terminal
// inside the popup frame (22 rows) and 72 columns.
func helpLines() []string {
	var agents []string
	for _, h := range harnesses {
		agents = append(agents, h.mark+" "+h.label)
	}
	half := (len(agents) + 1) / 2
	return []string{
		"Search box",
		"  space AND · quotes phrase · -word exclude",
		"  ↓ / esc results · / or ctrl+f back to search",
		"  left / right or ctrl+b / ctrl+f caret · home / end or ctrl+a / ctrl+e",
		"  alt+left / alt+right word · delete forward · backspace before",
		"  ctrl+u to line start · ctrl+w previous word",
		"  ↑ / ↓ in an empty query: query history",
		"Results",
		"  ↑ / k  ↓ / j move · tab / shift+tab agent · enter resume in cwd",
		"  y copy resume command · o open directory in editor",
		"  r sync and re-run · p project scope · d / D date range",
		"  h / H add / clear highlighter",
		"Transcript",
		"  n / N next / previous hit · pgup / pgdown scroll · wheel scrolls",
		"  O outline vs all steps · v full transcript · t tool output",
		"  mouse: drag title bar to resize (double-click resets), click/drag rail",
		"",
		"  agents  " + strings.Join(agents[:half], "  "),
		"          " + strings.Join(agents[half:], "  "),
		"  ? or esc close · esc cancel prompt / leave query / quit · ctrl+c quit",
	}
}

// overlayHelp paints the help as a centred, opaque box over the screen the user was looking at.
func (m model) overlayHelp(rows []string, w int) []string {
	lines := helpLines()
	inner := 0
	for _, l := range lines {
		inner = max(inner, displayWidth(l))
	}
	x0, y0, boxW, boxH := helpBox(inner, len(lines), w, len(rows))
	if boxW < 12 || boxH < 3 {
		return rows
	}
	lines = lines[:boxH-2]
	edge := m.color
	top := edge("╭"+strings.Repeat("─", boxW-2)+"╮", m.pal.muted)
	bottom := edge("╰"+strings.Repeat("─", boxW-2)+"╯", m.pal.muted)
	strong := lipgloss.NewStyle().Bold(true)
	box := []string{top}
	for _, l := range lines {
		body := clip(l, boxW-4)
		if !strings.HasPrefix(body, " ") && body != "" {
			body = strong.Render(body) // group titles
		}
		row := edge("│", m.pal.muted) + " " + pad(body, boxW-4) + " " + edge("│", m.pal.muted)
		box = append(box, m.bgRow(row, boxW, m.pal.surf))
	}
	box = append(box, bottom)
	out := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(rows, "\n")),
		lipgloss.NewLayer(strings.Join(box, "\n")).X(x0).Y(y0).Z(1),
	).Render()
	res := strings.Split(out, "\n") // the compositor trims trailing blanks; restore the width
	for i := range res {
		res[i] = fitLine(res[i], w)
	}
	for len(res) < len(rows) {
		res = append(res, fitLine("", w))
	}
	return res
}

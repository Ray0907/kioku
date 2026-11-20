package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// helpMarked is the help text with each key wrapped in keyOpen/keyClose so the popup can set keys
// apart from what they do.
const (
	keyOpen  = "\x02"
	keyClose = "\x03"
)

func k(s string) string { return keyOpen + s + keyClose }

func helpMarked() []string {
	var agents []string
	for _, h := range harnesses {
		agents = append(agents, h.mark+" "+h.label)
	}
	half := (len(agents) + 1) / 2
	return []string{
		"Search box",
		"  " + k("space") + " AND · " + k("quotes") + " phrase · " + k("-word") + " exclude",
		"  " + k("↓") + " / " + k("esc") + " results · " + k("/") + " or " + k("ctrl+f") + " back to search",
		"  " + k("left") + " / " + k("right") + " or " + k("ctrl+b") + " / " + k("ctrl+f") + " caret · " + k("home") + " / " + k("end") + " or " + k("ctrl+a") + " / " + k("ctrl+e"),
		"  " + k("alt+left") + " / " + k("alt+right") + " word · " + k("delete") + " forward · " + k("backspace") + " before",
		"  " + k("ctrl+u") + " to line start · " + k("ctrl+w") + " previous word",
		"  " + k("↑") + " / " + k("↓") + " in an empty query: query history",
		"",
		"Results",
		"  " + k("↑") + " / " + k("k") + " up · " + k("↓") + " / " + k("j") + " down · " + k("tab") + " / " + k("shift+tab") + " agent · " + k("enter") + " resume in cwd",
		"  " + k("y") + " copy resume command · " + k("o") + " open directory in editor",
		"  " + k("r") + " sync and re-run · " + k("p") + " project scope · " + k("d") + " / " + k("D") + " date range",
		"  " + k("h") + " / " + k("H") + " add / clear highlighter",
		"",
		"Transcript",
		"  " + k("n") + " / " + k("N") + " next / previous hit · " + k("pgup") + " / " + k("pgdown") + " scroll · " + k("wheel") + " scrolls",
		"  " + k("O") + " outline vs all steps · " + k("v") + " full transcript · " + k("t") + " tool output",
		"  " + k("mouse") + ": drag title bar to resize (double-click resets), click/drag rail",
		"  agents  " + strings.Join(agents[:half], "  "),
		"          " + strings.Join(agents[half:], "  "),
		"  " + k("?") + " or " + k("esc") + " close · " + k("esc") + " cancel prompt / leave query / quit · " + k("ctrl+c") + " quit",
	}
}

// helpLines lists every handled key, grouped by where it applies. It must fit a 24-row terminal
// inside the popup frame (22 rows) and 72 columns.
func helpLines() []string {
	out := helpMarked()
	for i, l := range out {
		out[i] = strings.NewReplacer(keyOpen, "", keyClose, "").Replace(l)
	}
	return out
}

// styleHelp paints group titles in the accent colour, keys bold, and the rest muted.
func (m model) styleHelp(l string) string {
	if l == "" {
		return ""
	}
	if !strings.HasPrefix(l, " ") {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.pal.acc)).Render(l)
	}
	key := lipgloss.NewStyle().Bold(true)
	var sb strings.Builder
	for _, part := range strings.SplitAfter(l, keyClose) {
		i := strings.Index(part, keyOpen)
		if i < 0 {
			sb.WriteString(m.color(part, m.pal.muted))
			continue
		}
		sb.WriteString(m.color(part[:i], m.pal.muted))
		sb.WriteString(key.Render(strings.NewReplacer(keyOpen, "", keyClose, "").Replace(part[i:])))
	}
	return sb.String()
}

// overlayHelp paints the help as a centred, opaque box over the screen the user was looking at.
func (m model) overlayHelp(rows []string, w int) []string {
	lines := helpLines()
	styled := helpMarked()
	inner := 0
	for _, l := range lines {
		inner = max(inner, displayWidth(l))
	}
	shown := len(lines) - len(lines)*m.helpAnim/(helpFrames+1) // opening: reveal the lines from the top
	x0, y0, boxW, boxH := helpBox(inner, max(shown, 1), w, len(rows))
	if boxW < 12 || boxH < 3 {
		return rows
	}
	lines, styled = lines[:boxH-2], styled[:boxH-2]
	edge := m.color
	top := edge("╭"+strings.Repeat("─", boxW-2)+"╮", m.pal.muted)
	bottom := edge("╰"+strings.Repeat("─", boxW-2)+"╯", m.pal.muted)
	box := []string{top}
	for i, l := range lines {
		body := m.styleHelp(styled[i])
		if displayWidth(l) > boxW-4 {
			body = clip(l, boxW-4) // too narrow to style safely
		}
		row := edge("│", m.pal.muted) + " " + pad(body, boxW-4) + " " + edge("│", m.pal.muted)
		box = append(box, m.bgRow(row, boxW, m.pal.surf))
	}
	box = append(box, bottom)
	back := make([]string, len(rows)) // dim the screen behind the popup so only the popup reads as active
	for i, r := range rows {
		back[i] = "\x1b[2m" + sgrCode.ReplaceAllStringFunc(r, func(c string) string { return c + "\x1b[2m" }) + "\x1b[22m"
	}
	out := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(back, "\n")),
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

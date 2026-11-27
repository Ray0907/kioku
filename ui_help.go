package main

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func helpBoxWidth(w int) int { return min(w-4, 72) }

func (m model) helpRows(w int) []string {
	const leftKey, leftDesc, rightKey, rightDesc = 19, 15, 14, 15
	innerWidth := helpBoxWidth(w) - 4
	twoColumns := innerWidth >= leftKey+leftDesc+rightKey+rightDesc
	strong := lipgloss.NewStyle().Bold(true)
	key := strong.Render
	title := strong.Foreground(lipgloss.Color(m.pal.acc)).Render
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(m.pal.muted)).Render
	pair := func(k1, d1, k2, d2 string) string {
		if !twoColumns {
			return "  " + key(k1) + " " + muted(d1+" · ") + key(k2) + " " + muted(d2)
		}
		return pad(key(k1), leftKey) + pad(muted(d1), leftDesc) + pad(key(k2), rightKey) + muted(d2)
	}
	rows := []string{
		title("Search"),
		pair("space", "AND terms", "↓ / esc", "go to results"),
		pair(`"quotes"`, "exact phrase", "/ or ctrl+f", "focus search"),
		pair("-word", "exclude term", "↑ / ↓", "history (empty)"),
		pair("left / right", "move caret", "ctrl+b/ctrl+f", "move caret"),
		pair("home / end", "line ends", "ctrl+a/ctrl+e", "line ends"),
		pair("alt+left/alt+right", "by word", "ctrl+w", "del prev word"),
		pair("delete/backspace", "del next/prev", "ctrl+u", "del to start"),
		title("Results & Filters"),
		pair("↑/k · ↓/j", "move selection", "tab/shift+tab", "cycle agent"),
		pair("enter", "resume in cwd", "p", "project scope"),
		pair("y / o", "copy / editor", "d / D", "set/clear range"),
		pair("r", "sync & re-run", "h / H", "highlight/clear"),
		title("Transcript"),
		pair("n / N", "next/prev hit", "O", "outline vs all"),
		pair("pgup/pgdown/wheel", "scroll", "v / t", "full/show tools"),
		"  " + key("mouse") + muted(" drag title; dbl-click reset; click/drag rail"),
	}
	labels := []string{"Claude", "AgY", "Codex", "Hermes", "Pi", "Cursor", "OpenCode", "Grok"}
	var legend []string
	for _, label := range labels {
		if h := findHarness(strings.ToLower(label)); h != nil {
			legend = append(legend, m.agentGlyph(h.name)+" "+muted(label))
		}
	}
	for _, h := range harnesses {
		if !slices.ContainsFunc(labels, func(label string) bool { return strings.EqualFold(label, h.name) }) {
			legend = append(legend, m.agentGlyph(h.name)+" "+muted(h.label))
		}
	}
	prefix := title("Agents") + "  "
	for group := range slices.Chunk(legend, 4) {
		row := prefix
		for _, entry := range group[:len(group)-1] {
			row += pad(entry, 11)
		}
		rows = append(rows, row+group[len(group)-1])
		prefix = "        "
	}
	rows = append(rows, "  "+key("?/esc")+muted(" close · ")+key("esc")+muted(" cancel/leave/quit · ")+key("ctrl+c")+muted(" quit"))
	if !twoColumns {
		return strings.Split(ansi.Wrap(strings.Join(rows, "\n"), innerWidth, ""), "\n")
	}
	return rows
}

// overlayHelp paints the help as a centred, opaque box over the screen the user was looking at.
func (m model) overlayHelp(rows []string, w int) []string {
	lines := m.helpRows(w)
	r := m.helpBounds(w, len(rows), len(lines))
	if r.w < 12 || r.h < 3 {
		return rows
	}
	edge := m.color
	top := edge("╭"+strings.Repeat("─", r.w-2)+"╮", m.pal.muted)
	bottom := edge("╰"+strings.Repeat("─", r.w-2)+"╯", m.pal.muted)
	box := []string{m.bgRow(top, r.w, m.pal.surf)}
	for _, l := range lines[:r.h-2] {
		row := edge("│", m.pal.muted) + " " + fitLine(l, r.w-4) + " " + edge("│", m.pal.muted)
		box = append(box, m.bgRow(row, r.w, m.pal.surf))
	}
	box = append(box, m.bgRow(bottom, r.w, m.pal.surf))
	// Replace the whole popup row so the transcript rail cannot show beside the frame.
	before, after := strings.Repeat(" ", r.x), strings.Repeat(" ", w-r.x-r.w)
	for y, row := range rows {
		if i := y - r.y; i >= 0 && i < len(box) {
			rows[y] = before + box[i] + after
		} else {
			rows[y] = faint(row)
		}
	}
	return rows
}

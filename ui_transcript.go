package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	transcriptTitleInset = "  "
	transcriptOutline    = "outline"
)

// hitPosition reports transcript-ordered hits; position is zero off a hit.
func (m model) hitPosition() (position, total int) {
	if len(m.hitIndex) == 0 || toFTS(m.q) == "" {
		return 0, 0
	}
	for i, x := range m.messages {
		if m.hitIndex[x.Index] {
			total++
			if i == m.cursor {
				position = total
			}
		}
	}
	return position, total
}

func (m model) outlineRect() rect {
	bar := m.layout().titleBar
	if !m.outline || bar.w == 0 {
		return rect{}
	}
	return rect{x: bar.x + bar.w - len(transcriptTitleInset) - len(transcriptOutline), y: bar.y, w: len(transcriptOutline), h: bar.h}
}

func (m model) transcriptTitle(w int) string {
	sel := m.rows[m.sel]
	strong := lipgloss.NewStyle().Bold(true)
	accent := strong.Foreground(lipgloss.Color(m.pal.acc))
	dot := m.color(" · ", m.pal.muted)
	right := m.color(m.readPercent(), m.pal.muted)
	if m.outline {
		if right != "" {
			right += dot
		}
		right += accent.Render(transcriptOutline)
	}
	if pos, total := m.hitPosition(); total > 0 {
		label := fmt.Sprintf("%d hits", total)
		if pos > 0 {
			label = fmt.Sprintf("hit %d/%d", pos, total)
		}
		hits := accent.Render(label)
		if right != "" {
			hits += dot
		}
		if displayWidth(hits+right)+len(transcriptTitleInset) <= w {
			right = hits + right
		}
	}
	leftW := w - displayWidth(right) - len(transcriptTitleInset)
	left := transcriptTitleInset + m.agentGlyph(sel.Harness) + m.color(" "+sel.Harness, m.pal.muted)
	date := ""
	if sel.TS != "" {
		date = dot + m.color(sel.TS[:min(10, len(sel.TS))], m.pal.muted)
	}
	project := displayInline(sel.Project)
	if room := leftW - displayWidth(left+dot+date) - 1; room > 0 && project != "" {
		left += dot + dirLink(strong, sel.CWD).Render(ansi.Truncate(project, room, "…"))
	}
	left += date
	if room := leftW - displayWidth(left) - 4; room > 14 {
		topic := m.topics[sel.UID]
		if topic == "" {
			topic = messagesTopic(m.messages)
		}
		if topic != "" {
			left += dot + m.color(clip(displayInline(topic), room), m.pal.muted)
		} else if cwd := sel.CWD; cwd != "" {
			if home, e := os.UserHomeDir(); e == nil && home != "" && strings.HasPrefix(cwd, home+"/") {
				cwd = "~" + cwd[len(home):]
			}
			left += dot + m.pathLink(sel.CWD, clip(displayInline(cwd), room))
		}
	}
	titleBg := m.pal.surf
	if m.drag == dragSplit {
		titleBg = m.pal.selbg
	}
	header := pad(clipANSI(left, leftW-1), leftW) + right + transcriptTitleInset
	return m.bgRow(header, w, titleBg)
}

func (m *model) pageLines(w int) []string {
	if len(m.rows) == 0 {
		m.transcriptOffset = 0
		m.rail = railState{}
		return nil
	}
	active := toFTS(m.q) != ""
	out := []string{m.transcriptTitle(w), ""}
	keep := map[int]bool{}
	start, end := 0, len(m.messages)
	// ponytail: render a 200-message window for huge sessions; virtualize line offsets if direct arbitrary scrolling is needed.
	if end > 200 {
		start = max(0, m.cursor-100)
		end = min(end, start+200)
	}
	var skeleton []bool
	if m.outline {
		skeleton = outlineKeep(m.messages)
	}
	for i := start; i < end; i++ {
		x := m.messages[i]
		if i == m.cursor || (active && m.hitIndex[x.Index]) {
			keep[i] = true
			if !m.outline && !m.full && active {
				if i > start {
					keep[i-1] = true
				}
				if i+1 < end {
					keep[i+1] = true
				}
				for j := i; j >= start; j-- {
					if m.messages[j].Role == "user" {
						keep[j] = true
						break
					}
				}
			}
			continue
		}
		if m.outline {
			if skeleton[i] {
				keep[i] = true
			}
		} else if m.full || !active {
			keep[i] = true
		}
	}
	// Only the messages that can reach the screen are painted: pass one lists the kept
	// messages cheaply, pass two renders outward from the selected one until the pane is full.
	type item struct {
		i    int
		lead string // fold marker shown above this message, if any
	}
	var items []item
	prev, folded, skipped := -1, start > 0, start
	foldRow := func(n int) string {
		if n <= 0 {
			return m.color("           ⋯", m.pal.dim)
		}
		if m.outline {
			return m.color(fmt.Sprintf("           ⋯ %d steps", n), m.pal.dim)
		}
		return m.color(fmt.Sprintf("           ⋯ %d hidden", n), m.pal.dim)
	}
	for i := start; i < end; i++ {
		if !keep[i] {
			folded = true
			skipped++
			continue
		}
		it := item{i: i}
		if folded || prev >= 0 && gap(m.messages[prev].TS, m.messages[i].TS) {
			it.lead = foldRow(skipped)
		}
		items = append(items, it)
		folded, skipped, prev = false, 0, i
	}
	trail := ""
	if folded || end < len(m.messages) {
		trail = foldRow(skipped + len(m.messages) - end)
	}
	hasRail := m.hasRail()
	bodyW := w
	if hasRail {
		bodyW = max(1, w-2)
	}
	render := func(it item) []string {
		var lines []string
		if it.lead != "" {
			lines = append(lines, it.lead)
		}
		i := it.i
		x := m.messages[i]
		who := clip(displayInline(x.Role), 4)
		if x.Role == "user" {
			who = "you"
		} else if x.Role == "assistant" {
			who = "asst"
		}
		selected := i == m.cursor
		isHit := active && m.hitIndex[x.Index]
		context := !m.full && active && !isHit
		edge, star := "  ", " "
		if selected {
			edge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.pal.acc)).Render("▎") + " "
			star = lipgloss.NewStyle().Bold(true).Render("›")
		} else if isHit {
			star = m.color("✱", m.pal.hit)
		}
		stamp := x.TS
		if len(stamp) >= 16 {
			stamp = stamp[11:16]
		}
		stampText := m.color(pad(clip(stamp, 5), 5), m.pal.muted)
		roleText := m.color(pad(who, 4), m.pal.muted)
		if who == "you" {
			style := lipgloss.NewStyle().Bold(true)
			if context || m.focus {
				style = style.Foreground(lipgloss.Color(m.pal.muted))
			}
			roleText = style.Render(pad(who, 4))
		}
		if selected {
			strong := lipgloss.NewStyle().Bold(true)
			stampText = strong.Render(pad(clip(stamp, 5), 5))
			roleText = strong.Render(pad(who, 4))
		}
		rule := m.color("│", m.pal.muted)
		if x.Role == "user" {
			ruleColor := m.pal.acc
			if m.focus {
				ruleColor = m.pal.muted
			}
			rule = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ruleColor)).Render("┃")
		}
		const gutterCol, textCol = 11, 19
		prefix := edge + star + "  " + stampText + " " + rule + " " + roleText + "  "
		continuation := edge + strings.Repeat(" ", gutterCol-2) + rule + strings.Repeat(" ", textCol-gutterCol-1)
		textW := min(maxReadingWidth, max(1, bodyW-textCol-2))
		body := wrap(displayTranscript(x.Text), textW)
		folded := x.Role == "tool" && !m.toolsOpen && !selected && !isHit
		if folded { // tool output is evidence, not reading: one line unless it is selected or matches
			all := strings.Split(strings.TrimSpace(displayTranscript(x.Text)), "\n")
			first := all[0]
			if n := len(all) - 1; n > 0 {
				first += fmt.Sprintf("  (+%d lines)", n)
			}
			body = []string{clip(first, textW)}
		}
		if m.outline && !selected && !isHit && len(body) > contextLines { // outline: context is a glance, not a read
			body = append(body[:contextLines-1:contextLines-1], clip(body[contextLines-1], textW-1)+"…")
		}
		selHex := m.pal.selbg
		if selected && m.pulseTicks > 0 {
			selHex = m.pal.pulse
		}
		for j, line := range body {
			p := prefix
			if j > 0 {
				p = continuation
			}
			line = m.paint(line, isHit, selected)
			if (context || folded || (m.focus && !isHit)) && !selected {
				line = m.dimUnstyled(line)
			}
			line = p + line
			if selected {
				line = m.bgRow(line, min(bodyW, textCol+textW+1), selHex)
			}
			lines = append(lines, clipANSI(line, bodyW))
		}
		return append(lines, "")
	}
	available := m.pageHeight()
	room := max(0, available-2)
	fi := -1
	for k, it := range items {
		if it.i == m.cursor {
			fi = k
			break
		}
	}
	rendered := make([][]string, len(items))
	get := func(k int) []string {
		if rendered[k] == nil {
			rendered[k] = render(items[k])
		}
		return rendered[k]
	}
	lo, hi, before, after := 0, 0, 0, 0
	if fi >= 0 {
		lo, hi = fi, fi+1
		after = len(get(fi))
	}
	// Extend until half a pane lies above the selection and a full pane below it, or an end is hit.
	trailRows := 0
	if trail != "" {
		trailRows = 1
	}
	needAbove := func() bool {
		// Include the upward offset above a bottom-clamped viewport, not just above the selection.
		return lo > 0 && (before < room/2+1+max(0, -m.transcriptOffset) || hi == len(items) && before+after+trailRows < room+max(0, -m.transcriptOffset))
	}
	needBelow := func() bool { return hi < len(items) && after < room+1+max(0, m.transcriptOffset) }
	for needAbove() || needBelow() {
		if needAbove() {
			lo--
			before += len(get(lo))
		}
		if needBelow() {
			after += len(get(hi))
			hi++
		}
	}
	var body []string
	var messageRows []int
	focus := 0
	for k := lo; k < hi; k++ {
		if k == fi {
			focus = len(body)
			if items[k].lead != "" {
				focus++
			}
		}
		lines := get(k)
		body = append(body, lines...)
		for j := range lines {
			idx := items[k].i
			if j == 0 && items[k].lead != "" {
				idx = -1
			}
			messageRows = append(messageRows, idx)
		}
	}
	if hi == len(items) && trail != "" {
		body = append(body, trail)
		messageRows = append(messageRows, -1)
	}
	viewStart := 0
	base := 0
	if len(body) > room && room > 0 {
		base = max(0, min(len(body)-room, focus-room/2))
		viewStart = max(0, min(len(body)-room, base+m.transcriptOffset))
		m.transcriptOffset = viewStart - base
		body = body[viewStart:]
	} else {
		m.transcriptOffset = 0
	}
	totalLines := viewStart + len(body)
	messageRows = messageRows[viewStart:min(totalLines, viewStart+room)]
	// Populate the 1-cell Hit-Map rail state and stamp it on the right edge.
	hitRows := map[int]int{}
	cursorRow := -1
	if hasRail && room > 0 {
		nMsgs := len(m.messages)
		mapRow := func(idx int) int {
			if nMsgs <= 1 || room <= 1 {
				return 0
			}
			return idx * (room - 1) / (nMsgs - 1)
		}
		for i, x := range m.messages {
			if active && m.hitIndex[x.Index] {
				r := mapRow(i)
				if _, exists := hitRows[r]; !exists || i == m.cursor {
					hitRows[r] = i
				}
			}
		}
		if m.cursor >= 0 && m.cursor < nMsgs && active && m.hitIndex[m.messages[m.cursor].Index] {
			cursorRow = mapRow(m.cursor)
		}
		m.rail = railState{
			total:       totalLines,
			start:       viewStart,
			visible:     room,
			base:        base,
			hitRows:     hitRows,
			cursorRow:   cursorRow,
			messageRows: messageRows,
		}
		thumbStart, thumbEnd := 0, room-1
		if totalLines > room && room > 0 {
			thumbH := max(1, room*room/totalLines)
			thumbStart = viewStart * max(0, room-thumbH) / max(1, totalLines-room)
			thumbEnd = min(room-1, thumbStart+thumbH-1)
		}
		thumbColor := m.pal.muted
		if m.drag == dragRail {
			thumbColor = m.pal.acc
		}
		visibleRows := make([]string, room)
		for r := 0; r < room; r++ {
			line := ""
			if r < len(body) {
				line = body[r]
			}
			var glyph string
			switch {
			case r == cursorRow:
				glyph = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.pal.acc)).Render("┃")
			case hitRows[r] != 0 || (len(hitRows) > 0 && hasHitRow(hitRows, r)):
				glyph = m.color("▪", m.pal.hit)
			case r >= thumbStart && r <= thumbEnd && totalLines > room:
				glyph = m.color("┃", thumbColor)
			default:
				glyph = m.color("│", m.pal.dim)
			}
			visibleRows[r] = fitLine(line, bodyW) + " " + glyph
		}
		out = append(out, visibleRows...)
	} else {
		m.rail = railState{total: totalLines, start: viewStart, visible: room, base: base, messageRows: messageRows}
		out = append(out, body...)
	}
	return out
}

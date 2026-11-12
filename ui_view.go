package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m model) View() string {
	if m.width < 25 || m.height < 10 {
		return fitLine("kioku · enlarge terminal", max(0, m.width))
	}
	w := m.width
	var b strings.Builder
	count := ""
	if m.progress != "" {
		count = m.progress
	} else if toFTS(m.q) == "" {
		count = fmt.Sprintf("%d recent sessions", len(m.rows))
	} else {
		sessions := map[string]bool{}
		for _, r := range m.rows {
			sessions[r.UID] = true
		}
		count = fmt.Sprintf("%d messages · %d sessions", len(m.rows), len(sessions))
		if m.total > len(m.rows) {
			count = fmt.Sprintf("%d of %d messages · %d sessions", len(m.rows), m.total, m.totalSessions)
		} else if m.total == 0 && len(m.rows) > 0 && len(m.rows) == m.limit {
			count += " shown"
		}
	}
	if m.fresh > 0 && m.progress == "" {
		count += fmt.Sprintf(" · %d new · r refreshes", m.fresh)
	}
	if m.root != "" {
		count = displayInline(filepath.Base(m.root)) + " · " + count
	} else if m.rng.active() {
		count = m.rng.label() + " · " + count
	}
	strong := lipgloss.NewStyle().Bold(true)
	q := "  " + strong.Foreground(lipgloss.Color(m.pal.muted)).Render("kioku") + " ▸ "
	input := displayInline(m.q)
	if m.focus {
		input += "▌"
	}
	right := m.color(clip(count, max(0, w-displayWidth(q)-4)), m.pal.muted)
	inputW := max(0, w-displayWidth(q)-displayWidth(right)-2)
	b.WriteString(m.bgRow(q+pad(strong.Render(clip(input, max(0, inputW-2))), inputW)+right+"  ", w, m.pal.surf) + "\n")
	if len(m.pens) > 0 {
		ps := m.color("  highlights ", m.pal.muted)
		for _, p := range m.pens {
			ps += lipgloss.NewStyle().Background(lipgloss.Color(m.pal.pens[p.color])).Render(" "+displayInline(p.word)+" ") + " "
		}
		b.WriteString(clipANSI(ps, w-2) + "\n")
	}
	b.WriteByte('\n')
	listH := m.listHeight()
	if !m.full {
		for i := 0; i < listH; i++ {
			idx := m.offset + i
			if idx >= len(m.rows) {
				if idx == 0 && m.rng.active() {
					b.WriteString(m.color("  No message in this date range. Press D to clear it.", m.pal.muted))
				} else if idx == 0 {
					b.WriteString(m.color("  No message contains all of these words. Drop a word or remove the quotes.", m.pal.muted))
				}
				b.WriteString("\n")
				continue
			}
			x := m.rows[idx]
			selected := idx == m.sel
			const pjW, ageW = 13, 4
			sw := max(1, w-5-pjW-ageW-2)
			sn := clip(tidySnippet(displayInline(x.Snippet)), max(1, sw-2))
			cursor := " "
			agent := m.agentGlyph(x.Harness)
			project := m.color(clip(displayInline(x.Project), pjW-1), m.pal.muted)
			when := m.color(fmt.Sprintf("%*s", ageW, age(x.TS)), m.pal.muted)
			band := selected && !m.focus // with the cursor in the query box the list only carries a quiet ›
			if selected {
				cursor = strong.Render("›")
				if band {
					cursor = strong.Foreground(lipgloss.Color(m.pal.acc)).Render("▎")
				}
				agent = strong.Foreground(lipgloss.Color(m.pal.agents[x.Harness])).Render(agentMark(x.Harness))
				project = strong.Render(clip(displayInline(x.Project), pjW-1))
				when = strong.Render(fmt.Sprintf("%*s", ageW, age(x.TS)))
			}
			row := " " + cursor + " " + agent + " " + pad(m.paint(sn, true, selected), sw) + pad(project, pjW) + when + "  "
			if band {
				row = m.selectionRow(row, w)
			}
			b.WriteString(clipANSI(row, w) + "\n")
		}
		b.WriteString("\n") // breathing room before the title bar; the bar's tint does the separating
	}
	pageH := m.pageHeight()
	lines := m.pageLines(w)
	for i := 0; i < pageH; i++ {
		if i < len(lines) {
			b.WriteString(lines[i])
		}
		b.WriteByte('\n')
	}
	if m.prompt == promptHighlight {
		b.WriteString("  " + strong.Render("highlight ▸ ") + displayInline(m.status) + "▌  enter add · esc cancel")
	} else if m.prompt == promptRange {
		b.WriteString("  " + strong.Render("range ▸ ") + displayInline(m.status) + "▌  since..until, e.g. 7d or 2026-10-01..2026-10-07 · enter set · esc cancel")
	} else if m.help {
		b.WriteString("  esc or ? closes help · ctrl+c quits")
	} else {
		if m.status != "" {
			b.WriteString("  " + strong.Render(clip(displayInline(m.status), w-4)) + "\n")
		}
		if m.focus {
			b.WriteString(m.keys(w, "enter", "resume", "↓", "list", "tab", "agent", "ctrl+w", "word", "ctrl+u", "clear"))
		} else {
			b.WriteString(m.keys(w, "enter", "resume", "pgup/pgdown", "scroll", "t", "tools", "r", "sync", "?", "help"))
		}
	}
	rendered := strings.Split(b.String(), "\n")
	for i := range rendered {
		rendered[i] = fitLine(rendered[i], w)
	}
	return strings.Join(rendered, "\n")
}

// keys renders bold keys and muted labels, clipped from the tail.
func (m model) keys(w int, pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		cap := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(m.pal.key)).Render(" " + pairs[i] + " ")
		parts = append(parts, cap+" "+m.color(pairs[i+1], m.pal.muted))
	}
	return "  " + clipANSI(strings.Join(parts, "   "), w-4)
}
func clipANSI(s string, w int) string {
	if w <= 0 {
		return ""
	}
	plain := ansi.Strip(s)
	short := clip(plain, w)
	if plain == short {
		return s
	}
	return ansi.Truncate(s, displayWidth(strings.TrimSuffix(short, "…"))+1, "…") + "\x1b[0m"
}
func fitLine(s string, w int) string {
	s = clipANSI(s, w)
	return s + strings.Repeat(" ", max(0, w-displayWidth(s)))
}

var sgrCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (m model) selectionRow(s string, w int) string { return m.bgRow(s, w, m.pal.selbg) }
func (m model) bgRow(s string, w int, hex string) string {
	color := lipgloss.ColorProfile().Color(hex)
	if color == nil || color.Sequence(true) == "" {
		return pad(s, w)
	}
	bg := "\x1b[" + color.Sequence(true) + "m"
	// Inner foreground/bold styles reset backgrounds; restore the band after each SGR.
	return bg + sgrCode.ReplaceAllStringFunc(pad(s, w), func(code string) string { return code + bg }) + "\x1b[49m"
}
func (m *model) pageLines(w int) []string {
	if m.help {
		var agents []string
		for _, h := range harnesses {
			agents = append(agents, h.mark+" "+h.label)
		}
		return []string{
			"  focus    ▌ in query = search; ↓ moves to results",
			"  query    space = AND · quotes = phrase · -word = exclude",
			"  ↓ / esc search to results    / results to search",
			"  ↑ / k   previous result    ↓ / j next result",
			"  backspace delete character    ctrl+u clear query",
			"  ctrl+w  delete previous query word (Latin or CJK)",
			"  r       sync the index, then re-run the search",
			"  n / N   next / previous hit in transcript",
			"  pgup / pgdown scroll transcript by a page · wheel scrolls lines",
			"  h / H   add highlighter / clear highlights",
			"  d / D   set date range / clear date range",
			"  p        toggle current project scope",
			"  v       full transcript (hide hit list)    t show / fold tool output",
			"  o       open directory in editor",
			"  y       copy resume command",
			"  enter   exit and resume in original cwd; apply prompt",
			"  tab / shift+tab next / previous agent (all or one harness)",
			"  agents   " + strings.Join(agents, "   "),
			"  ?       show / close help from results",
			"  esc     cancel prompt / leave query / quit from results",
			"  ctrl+c  quit",
		}
	}
	if len(m.rows) == 0 {
		m.transcriptOffset = 0
		return nil
	}
	sel := m.rows[m.sel]
	cwd := sel.CWD
	if home, e := os.UserHomeDir(); e == nil && home != "" && strings.HasPrefix(cwd, home+"/") {
		cwd = "~" + cwd[len(home):]
	}
	left := "  " + m.agentGlyph(sel.Harness) + " " + lipgloss.NewStyle().Bold(true).Render(displayInline(sel.Project)) + "  " + m.color(sel.Harness+"  "+sel.TS[:min(10, len(sel.TS))]+"  "+cwd, m.pal.muted)
	active := toFTS(m.q) != ""
	count := fmt.Sprintf("result %d of %d", m.sel+1, len(m.rows))
	if active {
		hits := 0
		for _, hit := range m.hitIndex {
			if hit {
				hits++
			}
		}
		noun := "hits"
		if hits == 1 {
			noun = "hit"
		}
		count += fmt.Sprintf(" · %d %s here", hits, noun)
	}
	right := m.color(clip(count, max(0, w-6)), m.pal.muted)
	header := pad(clipANSI(left, max(0, w-displayWidth(right)-4)), w-displayWidth(right)-2) + right + "  "
	out := []string{m.bgRow(header, w, m.pal.surf), ""}
	keep := map[int]bool{}
	start, end := 0, len(m.messages)
	// ponytail: render a 200-message window for huge sessions; virtualize line offsets if direct arbitrary scrolling is needed.
	if end > 200 {
		start = max(0, m.cursor-100)
		end = min(end, start+200)
	}
	for i := start; i < end; i++ {
		x := m.messages[i]
		if m.full || !active || m.hitIndex[x.Index] {
			keep[i] = true
			if !m.full && active {
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
			return m.color("           ⋯", m.pal.muted)
		}
		return m.color(fmt.Sprintf("           ⋯ %d hidden · v shows all", n), m.pal.muted)
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
		star := " "
		if selected {
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
			if context {
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
			rule = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.pal.acc)).Render("┃")
		}
		prefix := "  " + star + "  " + stampText + " " + rule + " " + roleText + "  "
		const textCol = 19
		textW := max(1, w-textCol-2)
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
		for j, line := range body {
			p := prefix
			if j > 0 {
				p = strings.Repeat(" ", textCol)
			}
			line = m.paint(line, isHit, selected)
			if (context || folded) && !selected {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color(m.pal.muted)).Render(line)
			}
			line = p + line
			if selected {
				line = m.selectionRow(line, w)
			}
			lines = append(lines, clipANSI(line, w))
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
	focus := 0
	for k := lo; k < hi; k++ {
		if k == fi {
			focus = len(body)
			if items[k].lead != "" {
				focus++
			}
		}
		body = append(body, get(k)...)
	}
	if hi == len(items) && trail != "" {
		body = append(body, trail)
	}
	out = append(out, body...)
	if len(out) > available {
		// The title bar stays pinned; the body scrolls around the selected message.
		body := out[2:]
		base := max(0, min(len(body)-room, focus-room/2))
		start := max(0, min(len(body)-room, base+m.transcriptOffset))
		m.transcriptOffset = start - base
		out = append(out[:2:2], body[start:]...)
	} else {
		m.transcriptOffset = 0
	}
	return out
}
func gap(a, b string) bool {
	x, e := time.Parse(time.RFC3339Nano, a)
	y, f := time.Parse(time.RFC3339Nano, b)
	return e == nil && f == nil && y.Sub(x) >= time.Hour
}
func wrap(s string, w int) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if ln == "" {
			out = append(out, "")
			continue
		}
		first := len(out)
		for len(ln) > 0 {
			g := uniseg.NewGraphemes(ln)
			end, cells, wordStart := 0, 0, 0
			wasWord := false
			for g.Next() {
				start, stop := g.Positions()
				r, _ := utf8.DecodeRuneInString(g.Str())
				word := latinWord(r)
				if word && !wasWord {
					wordStart = start
				}
				v := displayWidth(g.Str())
				if cells+v > w {
					if word && wasWord && wordStart > 0 {
						end = wordStart
					}
					break
				}
				end, cells, wasWord = stop, cells+v, word
			}
			if end == 0 { // A single oversized grapheme cannot be split.
				g = uniseg.NewGraphemes(ln)
				g.Next()
				_, end = g.Positions()
			}
			part := ln[:end]
			if end < len(ln) {
				part = strings.TrimRightFunc(part, unicode.IsSpace)
			}
			if part != "" {
				out = append(out, part)
			}
			ln = strings.TrimLeftFunc(ln[end:], unicode.IsSpace)
		}
		if len(out) == first {
			out = append(out, "")
		}
	}
	return out
}

package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = m.windowTitle()
	v.ProgressBar = m.progressBar()
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if m.width < 25 || m.height < 10 {
		v.SetContent(fitLine("kioku · enlarge terminal", max(0, m.width)))
		return v
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
	right := m.color(clip(count, max(0, w-displayWidth(q)-4)), m.pal.muted)
	inputW := max(0, w-displayWidth(q)-displayWidth(right)-2)
	p := []rune(m.q)[:m.caret()]
	shownInput, caretX := queryWindow(displayInline(string(p)), displayInline(string([]rune(m.q)[m.caret():])), max(0, inputW-2))
	b.WriteString(m.bgRow(q+pad(strong.Render(shownInput), inputW)+right+"  ", w, m.pal.surf) + "\n")
	if len(m.pens) > 0 {
		ps := m.color("  highlights ", m.pal.muted)
		for _, p := range m.pens {
			ps += lipgloss.NewStyle().Background(lipgloss.Color(m.pal.pens[p.color])).Render(" "+displayInline(p.word)+" ") + " "
		}
		b.WriteString(clipANSI(ps, w-2) + "\n")
	}
	b.WriteByte('\n')
	listH := m.listHeight()
	if listH > 0 {
		browse := toFTS(m.q) == ""
		var empty []string
		if len(m.rows) == 0 {
			empty = m.emptyHints()
		}
		for i := 0; i < listH; i++ {
			idx := m.offset + i
			if idx >= len(m.rows) {
				if idx < len(empty) {
					b.WriteString(m.color("  "+empty[idx], m.pal.muted))
				}
				b.WriteString("\n")
				continue
			}
			x := m.rows[idx]
			selected := idx == m.sel
			const pjW, ageW = 13, 4
			sw := max(1, w-5-pjW-ageW-2)
			raw := x.Snippet
			if browse {
				if topic := m.topics[x.UID]; topic != "" {
					raw = topic
				}
			}
			sn := clip(tidySnippet(displayInline(raw)), max(1, sw-4))
			cursor := " "
			agent := m.agentGlyph(x.Harness)
			metaColor := m.pal.muted
			if m.focus && !selected {
				metaColor = m.pal.dim
			}
			project := m.color(clip(displayInline(x.Project), pjW-1), metaColor)
			when := m.color(fmt.Sprintf("%*s", ageW, age(x.TS)), metaColor)
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
			body := m.paint(sn, true, selected)
			if m.focus && !selected {
				body = m.dimUnstyled(body)
			}
			row := " " + cursor + " " + agent + " " + pad(body, sw) + pad(project, pjW) + when + "  "
			if band {
				row = m.selectionRow(row, w)
			}
			if m.pending() && m.slow {
				row = faint(row)
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
	var promptCursorX int
	if m.prompt == promptHighlight {
		prefix := "  highlight ▸ "
		val := displayInline(m.status)
		promptCursorX = displayWidth(prefix) + displayWidth(val)
		b.WriteString("  " + strong.Render("highlight ▸ ") + val + "  enter add · esc cancel")
	} else if m.prompt == promptRange {
		prefix := "  range ▸ "
		val := displayInline(m.status)
		promptCursorX = displayWidth(prefix) + displayWidth(val)
		b.WriteString("  " + strong.Render("range ▸ ") + val + "  since..until, e.g. 7d or 2026-10-01..2026-10-07 · enter set · esc cancel")
	} else if m.help {
		b.WriteString("  esc or ? closes help · / or ctrl+f search · ctrl+c quits")
	} else {
		if m.status != "" {
			b.WriteString("  " + strong.Render(clip(displayInline(m.status), w-4)) + "\n")
		}
		if m.focus {
			b.WriteString(m.keys(w, "enter", "resume", "↓", "list", "tab", "agent", "ctrl+w", "word", "ctrl+u", "clear"))
		} else {
			b.WriteString(m.keys(w, "enter", "resume", "/", "search", "O", "outline", "t", "tools", "r", "sync", "?", "help", "pgup/pgdown", "scroll"))
		}
	}
	rendered := strings.Split(b.String(), "\n")
	for i := range rendered {
		rendered[i] = fitLine(rendered[i], w)
	}
	if m.help {
		rendered = m.overlayHelp(rendered, w)
	}
	v.SetContent(strings.Join(rendered, "\n"))
	if m.prompt != promptNone {
		cur := tea.NewCursor(min(w-1, promptCursorX), len(rendered)-1)
		cur.Shape = tea.CursorBar
		cur.Blink = true
		v.Cursor = cur
	} else if m.focus {
		cur := tea.NewCursor(min(w-1, displayWidth(q)+caretX), 0)
		cur.Shape = tea.CursorBar
		cur.Blink = true
		v.Cursor = cur
	}
	return v
}

// keys renders bold keys and muted labels, dropping trailing pairs that do not fit.
func (m model) keys(w int, pairs ...string) string {
	maxW := w - 4
	parts := make([]string, 0, len(pairs)/2)
	used := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		cap := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(m.pal.key)).Render(" " + pairs[i] + " ")
		item := cap + " " + m.color(pairs[i+1], m.pal.muted)
		iw := displayWidth(item)
		sep := 0
		if len(parts) > 0 {
			sep = 2
		}
		if len(parts) > 0 && used+sep+iw > maxW {
			break
		}
		parts = append(parts, item)
		used += sep + iw
	}
	return "  " + clipANSI(strings.Join(parts, "  "), maxW)
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
	out := ansi.Truncate(s, displayWidth(strings.TrimSuffix(short, "…"))+1, "…") + "\x1b[0m"
	if strings.Count(out, "\x1b]8;")%2 == 1 {
		out += ansi.ResetHyperlink() // a cut link must not stay open over the rest of the screen
	}
	return out
}
func fitLine(s string, w int) string {
	s = clipANSI(s, w)
	return s + strings.Repeat(" ", max(0, w-displayWidth(s)))
}

var sgrCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (m model) selectionRow(s string, w int) string { return m.bgRow(s, w, m.pal.selbg) }
func (m model) dimUnstyled(s string) string {
	fg := ansi.Style{}.ForegroundColor(lipgloss.Color(m.pal.dim)).String()
	if fg == "" {
		return s
	}
	// Apply dim foreground to plain spans while leaving red hit highlights intact.
	return fg + sgrCode.ReplaceAllStringFunc(s, func(code string) string {
		if code == "\x1b[0m" || code == "\x1b[m" {
			return code + fg
		}
		return code
	}) + "\x1b[39m"
}
func (m model) bgRow(s string, w int, hex string) string {
	bg := ansi.Style{}.BackgroundColor(lipgloss.Color(hex)).String()
	if bg == "" {
		return pad(s, w)
	}
	// Inner foreground/bold styles reset backgrounds; restore the band after each SGR.
	return bg + sgrCode.ReplaceAllStringFunc(pad(s, w), func(code string) string { return code + bg }) + "\x1b[49m"
}
func hasHitRow(hitRows map[int]int, r int) bool {
	_, ok := hitRows[r]
	return ok
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

// pending reports that the rows on screen belong to an older query than the one typed.
// contextLines caps how many lines an unselected, non-hit message takes in the outline.
const contextLines = 3

func (m model) pending() bool { return m.resultRev != m.revision }

// faint dims a rendered row without losing the styles inside it.
func faint(s string) string {
	on := "\x1b[2m"
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+on)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+on)
	return on + s + "\x1b[22m"
}

// pathLink shows shown as a muted OSC 8 link to the directory on disk.
func (m model) pathLink(dir, shown string) string {
	return dirLink(lipgloss.NewStyle().Foreground(lipgloss.Color(m.pal.muted)), dir).Render(displayInline(shown))
}

// emptyHints names why nothing matched and lists only the fixes that apply to the current state.
func (m model) emptyHints() []string {
	var out []string
	switch {
	case m.rng.active():
		out = append(out, "No message in this date range. Press D to clear it.")
	case toFTS(m.q) != "":
		out = append(out, "No message contains all of these words.")
	}
	var positive, phrases int
	var negatives []string
	for _, t := range terms(m.q) {
		switch {
		case t.Negative:
			negatives = append(negatives, "-"+t.Word)
		case t.Phrase:
			phrases++
			positive++
		default:
			positive++
		}
	}
	if positive > 1 {
		out = append(out, "Drop a word to match more messages.")
	}
	if phrases > 0 {
		out = append(out, "A phrase must match exactly; remove the quotes to match the words anywhere.")
	}
	if len(negatives) > 0 {
		out = append(out, "Remove "+strings.Join(negatives, " ")+" to allow messages that mention it.")
	}
	if m.rng.active() && toFTS(m.q) != "" {
		out = append(out, "Press d to widen the date range.")
	}
	if m.root != "" {
		out = append(out, "Press p to remove the project scope.")
	}
	if m.harness != "" && m.harness != "all" {
		out = append(out, "Press tab to search all agents.")
	}
	return out
}

// readPercent says how far through the session's text the selected message ends. It counts
// characters, not messages, because messages differ wildly in length.
func (m model) readPercent() string {
	if m.cursor < 0 || m.cursor >= len(m.messages) {
		return ""
	}
	total, upto := 0, 0
	for i, x := range m.messages {
		n := utf8.RuneCountInString(x.Text)
		total += n
		if i <= m.cursor {
			upto += n
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", upto*100/total)
}

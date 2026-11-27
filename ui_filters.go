package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const filterPrefix = "  filters "

type filterKind int

const (
	filterAgent filterKind = iota
	filterRoot
	filterProject
	filterRange
	filterPen
)

type filterChip struct {
	kind         filterKind
	index, x     int
	text, fg, bg string
}

// Both the renderer and mouse handler use these cell spans, including any clipped label.
func (m model) filterChips() (visible []filterChip, hidden int) {
	var chips []filterChip
	add := func(kind filterKind, i int, glyph, label, fg, bg string) {
		chips = append(chips, filterChip{kind: kind, index: i, text: " " + glyph + " " + displayInline(label), fg: fg, bg: bg})
	}
	if m.harness != "" && m.harness != "all" {
		c, ok := m.pal.agents[m.harness]
		if !ok {
			c = agentColors{m.pal.muted, m.pal.key}
		}
		add(filterAgent, 0, agentMark(m.harness), m.harness, c.fg, c.chip)
	}
	if m.root != "" {
		add(filterRoot, 0, "⌂", filepath.Base(m.root), m.pal.acc, m.pal.key)
	}
	for i, project := range m.projects {
		add(filterProject, i, "⌂", project, m.pal.acc, m.pal.key)
	}
	if m.rng.active() {
		add(filterRange, 0, "◷", m.rng.label(), m.pal.acc, m.pal.key)
	}
	for i, p := range m.pens {
		add(filterPen, i, "✎", p.word, "", m.pal.pens[p.color])
	}
	const clear = " × "
	const minChip = 7 // glyph, one cell of label and the clear affordance
	x, end, slots := len(filterPrefix), m.width, len(chips)
	if slots*(minChip+1)-1 > end-x {
		end -= len(fmt.Sprintf(" +%d", len(chips)))
		slots = max(0, (end-x+1)/(minChip+1))
	}
	for i := range chips[:slots] {
		budget := end - x - (slots-i-1)*(minChip+1)
		c := &chips[i]
		c.x = x
		c.text = ansi.Truncate(c.text, budget-displayWidth(clear), "…") + clear
		x += displayWidth(c.text) + 1
	}
	return chips[:slots], len(chips) - slots
}

func (m model) filterLine() string {
	var b strings.Builder
	b.WriteString(m.color(filterPrefix, m.pal.muted))
	chips, hidden := m.filterChips()
	for i, c := range chips {
		if i > 0 {
			b.WriteByte(' ')
		}
		style := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(c.bg))
		if c.fg != "" {
			style = style.Foreground(lipgloss.Color(c.fg))
		}
		b.WriteString(style.Render(c.text))
	}
	if hidden > 0 {
		b.WriteString(m.color(fmt.Sprintf(" +%d", hidden), m.pal.muted))
	}
	return b.String()
}

func (m model) clearFilterAt(x int) (tea.Model, tea.Cmd) {
	chips, _ := m.filterChips()
	for _, c := range chips {
		if x < c.x || x >= c.x+displayWidth(c.text) {
			continue
		}
		switch c.kind {
		case filterAgent:
			m.harness = "all"
		case filterRoot:
			m.root = ""
		case filterProject:
			m.projects = slices.Delete(slices.Clone(m.projects), c.index, c.index+1)
		case filterRange:
			m.rng = timeRange{}
		case filterPen:
			m.pens = slices.Delete(slices.Clone(m.pens), c.index, c.index+1)
			return m, nil
		}
		m.sel, m.offset, m.status = 0, 0, ""
		m.revision++
		return m, m.requestQuery()
	}
	return m, nil
}

package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m *model) scrubRail(ry int) {
	m.pageLines(m.width)
	if m.rail.visible <= 1 {
		return
	}
	ry = max(0, min(m.rail.visible-1, ry))
	if len(m.messages) > 2 {
		m.cursor = ry * (len(m.messages) - 1) / (m.rail.visible - 1)
		m.transcriptOffset = 0
		return
	}
	if m.rail.total <= m.rail.visible {
		return
	}
	maxStart := m.rail.total - m.rail.visible
	targetStart := ry * maxStart / (m.rail.visible - 1)
	m.transcriptOffset = targetStart - m.rail.base
	m.pageLines(m.width)
}

func (m *model) applySplitY(y int) {
	avail := max(4, m.height-5)
	desired := y - m.headerRows() - 1
	clamped := max(2, min(max(2, avail-4), desired))
	m.listRatio = float64(clamped) / float64(avail)
	m.animActive = false
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if listH := m.listHeight(); listH > 0 && m.sel >= m.offset+listH {
		m.offset = m.sel - listH + 1
	}
}

func (m model) mouseUpdate(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse {
		return m, nil
	}
	l := m.layout()
	switch x := msg.(type) {
	case tea.MouseClickMsg:
		if x.Button != tea.MouseLeft {
			return m, nil
		}
		if m.help { // a click outside the help popup closes it; inside does nothing
			if !m.helpRect().contains(x.X, x.Y) {
				m.help = false
			}
			return m, nil
		}
		if l.header.contains(x.X, x.Y) {
			m.focus = true
			return m, nil
		}
		if l.pens.contains(x.X, x.Y) {
			col := len("  highlights ")
			for i, p := range m.pens {
				w := displayWidth(displayInline(p.word)) + 2
				if x.X >= col && x.X < col+w {
					m.pens = append(m.pens[:i], m.pens[i+1:]...)
					break
				}
				col += w + 1
			}
			return m, nil
		}
		if l.list.contains(x.X, x.Y) {
			i := m.offset + x.Y - l.list.y
			if i < len(m.rows) {
				m.sel = i
				m.focus = false
				return m, m.requestLoad()
			}
			m.focus = false
			return m, nil
		}
		if l.titleBar.contains(x.X, x.Y) {
			m.focus = false
			if !m.full {
				now := time.Now()
				if !m.lastTitleClick.IsZero() && now.Sub(m.lastTitleClick) < 350*time.Millisecond {
					m.listRatio = 0
					m.animActive = false
					m.lastTitleClick = time.Time{}
					return m, nil
				}
				m.lastTitleClick = now
				m.drag = dragSplit
			}
			return m, nil
		}
		if l.rail.contains(x.X, x.Y) {
			m.focus = false
			m.pageLines(m.width)
			ry := x.Y - l.rail.y
			if idx, ok := m.rail.hitRows[ry]; ok {
				m.cursor = idx
				m.transcriptOffset = 0
				return m, m.startPulse()
			}
			m.drag = dragRail
			m.scrubRail(ry)
			return m, nil
		}
		m.focus = false
	case tea.MouseMotionMsg:
		switch m.drag {
		case dragSplit:
			if !m.full {
				m.applySplitY(x.Y)
			}
		case dragRail:
			if l.rail.h > 0 {
				m.scrubRail(x.Y - l.rail.y)
			}
		}
	case tea.MouseReleaseMsg:
		m.drag = dragNone
	case tea.MouseWheelMsg:
		if x.Button != tea.MouseWheelDown && x.Button != tea.MouseWheelUp {
			return m, nil
		}
		step := 3
		if x.Button == tea.MouseWheelUp {
			step = -3
		}
		if x.Y < m.headerRows()+m.listHeight() && m.listHeight() > 0 {
			m.offset = max(0, min(max(0, len(m.rows)-m.listHeight()), m.offset+step))
		} else {
			m.transcriptOffset += step
			m.pageLines(m.width)
		}
	}
	return m, nil
}

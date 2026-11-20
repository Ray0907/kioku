package main

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	maxReadingWidth = 84
	minRailWidth    = 40
	animFrameDelay  = 16 * time.Millisecond
	pulseFrameCount = 8 // ~128ms gentle visual confirmation on hit jump
)

type rect struct {
	x, y, w, h int
}

func (r rect) contains(px, py int) bool {
	return r.w > 0 && r.h > 0 && px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

type uiLayout struct {
	header     rect
	pens       rect
	list       rect
	titleBar   rect
	transcript rect
	rail       rect
}

type dragKind int

const (
	dragNone dragKind = iota
	dragSplit
	dragRail
)

type animTickMsg struct {
	id int
}

type topicsMsg struct {
	topics map[string]string
}

type railState struct {
	total     int
	start     int
	visible   int
	base      int
	hitRows   map[int]int // rail row (0..visible-1) -> message index in m.messages
	cursorRow int         // -1 if none
}

// layout computes the screen rects for every interactive zone in one place so
// View and mouse hit-testing can never drift out of sync.
func (m model) layout() uiLayout {
	var l uiLayout
	if m.width < 25 || m.height < 10 {
		return l
	}
	l.header = rect{x: 0, y: 0, w: m.width, h: 1}
	if len(m.pens) > 0 {
		l.pens = rect{x: 0, y: 1, w: m.width, h: 1}
	}
	y := m.headerRows()
	listH := m.listHeight()
	if listH > 0 {
		l.list = rect{x: 0, y: y, w: m.width, h: listH}
		y += listH + 1 // +1 for the blank breathing row before the title bar
	}
	pageH := m.pageHeight()
	if len(m.rows) > 0 && pageH > 0 {
		l.titleBar = rect{x: 0, y: y, w: m.width, h: 1}
		bodyH := max(0, pageH-2)
		if bodyH > 0 {
			if m.hasRail() {
				l.transcript = rect{x: 0, y: y + 2, w: m.width - 2, h: bodyH}
				// Give the 1-cell rail a 2-column hit target on the right edge for easy grabbing.
				l.rail = rect{x: m.width - 2, y: y + 2, w: 2, h: bodyH}
			} else {
				l.transcript = rect{x: 0, y: y + 2, w: m.width, h: bodyH}
			}
		}
	}
	return l
}

func (m model) hasRail() bool {
	return m.width >= minRailWidth && len(m.rows) > 0
}

// baseListHeight returns the settled (non-animated) hit-list height.
func (m model) baseListHeight() int {
	if m.full {
		return 0
	}
	avail := max(4, m.height-5)
	if m.listRatio > 0 {
		h := int(float64(avail)*m.listRatio + 0.5)
		return max(2, min(max(2, avail-4), h))
	}
	if avail >= 24 { // a tall window has room to give the result list half of it
		return max(2, avail/2)
	}
	return max(2, avail*38/100)
}

func (m *model) startListAnim(from, to int) tea.Cmd {
	if from == to {
		m.animActive = false
		return nil
	}
	m.animActive = true
	m.animListH = from
	m.animTargetH = to
	m.stepListAnim()
	if !m.animActive && m.pulseTicks == 0 {
		return nil
	}
	return m.armAnimTick()
}

func (m *model) stepListAnim() {
	if !m.animActive {
		return
	}
	diff := m.animTargetH - m.animListH
	if diff == 0 {
		m.animActive = false
		return
	}
	step := diff * 35 / 100
	if step == 0 {
		if diff > 0 {
			step = 1
		} else {
			step = -1
		}
	}
	m.animListH += step
	if (diff > 0 && m.animListH >= m.animTargetH) || (diff < 0 && m.animListH <= m.animTargetH) {
		m.animListH = m.animTargetH
		m.animActive = false
	}
}

func (m *model) startPulse() tea.Cmd {
	m.pulseTicks = pulseFrameCount
	return m.armAnimTick()
}

func (m *model) armAnimTick() tea.Cmd {
	m.animTickID++
	id := m.animTickID
	return tea.Tick(animFrameDelay, func(time.Time) tea.Msg {
		return animTickMsg{id: id}
	})
}

// messagesTopic extracts the first non-injected user message as a concise session topic.
func messagesTopic(msgs []message) string {
	for _, x := range msgs {
		if x.Role != "user" || injectedUserText(x.Text) {
			continue
		}
		text := tidySnippet(displayInline(x.Text))
		if text != "" {
			return clip(text, 70)
		}
	}
	return ""
}

// outlineKeep marks the skeleton of a session: every user message plus the final
// assistant reply of each turn, matching the CLI --outline semantics.
func outlineKeep(msgs []message) []bool {
	keep := make([]bool, len(msgs))
	lastAsst := -1
	flushAsst := func() {
		if lastAsst >= 0 {
			keep[lastAsst] = true
			// Include adjacent same-timestamp assistant blocks as one final reply.
			ts := msgs[lastAsst].TS
			for j := lastAsst - 1; j >= 0 && isAssistantRole(msgs[j].Role) && msgs[j].TS == ts; j-- {
				keep[j] = true
			}
			lastAsst = -1
		}
	}
	for i, x := range msgs {
		switch {
		case x.Role == "user":
			flushAsst()
			keep[i] = true
		case isAssistantRole(x.Role):
			lastAsst = i
		}
	}
	flushAsst()
	return keep
}

func isAssistantRole(role string) bool {
	return role == "assistant" || role == "asst"
}

func (m *model) requestTopics() tea.Cmd {
	if m.db == nil || len(m.rows) == 0 {
		return nil
	}
	if m.topics == nil {
		m.topics = map[string]string{}
	}
	seen := map[string]bool{}
	var missing []string
	limit := min(len(m.rows), 80)
	for _, r := range m.rows[:limit] {
		if r.UID == "" || seen[r.UID] {
			continue
		}
		if _, ok := m.topics[r.UID]; ok {
			continue
		}
		seen[r.UID] = true
		missing = append(missing, r.UID)
	}
	if len(missing) == 0 {
		return nil
	}
	db := m.db
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out := make(map[string]string, len(missing))
		for _, uid := range missing {
			topic, err := sessionTopic(ctx, db, uid, true)
			if err != nil {
				continue
			}
			if topic != "" && topic != "(no user message)" {
				out[uid] = tidySnippet(topic)
			} else {
				out[uid] = ""
			}
		}
		return topicsMsg{topics: out}
	}
}

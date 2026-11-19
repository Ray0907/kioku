package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const historyCap = 100

// caret is qpos clamped to the query, so any code that replaces m.q stays safe.
func (m model) caret() int {
	return min(max(m.qpos, 0), utf8.RuneCountInString(m.q))
}

func (m *model) setQuery(r []rune, pos int) {
	m.q, m.qpos, m.hist = string(r), pos, 0
}

// insertAtCaret adds typed or pasted text at the caret and leaves the caret after it.
func (m *model) insertAtCaret(text string) {
	r, p := []rune(m.q), m.caret()
	ins := []rune(text)
	m.setQuery(slices.Insert(r, p, ins...), p+len(ins))
}

// wordStart is where ctrl+w and alt+left stop: skip spaces, then one run of CJK or non-CJK.
func wordStart(r []rune, p int) int {
	for p > 0 && unicode.IsSpace(r[p-1]) {
		p--
	}
	if p == 0 {
		return 0
	}
	cjk := isCJK(r[p-1])
	for p > 0 && !unicode.IsSpace(r[p-1]) && isCJK(r[p-1]) == cjk {
		p--
	}
	return p
}

func wordEnd(r []rune, p int) int {
	for p < len(r) && unicode.IsSpace(r[p]) {
		p++
	}
	if p == len(r) {
		return p
	}
	cjk := isCJK(r[p])
	for p < len(r) && !unicode.IsSpace(r[p]) && isCJK(r[p]) == cjk {
		p++
	}
	return p
}

// queryKey applies a caret or edit key. edited means the text changed and results must refresh;
// the delete keys count as edits even when there was nothing to delete.
func (m *model) queryKey(key string) (handled, edited bool) {
	r, p := []rune(m.q), m.caret()
	switch key {
	case "left", "ctrl+b":
		m.qpos = max(0, p-1)
	case "right", "ctrl+f":
		m.qpos = min(len(r), p+1)
	case "home", "ctrl+a":
		m.qpos = 0
	case "end", "ctrl+e":
		m.qpos = len(r)
	case "alt+left":
		m.qpos = wordStart(r, p)
	case "alt+right":
		m.qpos = wordEnd(r, p)
	case "backspace":
		if p > 0 {
			m.setQuery(slices.Delete(r, p-1, p), p-1)
		}
		return true, true
	case "delete":
		if p < len(r) {
			m.setQuery(slices.Delete(r, p, p+1), p)
		}
		return true, true
	case "ctrl+u":
		m.setQuery(r[p:], 0)
		return true, true
	case "ctrl+w":
		s := wordStart(r, p)
		m.setQuery(slices.Delete(r, s, p), s)
		return true, true
	default:
		return false, false
	}
	return true, false
}

// historyKey walks saved queries: up from an empty query, then up/down while walking.
// hist counts steps back from the newest entry; 0 means not walking.
func (m *model) historyKey(key string) bool {
	switch {
	case key == "up" && (m.hist > 0 || m.q == "") && m.hist < len(m.history):
		m.hist++
	case key == "down" && m.hist > 0:
		m.hist--
	default:
		return false
	}
	m.q = ""
	if m.hist > 0 {
		m.q = m.history[len(m.history)-m.hist]
	}
	m.qpos = utf8.RuneCountInString(m.q)
	return true
}

func (m *model) remember() { m.history = addHistory(m.history, m.q) }

func addHistory(h []string, q string) []string {
	q = strings.TrimSpace(displayInline(q))
	if q == "" || len(h) > 0 && h[len(h)-1] == q {
		return h
	}
	h = append(h, q)
	if len(h) > historyCap {
		h = h[len(h)-historyCap:]
	}
	return h
}

// historyPath sits next to the index, so KIOKU_INDEX moves both.
func historyPath() string { return filepath.Join(filepath.Dir(indexPath()), "history") }

func loadHistory() []string {
	data, err := os.ReadFile(historyPath())
	if err != nil {
		return nil
	}
	var h []string
	for _, line := range strings.Split(string(data), "\n") {
		h = addHistory(h, line)
	}
	return h
}

// saveHistory is best effort: a read-only cache dir must not break leaving the TUI.
func saveHistory(h []string) {
	path := historyPath()
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	os.WriteFile(path, []byte(strings.Join(h, "\n")+"\n"), 0600)
}

// queryWindow fits the query into cells columns with the caret inside the window.
// It returns the text to draw and the caret's column within it.
func queryWindow(prefix, rest string, cells int) (string, int) {
	r := []rune(prefix + rest)
	p := utf8.RuneCountInString(prefix)
	width := func(rs []rune) int { return displayWidth(string(rs)) }
	if width(r) <= cells {
		return string(r), width(r[:p])
	}
	if cells < 3 {
		return "", 0
	}
	s, left := p, 0
	for s > 0 {
		c := width(r[s-1 : s])
		if left+c > cells-2 {
			break
		}
		left += c
		s--
	}
	head, budget, lead := "", cells-left, 0
	if s > 0 {
		head, budget, lead = "…", budget-1, 1
	}
	e, right := p, 0
	for e < len(r) {
		c := width(r[e : e+1])
		if right+c > budget {
			break
		}
		right += c
		e++
	}
	tail := ""
	if e < len(r) {
		for e > p && right+1 > budget {
			e--
			right -= width(r[e : e+1])
		}
		tail = "…"
	}
	return head + string(r[s:e]) + tail, left + lead
}

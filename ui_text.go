package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

func displayWidth(s string) int { return ansi.StringWidth(s) }

func displayText(s string, multiline bool) string {
	s = ansi.Strip(strings.ReplaceAll(s, "\r\n", "\n"))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r':
			if multiline {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		case r == '\t':
			if multiline {
				b.WriteString("    ")
			} else {
				b.WriteByte(' ')
			}
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d':
			// Escape and bidi controls must not steer the terminal or reorder text.
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
func displayInline(s string) string     { return displayText(s, false) }
func displayTranscript(s string) string { return displayText(s, true) }

var pipeRun = regexp.MustCompile(`\s*\|[\s|:\-]*`)

// tidySnippet drops the markdown noise a list row would otherwise lead with: bullets, headings,
// bold markers and table pipes. Display only; the index and search never see it.
func tidySnippet(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.Trim(pipeRun.ReplaceAllString(s, " · "), " ·")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimLeft(s, "-*#> ")
}

func pad(s string, n int) string {
	w := displayWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if displayWidth(s) <= n {
		return s
	}
	g := uniseg.NewGraphemes(s)
	end, cells := 0, 0
	for g.Next() {
		if cells+displayWidth(g.Str()) > n-1 {
			break
		}
		_, end = g.Positions()
		cells += displayWidth(g.Str())
	}
	prefix := s[:end]
	if end < len(s) {
		next, _ := utf8.DecodeRuneInString(s[end:])
		lastPart := prefix
		last, size := utf8.DecodeLastRuneInString(lastPart)
		for unicode.Is(unicode.Mn, last) && size > 0 {
			lastPart = lastPart[:len(lastPart)-size]
			last, size = utf8.DecodeLastRuneInString(lastPart)
		}
		if latinWord(last) && latinWord(next) {
			for len(prefix) > 0 {
				r, size := utf8.DecodeLastRuneInString(prefix)
				if !latinWord(r) && !unicode.Is(unicode.Mn, r) {
					break
				}
				prefix = prefix[:len(prefix)-size]
			}
		}
	}
	return strings.TrimRightFunc(prefix, unicode.IsSpace) + "…"
}
func latinWord(r rune) bool { return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) || r == '_' }
func (m model) paint(s string, query, selected bool) string {
	ts := terms(m.q)
	r := []rune(s)
	h := make([]bool, len(r))
	marks := make([]int, len(r))
	for i := range marks {
		marks[i] = -1
	}
	for _, p := range m.pens {
		for _, at := range positions(r, p.word) {
			for j := at; j < len(r) && j < at+len([]rune(p.word)); j++ {
				marks[j] = p.color
			}
		}
	}
	if query {
		for _, t := range ts {
			if t.Negative {
				continue
			}
			for _, at := range positions(r, t.Word) {
				for j := at; j < len(r) && j < at+len([]rune(t.Word)); j++ {
					h[j] = true
				}
			}
		}
	}
	var b strings.Builder
	for i := 0; i < len(r); {
		j := i + 1
		for j < len(r) && h[j] == h[i] && marks[j] == marks[i] {
			j++
		}
		s := string(r[i:j])
		if h[i] || marks[i] >= 0 || selected {
			style := lipgloss.NewStyle()
			if selected {
				style = style.Bold(true)
			}
			if h[i] {
				style = style.Foreground(lipgloss.Color(m.pal.hit)).Underline(true).Bold(true)
			}
			if marks[i] >= 0 {
				style = style.Background(lipgloss.Color(m.pal.pens[marks[i]]))
			}
			s = style.Render(s)
		}
		b.WriteString(s)
		i = j
	}
	return b.String()
}
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func positions(r []rune, word string) []int {
	w := []rune(strings.ToLower(word))
	if len(w) == 0 {
		return nil
	}
	var out []int
	for i := 0; i+len(w) <= len(r); i++ {
		if fold(string(r[i:i+len(w)])) == fold(word) {
			if isCJK(w[0]) || i == 0 || (!unicode.IsLetter(r[i-1]) && !unicode.IsDigit(r[i-1])) {
				out = append(out, i)
			}
		}
	}
	return out
}

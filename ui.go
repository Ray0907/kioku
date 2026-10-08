package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

type palette struct {
	hit, muted, rule, selbg, surf string
	pens                          [3]string
}

var light = palette{"#b6322d", "#606a74", "#798898", "#d2e3f9", "#eef3f9", [3]string{"#fde89a", "#bdfac8", "#ffe0f2"}}
var dark = palette{"#f6857a", "#95a0ab", "#6f7e8d", "#293647", "#232a35", [3]string{"#3c3207", "#193b22", "#492537"}}

type pen struct {
	word  string
	color int
}
type model struct {
	db                               *sql.DB
	q                                string
	harness                          string
	projects                         []string
	rows                             []hit
	messages                         []message
	hitIndex                         map[int]bool
	sel, cursor, width, height       int
	limit                            int
	allTime, includeSelf             bool
	focus, full, prompt, help, mouse bool
	pens                             []pen
	status                           string
	revision                         int
	progress                         string
	resume                           *hit
	pal                              palette
	offset                           int
	cancel, loadCancel               context.CancelFunc
	loadRevision                     int
	initial                          tea.Cmd
}
type resultMsg struct {
	revision int
	rows     []hit
	err      error
}
type loadMsg struct {
	revision int
	messages []message
	hits     map[int]bool
	err      error
}
type syncMsg struct {
	done     bool
	progress string
	err      error
}
type debounce int

func newModel(db *sql.DB, q, h string, projects []string, rows []hit, mouse bool, limit int, includeSelf bool) model {
	p := light
	if os.Getenv("KIOKU_THEME") == "dark" || os.Getenv("KIOKU_THEME") == "" && termenv.HasDarkBackground() {
		p = dark
	}
	m := model{db: db, q: q, harness: h, projects: projects, rows: rows, mouse: mouse, limit: limit, includeSelf: includeSelf, pal: p, width: 80, height: 24, cursor: -1, focus: true}
	m.initial = m.requestLoad()
	return m
}
func (m model) Init() tea.Cmd { return tea.Batch(startSync(), m.initial) }
func startSync() tea.Cmd {
	ch := make(chan syncMsg, 32)
	go func() {
		db, err := openDB()
		if err != nil {
			ch <- syncMsg{done: true, err: err}
			close(ch)
			return
		}
		defer db.Close()
		_, err = syncIndex(db, false, func(i, n int) {
			if i == 1 || i == n || i%25 == 0 {
				select {
				case ch <- syncMsg{progress: fmt.Sprintf("indexing %d/%d", i, n)}:
				default:
				}
			}
		})
		ch <- syncMsg{done: true, err: err}
		close(ch)
	}()
	return waitSync(ch)
}
func waitSync(ch <-chan syncMsg) tea.Cmd {
	return func() tea.Msg {
		x, ok := <-ch
		if !ok {
			return nil
		}
		return syncEnvelope{x, ch}
	}
}

type syncEnvelope struct {
	x  syncMsg
	ch <-chan syncMsg
}

func queryCmd(ctx context.Context, db *sql.DB, q, h string, projects []string, limit, rev int, allTime, includeSelf bool) tea.Cmd {
	return func() tea.Msg {
		rows, e := search(ctx, db, q, h, projects, limit, allTime, includeSelf)
		return resultMsg{rev, rows, e}
	}
}
func (m *model) requestQuery() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	m.messages = nil
	m.hitIndex = map[int]bool{}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return queryCmd(ctx, m.db, m.q, m.harness, m.projects, m.limit, m.revision, m.allTime, m.includeSelf)
}
func (m *model) requestLoad() tea.Cmd {
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	m.messages = nil
	m.hitIndex = map[int]bool{}
	m.cursor = -1
	if m.sel >= len(m.rows) {
		m.sel = max(0, len(m.rows)-1)
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+m.listHeight() {
		m.offset = m.sel - m.listHeight() + 1
	}
	if len(m.rows) == 0 {
		return nil
	}
	selected := m.rows[m.sel]
	m.cursor = selected.Index
	ctx, cancel := context.WithCancel(context.Background())
	m.loadCancel = cancel
	rev := m.loadRevision
	q := m.q
	includeSelf := m.includeSelf
	return func() tea.Msg {
		msgs, e := transcript(ctx, m.db, selected.UID)
		hits := map[int]bool{}
		if e == nil && toFTS(q) != "" {
			rows, err := m.db.QueryContext(ctx, `SELECT m.idx FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid WHERE messages_fts MATCH ? AND m.session_uid=? `+selfFilter(includeSelf), toFTS(q), selected.UID)
			if err == nil {
				for rows.Next() {
					var i int
					if rows.Scan(&i) == nil {
						hits[i] = true
					}
				}
				e = rows.Err()
				rows.Close()
			} else {
				e = err
			}
		}
		return loadMsg{rev, msgs, hits, e}
	}
}
func (m *model) refresh() tea.Cmd {
	m.revision++
	if m.cancel != nil {
		m.cancel()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	m.messages = nil
	m.hitIndex = map[int]bool{}
	return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg { return debounce(m.revision) })
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = x.Width
		m.height = x.Height
	case syncEnvelope:
		m.progress = x.x.progress
		if x.x.done {
			m.progress = ""
			if x.x.err != nil {
				m.status = x.x.err.Error()
			}
			return m, m.requestQuery()
		}
		return m, waitSync(x.ch)
	case debounce:
		if int(x) != m.revision {
			return m, nil
		}
		return m, m.requestQuery()
	case resultMsg:
		if x.revision == m.revision {
			if x.err != nil {
				if x.err != context.Canceled {
					m.status = x.err.Error()
				}
			} else {
				m.rows = x.rows
				m.sel = 0
				return m, m.requestLoad()
			}
		}
	case loadMsg:
		if x.revision == m.loadRevision {
			if x.err != nil {
				if x.err != context.Canceled {
					m.status = x.err.Error()
				}
			} else {
				m.messages = x.messages
				m.hitIndex = x.hits
			}
		}
	case tea.MouseMsg:
		return m.mouseUpdate(x)
	case tea.KeyMsg:
		key := x.String()
		if key == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			if m.loadCancel != nil {
				m.loadCancel()
			}
			return m, tea.Quit
		}
		if m.prompt {
			switch key {
			case "esc":
				m.prompt = false
				m.status = ""
			case "enter":
				w := strings.TrimSpace(m.status)
				if w != "" {
					exists := false
					for _, p := range m.pens {
						if p.word == w {
							exists = true
						}
					}
					if !exists {
						m.pens = append(m.pens, pen{w, len(m.pens) % 3})
					}
				}
				m.status = ""
				m.prompt = false
			case "backspace":
				m.status = trimLast(m.status)
			default:
				if x.Type == tea.KeyRunes {
					m.status += string(x.Runes)
				}
			}
			return m, nil
		}
		if key == "tab" || key == "shift+tab" {
			hs := []string{"all", "claude", "codex", "pi", "grok", "opencode", "cursor"}
			for i, h := range hs {
				if h == m.harness {
					step := 1
					if key == "shift+tab" {
						step = len(hs) - 1
					}
					m.harness = hs[(i+step)%len(hs)]
					break
				}
			}
			m.sel = 0
			m.revision++
			return m, m.requestQuery()
		}
		if key == "enter" {
			if len(m.rows) > 0 {
				r := m.rows[m.sel]
				if _, e := os.Stat(r.CWD); e != nil {
					m.status = "directory no longer exists: " + r.CWD
				} else {
					m.resume = &r
					return m, tea.Quit
				}
			}
			return m, nil
		}
		if m.focus {
			switch key {
			case "down", "esc":
				m.focus = false
			case "backspace":
				m.q = trimLast(m.q)
				m.status = ""
				return m, m.refresh()
			case "ctrl+u":
				m.q = ""
				return m, m.refresh()
			default:
				if x.Type == tea.KeyRunes {
					m.q += string(x.Runes)
					m.status = ""
					return m, m.refresh()
				}
			}
			return m, nil
		}
		if m.help && key != "?" {
			m.help = false
			return m, nil
		}
		switch key {
		case "esc":
			if m.help {
				m.help = false
				return m, nil
			}
			return m, tea.Quit
		case "up", "k":
			if m.sel == 0 {
				m.focus = true
			} else {
				m.sel--
				return m, m.requestLoad()
			}
		case "down", "j":
			if m.sel+1 < len(m.rows) {
				m.sel++
				return m, m.requestLoad()
			}
		case "/":
			m.focus = true
		case "n", "N":
			m.jump(key == "n")
		case "h":
			m.prompt = true
			m.status = ""
		case "H":
			m.pens = nil
		case "v":
			m.full = !m.full
		case "?":
			m.help = !m.help
		case "o":
			if len(m.rows) > 0 {
				m.status = openProject(m.rows[m.sel].CWD)
			}
		case "y":
			if len(m.rows) > 0 {
				m.status = copyCommand(m.rows[m.sel].ResumeCmd)
			}
		}
	}
	return m, nil
}
func trimLast(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		return string(r[:len(r)-1])
	}
	return s
}
func (m *model) jump(next bool) {
	if len(m.rows) == 0 {
		return
	}
	hits := []int{}
	for i, x := range m.messages {
		if m.hitIndex[x.Index] {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return
	}
	at := -1
	for i, v := range hits {
		if v == m.cursor {
			at = i
		}
	}
	if next {
		m.cursor = hits[(at+1)%len(hits)]
	} else {
		m.cursor = hits[(at-1+len(hits))%len(hits)]
	}
}
func matches(text string, ts []term) bool {
	positive := false
	for _, t := range ts {
		yes := strings.Contains(strings.ToLower(text), strings.ToLower(t.Word))
		if t.Negative {
			if yes {
				return false
			}
		} else {
			positive = true
			if !yes {
				return false
			}
		}
	}
	return positive
}
func (m model) mouseUpdate(x tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouse {
		return m, nil
	}
	if x.Action == tea.MouseActionPress && x.Button == tea.MouseButtonLeft {
		if x.Y == 0 {
			m.focus = true
			return m, nil
		}
		if x.Y == 1 && len(m.pens) > 0 {
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
		top := m.headerRows()
		listH := m.listHeight()
		if !m.full && x.Y >= top && x.Y < top+listH {
			i := m.offset + x.Y - top
			if i < len(m.rows) {
				m.sel = i
				m.focus = false
				return m, m.requestLoad()
			}
			m.focus = false
			return m, nil
		}
		m.focus = false
	}
	if x.Button == tea.MouseButtonWheelDown || x.Button == tea.MouseButtonWheelUp {
		step := 3
		if x.Button == tea.MouseButtonWheelUp {
			step = -3
		}
		if x.Y < m.headerRows()+m.listHeight() && !m.full {
			m.offset = max(0, min(max(0, len(m.rows)-m.listHeight()), m.offset+step))
		} else {
			m.cursor = max(0, min(len(m.messages)-1, m.cursor+step))
		}
	}
	return m, nil
}

// headerRows counts query, optional highlights, and a blank row.
func (m model) headerRows() int {
	if len(m.pens) > 0 {
		return 3
	}
	return 2
}

// chromeRows counts the header, list gap (unless full), and footer.
func (m model) chromeRows() int {
	if m.full {
		return m.headerRows() + 1
	}
	return m.headerRows() + 2
}
func (m model) listHeight() int {
	if m.full {
		return 0
	}
	return max(2, (m.height-5)*38/100)
}
func (m model) color(s, c string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(displayInline(s))
}
func agentMark(h string) string {
	switch h {
	case "claude":
		return "✻"
	case "codex":
		return "◇"
	case "pi":
		return "π"
	case "grok":
		return "✕"
	case "opencode":
		return "▢"
	case "cursor":
		return "↖"
	default:
		return "?"
	}
}

// ANSI-aware grapheme width; ambiguous-width characters occupy one cell.
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
	}
	strong := lipgloss.NewStyle().Bold(true)
	q := "  " + strong.Foreground(lipgloss.Color(m.pal.muted)).Render("kioku") + " ▸ "
	input := displayInline(m.q)
	if m.focus {
		input += "▌"
	}
	right := m.color(clip(count, max(0, w-displayWidth(q)-4)), m.pal.muted)
	inputW := max(0, w-displayWidth(q)-displayWidth(right)-2)
	b.WriteString(q + pad(strong.Render(clip(input, max(0, inputW-2))), inputW) + right + "  \n")
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
				if idx == 0 {
					b.WriteString(m.color("  No message contains all of these words. Drop a word or remove the quotes.", m.pal.muted))
				}
				b.WriteString("\n")
				continue
			}
			x := m.rows[idx]
			selected := idx == m.sel
			const pjW, ageW = 13, 4
			sw := max(1, w-5-pjW-ageW-2)
			sn := clip(displayInline(x.Snippet), max(1, sw-2))
			cursor := " "
			agent := m.color(agentMark(x.Harness), m.pal.muted)
			project := clip(displayInline(x.Project), pjW-1)
			when := m.color(fmt.Sprintf("%*s", ageW, age(x.TS)), m.pal.muted)
			if selected {
				cursor = strong.Render("›")
				agent = strong.Render(agentMark(x.Harness))
				project = strong.Render(project)
				when = strong.Render(fmt.Sprintf("%*s", ageW, age(x.TS)))
			}
			row := "  " + cursor + agent + " " + pad(m.paint(sn, true, selected), sw) + pad(project, pjW) + when + "  "
			if selected {
				row = m.selectionRow(row, w)
			}
			b.WriteString(clipANSI(row, w) + "\n")
		}
		b.WriteString("\n") // breathing room before the title bar; the bar's tint does the separating
	}
	extra := 0
	if m.status != "" && !m.prompt {
		extra = 1
	}
	pageH := max(1, m.height-m.chromeRows()-listH-extra)
	lines := m.pageLines(w)
	for i := 0; i < pageH; i++ {
		if i < len(lines) {
			b.WriteString(lines[i])
		}
		b.WriteByte('\n')
	}
	if m.prompt {
		b.WriteString("  " + strong.Render("highlight ▸ ") + displayInline(m.status) + "▌  enter add · esc cancel")
	} else if m.help {
		b.WriteString("  focus · query · ↑↓ · n/N · h/H · v · o · y · enter · tab · ctrl+c")
	} else {
		if m.status != "" {
			b.WriteString("  " + strong.Render(clip(displayInline(m.status), w-4)) + "\n")
		}
		if m.focus {
			b.WriteString(m.keys(w, "enter", "resume", "↓", "results", "tab", "agent", "ctrl+u", "clear"))
		} else {
			b.WriteString(m.keys(w, "enter", "resume", "↑↓", "move", "n/N", "next hit", "/", "search", "tab", "✻ ◇ π", "?", "help"))
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
		parts = append(parts, lipgloss.NewStyle().Bold(true).Render(pairs[i])+" "+m.color(pairs[i+1], m.pal.muted))
	}
	return "  " + clipANSI(strings.Join(parts, "    "), w-4)
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
func (m model) pageLines(w int) []string {
	if m.help {
		return []string{"  focus    ▌ in query = search; ↓ moves to results", "  query    space = AND · quotes = phrase · -word = exclude", "  ↓ / esc search to results    / results to search", "  ↑ ↓     previous / next result", "  n / N   next / previous hit in transcript", "  h / H   add highlighter / clear highlights", "  v       full transcript (hide hit list)", "  o       open directory in editor", "  y       copy resume command", "  enter   exit and resume in original cwd", "  tab     all · claude · codex · pi · grok · opencode · cursor", "  agents   ✻ claude   ◇ codex   π pi   ✕ grok   ▢ opencode   ↖ cursor", "  ctrl+c  quit"}
	}
	if len(m.rows) == 0 {
		return nil
	}
	sel := m.rows[m.sel]
	cwd := sel.CWD
	if home, e := os.UserHomeDir(); e == nil && home != "" && strings.HasPrefix(cwd, home+"/") {
		cwd = "~" + cwd[len(home):]
	}
	left := "  " + m.color(agentMark(sel.Harness), m.pal.muted) + " " + lipgloss.NewStyle().Bold(true).Render(displayInline(sel.Project)) + "  " + m.color(sel.Harness+"  "+sel.TS[:min(10, len(sel.TS))]+"  "+cwd, m.pal.muted)
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
		prefix := "  " + star + "  " + stampText + "  " + roleText + "  "
		const textCol = 18
		textW := max(1, w-textCol-2)
		for j, line := range wrap(displayTranscript(x.Text), textW) {
			p := prefix
			if j > 0 {
				p = strings.Repeat(" ", textCol)
			}
			line = m.paint(line, isHit, selected)
			if context && !selected {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color(m.pal.muted)).Render(line)
			}
			line = p + line
			if selected {
				line = m.selectionRow(line, w)
			}
			lines = append(lines, clipANSI(line, w))
		}
		return lines
	}
	extra := 0
	if m.status != "" && !m.prompt {
		extra = 1
	}
	available := max(1, m.height-m.chromeRows()-m.listHeight()-extra)
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
		// Half a pane above the selection; a whole pane when the end is in view, since the page then scrolls to the bottom.
		return lo > 0 && (before < room/2+1 || hi == len(items) && before+after+trailRows < room)
	}
	needBelow := func() bool { return hi < len(items) && after < room+1 }
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
		start := max(0, min(len(body)-room, focus-room/2))
		out = append(out[:2:2], body[start:]...)
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

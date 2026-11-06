package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type palette struct {
	hit, muted, selbg, surf, acc, key string
	pens                              [3]string
	agents                            map[string]string // glyph colour per harness; red stays reserved for hits
}

var light = palette{"#b6322d", "#606a74", "#d2e3f9", "#eef3f9", "#2a64b0", "#e3eaf3", [3]string{"#fde89a", "#bdfac8", "#ffe0f2"},
	map[string]string{"claude": "#8d5d1c", "codex": "#147175", "pi": "#3b723e"}}
var dark = palette{"#f6857a", "#95a0ab", "#293647", "#232a35", "#7fb0ee", "#303a49", [3]string{"#3c3207", "#193b22", "#492537"},
	map[string]string{"claude": "#ddae6c", "codex": "#76c7cc", "pi": "#8fc990"}}

type pen struct {
	word  string
	color int
}
type model struct {
	db                         *sql.DB
	q                          string
	harness                    string
	projects                   []string
	rows                       []hit
	messages                   []message
	hitIndex                   map[int]bool
	sel, cursor, width, height int
	limit                      int
	allTime, includeSelf       bool
	focus, full, help, mouse   bool
	prompt                     promptKind
	rng                        timeRange
	pens                       []pen
	status                     string
	revision                   int
	progress                   string
	resume                     *hit
	pal                        palette
	offset                     int
	cancel, loadCancel         context.CancelFunc
	loadRevision               int
	loadedKey                  string
	synced, syncing, tick      bool
	fresh                      int
	initial                    tea.Cmd
}

// promptKind says what the one-line prompt at the bottom collects; status holds the typed text.
type promptKind int

const (
	promptNone promptKind = iota
	promptHighlight
	promptRange
)

type resultMsg struct {
	revision int
	rows     []hit
	err      error
}
type loadMsg struct {
	revision int
	key      string
	messages []message
	hits     map[int]bool
	err      error
}
type syncMsg struct {
	done     bool
	progress string
	err      error
	periodic bool
	added    int
}
type syncTick struct{}

// syncEvery is how often the open TUI looks for sessions written by other
// agents; zero (the default) turns background syncing off. KIOKU_SYNC_EVERY sets it.
var syncEvery time.Duration

func parseSyncEvery(s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 || (d != 0 && d < 5*time.Second) {
		return 0, fmt.Errorf("KIOKU_SYNC_EVERY=%q: use a duration of at least 5s such as 30s or 5m, or 0 to turn it off", s)
	}
	return d, nil
}

type debounce int

func newModel(db *sql.DB, q, h string, projects []string, rows []hit, mouse bool, limit int, includeSelf bool) model {
	p := light
	if os.Getenv("KIOKU_THEME") == "dark" || os.Getenv("KIOKU_THEME") == "" && termenv.HasDarkBackground() {
		p = dark
	}
	m := model{syncing: true, db: db, q: q, harness: h, projects: projects, rows: rows, mouse: mouse, limit: limit, includeSelf: includeSelf, pal: p, width: 80, height: 24, cursor: -1, focus: true}
	m.initial = m.requestLoad()
	return m
}
func (m model) Init() tea.Cmd { return tea.Batch(startSync(false), m.initial) }

// armTick schedules the next background sync unless syncing is off or one is
// already scheduled, so there is never more than one timer chain.
func (m *model) armTick() tea.Cmd {
	if syncEvery <= 0 || m.tick {
		return nil
	}
	m.tick = true
	return tea.Tick(syncEvery, func(time.Time) tea.Msg { return syncTick{} })
}

// startSync does nothing until the runtime runs the command, so tests that
// build model updates never touch the real index.
func startSync(periodic bool) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan syncMsg, 32)
		go func() {
			db, err := openDB()
			if err != nil {
				ch <- syncMsg{done: true, err: err, periodic: periodic}
				close(ch)
				return
			}
			defer db.Close()
			added, err := syncAdded(db, func(i, n int) {
				if periodic {
					return
				}
				if i == 1 || i == n || i%25 == 0 {
					select {
					case ch <- syncMsg{progress: fmt.Sprintf("indexing %d/%d", i, n)}:
					default:
					}
				}
			})
			ch <- syncMsg{done: true, err: err, periodic: periodic, added: added}
			close(ch)
		}()
		return waitSync(ch)()
	}
}

// syncAdded syncs the index and returns how many messages it gained.
// syncStats.Messages holds per-agent totals, so the gain is the difference.
func syncAdded(db *sql.DB, progress func(int, int)) (int, error) {
	var before int
	if err := db.QueryRow("SELECT coalesce(sum(msg_count),0) FROM sessions").Scan(&before); err != nil {
		return 0, err
	}
	stats, err := syncIndex(db, false, progress)
	after := 0
	for _, n := range stats.Messages {
		after += n
	}
	return max(0, after-before), err
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

func queryCmd(ctx context.Context, db *sql.DB, q, h string, projects []string, limit, rev int, allTime, includeSelf bool, rng timeRange) tea.Cmd {
	return func() tea.Msg {
		rows, e := search(ctx, db, q, h, projects, limit, allTime, includeSelf, rng)
		return resultMsg{rev, rows, e}
	}
}
func (m *model) requestQuery() tea.Cmd {
	if m.fresh > 0 { // this query reads the data a background sync added
		m.loadedKey = ""
	}
	m.fresh = 0
	if m.cancel != nil {
		m.cancel()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return queryCmd(ctx, m.db, m.q, m.harness, m.projects, m.limit, m.revision, m.allTime, m.includeSelf, m.rng)
}

// loadKey names what the preview shows: the same session under the same query
// has the same messages and hits, so there is nothing to reload.
func (m model) loadKey(h hit) string {
	return h.UID + "\x00" + m.q + "\x00" + strconv.FormatBool(m.includeSelf)
}
func (m *model) requestLoad() tea.Cmd {
	if m.loadCancel != nil {
		m.loadCancel()
	}
	m.loadRevision++
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
		m.messages, m.hitIndex, m.loadedKey = nil, map[int]bool{}, ""
		return nil
	}
	selected := m.rows[m.sel]
	m.cursor = selected.Index
	key := m.loadKey(selected)
	if key == m.loadedKey {
		return nil
	}
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
		return loadMsg{rev, key, msgs, hits, e}
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
	m.messages, m.hitIndex, m.loadedKey = nil, map[int]bool{}, ""
	return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg { return debounce(m.revision) })
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = x.Width
		m.height = x.Height
	case syncEnvelope:
		if x.x.periodic {
			// Background syncs never move the results or interrupt typing: they
			// only count what is new, and the next query or `r` picks it up.
			if !x.x.done {
				return m, waitSync(x.ch)
			}
			m.syncing = false
			if x.x.err == nil {
				m.fresh += x.x.added
			}
			return m, m.armTick()
		}
		m.progress = x.x.progress
		if x.x.done {
			m.progress = ""
			m.synced, m.syncing = true, false
			if x.x.err != nil {
				m.status = x.x.err.Error()
			}
			m.loadedKey = ""
			return m, tea.Batch(m.requestQuery(), m.armTick())
		}
		return m, waitSync(x.ch)
	case syncTick:
		m.tick = false
		if m.syncing {
			return m, m.armTick()
		}
		m.syncing = true
		return m, startSync(true)
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
				m.loadedKey = x.key
			}
		}
	case openMsg:
		m.status = string(x)
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
		if m.prompt != promptNone {
			switch key {
			case "esc":
				m.prompt = promptNone
				m.status = ""
			case "enter":
				w := strings.TrimSpace(m.status)
				if m.prompt == promptRange {
					return m.setRange(w)
				}
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
				m.prompt = promptNone
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
			hs := []string{"all", "claude", "codex", "pi", "grok", "opencode", "cursor", "agy"}
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
			m.prompt = promptHighlight
			m.status = ""
		case "H":
			m.pens = nil
		case "d":
			m.prompt = promptRange
			m.status = ""
		case "D":
			return m.setRange("")
		case "v":
			m.full = !m.full
		case "?":
			m.help = !m.help
		case "r": // sync the index, then re-run the search
			if m.syncing {
				return m, nil
			}
			m.syncing = true
			return m, startSync(false)
		case "o":
			if len(m.rows) > 0 {
				dir := m.rows[m.sel].CWD
				cmd, terminal, label := openProject(dir)
				if cmd == nil {
					m.status = label
				} else if terminal {
					return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
						return openMsg(openStatus(dir, label, err))
					})
				} else {
					err := cmd.Start()
					if err == nil {
						go cmd.Wait()
					}
					m.status = openStatus(dir, label, err)
				}
			}
		case "y":
			if len(m.rows) > 0 {
				m.status = copyCommand(m.rows[m.sel].ResumeCmd)
			}
		}
	}
	return m, nil
}

// setRange applies the typed "since..until" text (empty clears) and re-runs the search.
func (m model) setRange(spec string) (tea.Model, tea.Cmd) {
	m.prompt = promptNone
	m.status = ""
	rng, e := parseRangeSpec(spec, time.Now())
	if e != nil {
		m.status = e.Error()
		return m, nil
	}
	if rng == m.rng {
		return m, nil
	}
	m.rng = rng
	m.sel = 0
	m.revision++
	return m, m.requestQuery()
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

// agentGlyph paints the harness mark in its own colour, falling back to muted.
func (m model) agentGlyph(h string) string {
	c, ok := m.pal.agents[h]
	if !ok {
		c = m.pal.muted
	}
	return m.color(agentMark(h), c)
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
	case "agy":
		return "a"
	default:
		return "?"
	}
}

// ANSI-aware grapheme width; ambiguous-width characters occupy one cell.

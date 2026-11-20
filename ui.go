package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type palette struct {
	hit, muted, dim, selbg, pulse, surf, acc, key string
	pens                                          [3]string
	agents                                        map[string]string // glyph colour per harness; red stays reserved for hits
}

var light = palette{"#b6322d", "#56606a", "#727c87", "#d2e3f9", "#bfd8f8", "#eef3f9", "#2a64b0", "#e3eaf3", [3]string{"#fde89a", "#bdfac8", "#ffe0f2"},
	harnessColors(false)}
var dark = palette{"#f6857a", "#95a0ab", "#7a8591", "#293647", "#364b68", "#232a35", "#7fb0ee", "#303a49", [3]string{"#3c3207", "#193b22", "#492537"},
	harnessColors(true)}

type pen struct {
	word  string
	color int
}
type model struct {
	db                         *sql.DB
	q                          string
	qpos, hist                 int      // caret (rune index into q); steps back in history, 0 = not walking
	history                    []string // oldest first, loaded by main and saved on exit
	harness                    string
	projects                   []string
	rows                       []hit
	total, totalSessions       int
	messages                   []message
	hitIndex                   map[int]bool
	topics                     map[string]string
	sel, cursor, width, height int
	limit                      int
	allTime, includeSelf       bool
	includeAutomated           bool
	focus, full, help, mouse   bool
	helpAnim                   int  // frames left of the popup opening; 0 = fully open
	outline                    bool // O toggles outline view (user prompts + final replies + hits)
	toolsOpen                  bool // t unfolds tool output in the transcript
	listRatio                  float64
	animActive                 bool
	animListH, animTargetH     int
	animTickID                 int
	pulseTicks                 int
	drag                       dragKind
	lastTitleClick             time.Time
	rail                       railState
	prompt                     promptKind
	rng                        timeRange
	root                       string
	resultRev                  int // revision of the newest query whose result is on screen
	pens                       []pen
	status                     string
	revision                   int
	progress                   string
	pct                        int  // indexing progress, 0-100, for the terminal's progress bar
	blurred                    bool // the terminal window lost focus
	resume                     *hit
	pal                        palette
	offset, transcriptOffset   int
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
	revision             int
	rows                 []hit
	total, totalSessions int
	err                  error
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
	pct      int
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
	if os.Getenv("KIOKU_THEME") == "dark" {
		p = dark
	}
	m := model{
		syncing: true, db: db, q: q, harness: h, projects: projects, rows: rows,
		topics: map[string]string{}, mouse: mouse, limit: limit, includeSelf: includeSelf,
		pal: p, width: 80, height: 24, cursor: -1, focus: true, outline: true,
		qpos: utf8.RuneCountInString(q),
	}
	m.initial = tea.Batch(m.requestLoad(), m.requestTopics())
	return m
}
func (m model) Init() tea.Cmd {
	if os.Getenv("KIOKU_THEME") == "" {
		return tea.Batch(tea.RequestBackgroundColor, startSync(false), m.initial)
	}
	return tea.Batch(startSync(false), m.initial)
}

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
					case ch <- syncMsg{progress: fmt.Sprintf("indexing %d/%d", i, n), pct: i * 100 / max(1, n)}:
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

func queryCmd(ctx context.Context, db *sql.DB, q, h string, projects []string, limit, rev int, allTime, includeSelf, includeAutomated bool, rng timeRange, root ...string) tea.Cmd {
	return func() tea.Msg {
		k := pageKey{Query: q, Harness: h, Projects: projects, Limit: limit, AllTime: allTime, IncludeSelf: includeSelf, IncludeAutomated: includeAutomated, Since: rng.Since, Until: rng.Until}
		if len(root) > 0 {
			k.Root = root[0]
		}
		rows, e := search(ctx, db, k)
		if e != nil {
			return resultMsg{revision: rev, err: e}
		}
		total, sessions, e := searchTotals(ctx, db, k, len(rows))
		return resultMsg{revision: rev, rows: rows, total: total, totalSessions: sessions, err: e}
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
	return queryCmd(ctx, m.db, m.q, m.harness, m.projects, m.limit, m.revision, m.allTime, m.includeSelf, m.includeAutomated, m.rng, m.root)
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
	m.transcriptOffset = 0
	m.cursor = -1
	if m.sel >= len(m.rows) {
		m.sel = max(0, len(m.rows)-1)
	}
	if m.sel < m.offset {
		m.offset = m.sel
	}
	listH := max(1, m.listHeight())
	if m.sel >= m.offset+listH {
		m.offset = m.sel - listH + 1
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
			rows, err := m.db.QueryContext(ctx, `SELECT m.idx FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid WHERE messages_fts MATCH ? AND m.session_id=(SELECT session_id FROM sessions WHERE uid=?) `+selfFilter(includeSelf), toFTS(q), selected.UID)
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
		m.animActive = false
	case animTickMsg:
		if x.id != m.animTickID {
			return m, nil
		}
		m.stepListAnim()
		if m.pulseTicks > 0 {
			m.pulseTicks--
		}
		if m.animActive || m.pulseTicks > 0 {
			return m, m.armAnimTick()
		}
		return m, nil
	case topicsMsg:
		if m.topics == nil {
			m.topics = map[string]string{}
		}
		for uid, topic := range x.topics {
			m.topics[uid] = topic
		}
		return m, nil
	case tea.BackgroundColorMsg:
		if os.Getenv("KIOKU_THEME") == "" {
			if x.IsDark() {
				m.pal = dark
			} else {
				m.pal = light
			}
		}
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
		m.progress, m.pct = x.x.progress, x.x.pct
		if x.x.done {
			m.progress, m.pct = "", 0
			m.synced, m.syncing = true, false
			if x.x.err != nil {
				m.status = x.x.err.Error()
			}
			m.loadedKey = ""
			return m, tea.Batch(m.requestQuery(), m.armTick())
		}
		return m, waitSync(x.ch)
	case helpTickMsg:
		if m.helpAnim > 0 {
			m.helpAnim--
		}
		if m.helpAnim > 0 && m.help {
			return m, helpTick()
		}
		return m, nil
	case tea.FocusMsg:
		return m.onFocus(true)
	case tea.BlurMsg:
		return m.onFocus(false)
	case syncTick:
		m.tick = false
		if m.blurred {
			return m, nil // the next focus-in syncs and restarts the timer
		}
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
			m.resultRev = x.revision
			if x.err != nil {
				if x.err != context.Canceled {
					m.status = x.err.Error()
				}
			} else {
				var prev hit // keep the same message if it is still listed, else the same session
				if m.sel < len(m.rows) {
					prev = m.rows[m.sel]
				}
				m.rows = x.rows
				m.total, m.totalSessions = x.total, x.totalSessions
				m.sel = 0
				for _, same := range []func(hit) bool{
					func(h hit) bool { return prev.ID != 0 && h.ID == prev.ID },
					func(h hit) bool { return prev.UID != "" && h.UID == prev.UID },
				} {
					if i := slices.IndexFunc(m.rows, same); i >= 0 {
						m.sel = i
						break
					}
				}
				return m, tea.Batch(m.requestLoad(), m.requestTopics())
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
				if len(m.rows) > 0 && m.sel < len(m.rows) {
					if topic := messagesTopic(x.messages); topic != "" {
						if m.topics == nil {
							m.topics = map[string]string{}
						}
						m.topics[m.rows[m.sel].UID] = topic
					}
				}
			}
		}
	case openMsg:
		m.status = string(x)
	case tea.MouseMsg:
		return m.mouseUpdate(x)
	case tea.PasteMsg:
		pasted := displayInline(x.Content)
		if pasted == "" {
			return m, nil
		}
		if m.prompt != promptNone {
			m.status += pasted
			return m, nil
		}
		if m.focus {
			m.insertAtCaret(pasted)
			m.status = ""
			return m, m.refresh()
		}
		return m, nil
	case tea.KeyPressMsg:
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
		if key == "ctrl+f" && (!m.focus || m.prompt != promptNone) {
			if m.prompt != promptNone {
				m.prompt = promptNone
				m.status = ""
			}
			m.help = false
			m.focus = true
			return m, nil
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
				if text := keyText(x); text != "" {
					m.status += text
				}
			}
			return m, nil
		}
		if key == "tab" || key == "shift+tab" {
			hs := harnessNames()
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
			m.remember()
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
			case "up", "down":
				if m.historyKey(key) {
					m.status = ""
					return m, m.refresh()
				}
				if key == "down" {
					m.remember()
					m.focus = false
				}
			case "esc":
				m.remember()
				m.focus = false
			default:
				handled, edited := m.queryKey(key)
				if edited {
					m.status = ""
					return m, m.refresh()
				}
				if !handled {
					if text := keyText(x); text != "" {
						m.insertAtCaret(text)
						m.status = ""
						return m, m.refresh()
					}
				}
			}
			return m, nil
		}
		if m.help && key != "?" {
			m.help = false
			if key == "/" {
				m.focus = true
			}
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
			if m.jump(key == "n") {
				return m, m.startPulse()
			}
		case "pgdown", "pgup":
			step := max(1, m.pageHeight()-2)
			if key == "pgup" {
				step = -step
			}
			m.transcriptOffset += step
		case "h":
			m.prompt = promptHighlight
			m.status = ""
		case "H":
			m.pens = nil
		case "p":
			root := ""
			if m.root == "" {
				var err error
				root, err = scopeRoot(".")
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
			}
			m.root = root
			m.status = ""
			m.revision++
			return m, m.requestQuery()
		case "d":
			m.prompt = promptRange
			m.status = ""
		case "D":
			return m.setRange("")
		case "v":
			cur := m.listHeight()
			m.full = !m.full
			return m, m.startListAnim(cur, m.baseListHeight())
		case "O":
			m.outline = !m.outline
			m.transcriptOffset = 0
		case "t":
			m.toolsOpen = !m.toolsOpen
		case "?":
			m.help = !m.help
			if m.help {
				m.helpAnim = helpFrames
				return m, helpTick()
			}
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
				return m, tea.SetClipboard(m.rows[m.sel].ResumeCmd)
			}
		}
	}
	if m.transcriptOffset != 0 {
		m.pageLines(m.width)
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
func (m *model) jump(next bool) bool {
	if len(m.rows) == 0 {
		return false
	}
	hits := []int{}
	for i, x := range m.messages {
		if m.hitIndex[x.Index] {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return false
	}
	at := -1
	for i, v := range hits {
		if v == m.cursor {
			at = i
		}
	}
	m.transcriptOffset = 0
	if next {
		m.cursor = hits[(at+1)%len(hits)]
	} else {
		m.cursor = hits[(at-1+len(hits))%len(hits)]
	}
	return true
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
func keyText(k tea.KeyPressMsg) string {
	if k.Text != "" {
		return k.Text
	}
	if k.Code == tea.KeySpace && k.Mod == 0 {
		return " "
	}
	return ""
}
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

func (m model) pageHeight() int {
	extra := 0
	if m.status != "" && m.prompt == promptNone {
		extra = 1
	}
	return max(1, m.height-m.chromeRows()-m.listHeight()-extra)
}

// headerRows counts query, optional highlights, and a blank row.
func (m model) headerRows() int {
	if len(m.pens) > 0 {
		return 3
	}
	return 2
}

// chromeRows counts the header, list gap (when list is visible), and footer.
func (m model) chromeRows() int {
	if m.listHeight() == 0 {
		return m.headerRows() + 1
	}
	return m.headerRows() + 2
}
func (m model) listHeight() int {
	if m.animActive {
		return max(0, m.animListH)
	}
	return m.baseListHeight()
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
	if h := findHarness(h); h != nil {
		return h.mark
	}
	return "?"
}

// ANSI-aware grapheme width; ambiguous-width characters occupy one cell.

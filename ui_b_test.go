package main

import (
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// B1: the native cursor is a bar in the query box while it has focus, a bar in the status input
// while a prompt is open, and hidden while browsing results.
func TestCursorByMode(t *testing.T) {
	m := lookModel(true)
	m.q, m.qpos = "紅茶x", 2
	cur := m.View().Cursor
	if cur == nil || cur.Shape != tea.CursorBar || cur.Position.X != queryPrefixWidth()+4 || cur.Position.Y != 0 {
		t.Fatalf("focus cursor = %+v", cur)
	}
	m.focus = false
	if m.View().Cursor != nil {
		t.Fatal("browsing results must hide the cursor")
	}
	for _, tc := range []struct {
		prompt promptKind
		prefix string
	}{{promptHighlight, "  highlight ▸ "}, {promptRange, "  range ▸ "}} {
		for _, focus := range []bool{false, true} {
			m.focus, m.prompt, m.status = focus, tc.prompt, "中a"
			cur := m.View().Cursor
			if cur == nil || cur.Shape != tea.CursorBar || cur.Position.X != displayWidth(tc.prefix+"中a") || cur.Position.Y != m.height-1 {
				t.Fatalf("prompt %v focus %v: cursor %+v", tc.prompt, focus, cur)
			}
		}
	}
}

// B2: hit words get a curly underline in the hit colour, never a background or reverse video,
// so the selected row's band and the hit mark do not collide.
func TestHitsUseCurlyUnderline(t *testing.T) {
	m := lookModel(false)
	m.q = "world"
	for _, selected := range []bool{false, true} {
		out := m.paint("hello world", true, selected)
		if !strings.Contains(out, "4:3") || !strings.Contains(out, "58;2;246;133;122") {
			t.Fatalf("selected=%v: want curly underline in the hit colour: %q", selected, out)
		}
		if strings.Contains(out, ";7m") || strings.Contains(out, "[7m") || strings.Contains(out, "48;") {
			t.Fatalf("selected=%v: hit must not use reverse or a background: %q", selected, out)
		}
		if ansi.Strip(out) != "hello world" {
			t.Fatalf("text changed: %q", ansi.Strip(out))
		}
	}
	// With colours stripped (NO_COLOR) the hit must still differ from the text around it.
	plain := m.paint("hello world", true, false)
	if !strings.Contains(plain, "\x1b[1") || !strings.Contains(plain, "4") {
		t.Fatalf("hit keeps bold and underline without colour: %q", plain)
	}
}

// B3: the session directory in the preview title is an OSC 8 link to a percent-encoded file URL,
// and no width calculation counts or leaks the escape.
func TestSessionPathIsHyperlink(t *testing.T) {
	m := lookModel(false)
	m.rows[0].CWD = "/work/My Projects/中文"
	m.topics = map[string]string{}
	m.messages = nil
	m.width, m.height = 100, 24
	lines := m.pageLines(m.width)
	joined := strings.Join(lines, "\n")
	want := "\x1b]8;;file:///work/My%20Projects/%E4%B8%AD%E6%96%87"
	if !strings.Contains(joined, want) {
		t.Fatalf("no file:// hyperlink for the cwd: %q", joined)
	}
	if got := ansi.Strip(joined); !strings.Contains(got, "/work/My Projects/中文") {
		t.Fatalf("visible path lost: %q", got)
	}
	for _, w := range []int{100, 60, 38, 30} {
		m.width = w
		for i, line := range strings.Split(m.View().Content, "\n") {
			if displayWidth(line) > w {
				t.Fatalf("width %d line %d is %d wide", w, i, displayWidth(line))
			}
			if strings.Count(line, "\x1b]8;")%2 != 0 {
				t.Fatalf("width %d line %d leaves a hyperlink open: %q", w, i, line)
			}
		}
	}
	if displayWidth("a\x1b]8;;file:///x\x1b\\bc\x1b]8;;\x1b\\") != 3 {
		t.Fatal("displayWidth must ignore OSC 8")
	}
}

func luminance(hex string) float64 {
	c := parseHex(hex)
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}
func parseHex(hex string) color.RGBA {
	var r, g, b uint8
	n := strings.TrimPrefix(hex, "#")
	for i, p := range []*uint8{&r, &g, &b} {
		var v uint8
		for _, ch := range n[i*2 : i*2+2] {
			v <<= 4
			switch {
			case ch >= 'a':
				v |= uint8(ch-'a') + 10
			default:
				v |= uint8(ch - '0')
			}
		}
		*p = v
	}
	return color.RGBA{r, g, b, 255}
}
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// B4: text colours must stay readable on the terminal background and on the palette's own bands.
func TestPaletteContrast(t *testing.T) {
	for _, tc := range []struct {
		name string
		pal  palette
		bg   string
	}{{"light", light, "#ffffff"}, {"dark", dark, "#1e1e1e"}} {
		for _, band := range []struct{ name, bg string }{{"terminal", tc.bg}, {"surf", tc.pal.surf}, {"selbg", tc.pal.selbg}} {
			for _, fg := range []struct {
				name, hex string
				min       float64
			}{{"hit", tc.pal.hit, 4.5}, {"muted", tc.pal.muted, 4.5}, {"acc", tc.pal.acc, 3}, {"dim", tc.pal.dim, 3}} {
				if band.name == "terminal" && fg.name == "acc" {
					continue
				}
				if got := contrast(fg.hex, band.bg); got < fg.min {
					t.Errorf("%s %s %s on %s %s: contrast %.2f < %.1f", tc.name, fg.name, fg.hex, band.name, band.bg, got, fg.min)
				}
			}
		}
	}
}

// B5: an empty result lists only the hints that apply to the current state.
func TestEmptyResultHints(t *testing.T) {
	view := func(m model) string {
		m.rows, m.width, m.height = nil, 100, 30
		return ansi.Strip(m.View().Content)
	}
	m := lookModel(true)
	m.q = "alpha"
	one := view(m)
	for _, bad := range []string{"Drop a word", "date range", "project scope", "all agents"} {
		if strings.Contains(one, bad) {
			t.Errorf("single plain term shows inapplicable hint %q:\n%s", bad, one)
		}
	}
	if !strings.Contains(one, "No message") {
		t.Errorf("no empty-result message:\n%s", one)
	}
	m.q = "alpha beta"
	if v := view(m); !strings.Contains(v, "Drop a word") {
		t.Errorf("two terms need the fewer-terms hint:\n%s", v)
	}
	m.q = `"alpha"`
	if v := view(m); !strings.Contains(v, "remove the quotes") {
		t.Errorf("phrase needs the quotes hint:\n%s", v)
	}
	m.q = "alpha -beta"
	if v := view(m); !strings.Contains(v, "-beta") {
		t.Errorf("exclusion needs a hint naming it:\n%s", v)
	}
	m.q, m.rng = "alpha", timeRange{Since: "2026-01-01"}
	if v := view(m); !strings.Contains(v, "Press D") {
		t.Errorf("active range needs the D hint:\n%s", v)
	}
	m.rng, m.root = timeRange{}, "/tmp/proj"
	if v := view(m); !strings.Contains(v, "Press p") {
		t.Errorf("active scope needs the p hint:\n%s", v)
	}
	m.root, m.harness = "", "claude"
	if v := view(m); !strings.Contains(v, "all agents") {
		t.Errorf("one agent selected needs the tab hint:\n%s", v)
	}
	m.q, m.harness = "", "all"
	if v := view(m); strings.Contains(v, "No message contains") {
		t.Errorf("empty query must not claim words matched nothing:\n%s", v)
	}
}

func rowsModel() model {
	m := lookModel(false)
	m.rows = []hit{{UID: "a", ID: 1, Snippet: "first"}, {UID: "b", ID: 2, Snippet: "second"}, {UID: "c", ID: 3, Snippet: "third"}}
	m.sel = 1
	return m
}

// B6: a new query keeps the old rows, faint, until its own result arrives; the selection then
// stays on the same session when that session is still listed.
func TestPendingResultsStayFaintAndKeepSelection(t *testing.T) {
	m := rowsModel()
	steady := m.View().Content
	if strings.Contains(steady, "\x1b[2m") {
		t.Fatal("settled results must not be faint")
	}
	m.q, m.qpos = "x", 1
	m.refresh()
	rev := m.revision
	pending := m.View().Content
	if !strings.Contains(pending, "\x1b[2m") || !strings.Contains(ansi.Strip(pending), "second") {
		t.Fatalf("pending results must keep old rows, faint:\n%s", ansi.Strip(pending))
	}
	// a stale revision repaints nothing
	next, _ := m.Update(resultMsg{revision: rev - 1, rows: []hit{{UID: "z", Snippet: "stale"}}})
	m = next.(model)
	if len(m.rows) != 3 || m.sel != 1 || !strings.Contains(m.View().Content, "\x1b[2m") {
		t.Fatalf("stale result changed the page: rows %d sel %d", len(m.rows), m.sel)
	}
	next, _ = m.Update(resultMsg{revision: rev, rows: []hit{{UID: "c", ID: 9, Snippet: "c"}, {UID: "b", ID: 8, Snippet: "b"}, {UID: "d", ID: 7, Snippet: "d"}}})
	m = next.(model)
	if m.sel != 1 || m.rows[m.sel].UID != "b" {
		t.Fatalf("selection moved off session b: sel %d", m.sel)
	}
	if strings.Contains(m.View().Content, "\x1b[2m") {
		t.Fatal("arrived results must not be faint")
	}
	// when the session is gone the selection falls back to the first row
	m.q = "y"
	m.refresh()
	next, _ = m.Update(resultMsg{revision: m.revision, rows: []hit{{UID: "d"}, {UID: "e"}}})
	if got := next.(model); got.sel != 0 {
		t.Fatalf("missing session must select the first row, got %d", got.sel)
	}
}

// `?` opens the help as a popup over the live screen: the results behind it stay visible, the
// box is centred and framed, every line keeps the screen width, and it fits a 24-row terminal.
func TestHelpIsAPopupOverTheScreen(t *testing.T) {
	m := lookModel(false)
	m.width, m.height = 100, 30
	before := strings.Split(ansi.Strip(m.View().Content), "\n")
	m.help = true
	after := strings.Split(ansi.Strip(m.View().Content), "\n")
	if len(after) != len(before) {
		t.Fatalf("popup changed the screen height: %d != %d", len(after), len(before))
	}
	top := -1
	for i, l := range after {
		if w := displayWidth(l); w != m.width {
			t.Fatalf("row %d is %d wide, want %d: %q", i, w, m.width, l)
		}
		if strings.Contains(l, "╭") {
			top = i
		}
	}
	if top < 1 {
		t.Fatalf("no popup frame in:\n%s", strings.Join(after, "\n"))
	}
	if after[0] != before[0] {
		t.Fatalf("query row must stay visible behind the popup:\n%q\n%q", after[0], before[0])
	}
	same := 0
	for i := range before {
		if before[i] == after[i] {
			same++
		}
	}
	if same == 0 {
		t.Fatal("nothing of the original screen is left around the popup")
	}
	m.width, m.height = 80, 24
	small := ansi.Strip(m.View().Content)
	for _, l := range helpLines() {
		if l != "" && !strings.Contains(small, strings.TrimSpace(l)) {
			t.Fatalf("help line cut off at 80x24: %q\n%s", l, small)
		}
	}
}

// The View asks the terminal for focus reports, names the tab after the search, and mirrors
// first-run indexing in the terminal's progress indicator.
func TestViewTerminalHints(t *testing.T) {
	m := lookModel(false)
	v := m.View()
	if !v.ReportFocus {
		t.Fatal("focus reports must be on")
	}
	if want := "kioku · fts5 · 2 hits"; v.WindowTitle != want {
		t.Fatalf("title = %q, want %q", v.WindowTitle, want)
	}
	m.q = ""
	if got := m.View().WindowTitle; got != "kioku" {
		t.Fatalf("browsing title = %q", got)
	}
	if m.View().ProgressBar != nil {
		t.Fatal("no progress bar while idle")
	}
	m.progress, m.pct = "indexing 5/10", 50
	if pb := m.View().ProgressBar; pb == nil || pb.State != tea.ProgressBarDefault || pb.Value != 50 {
		t.Fatalf("progress bar = %+v", pb)
	}
	m.pct = 0
	if pb := m.View().ProgressBar; pb == nil || pb.State != tea.ProgressBarIndeterminate {
		t.Fatalf("want an indeterminate bar before the first count, got %+v", pb)
	}
}

// Focus-in catches up with a sync only when timed syncing is on and the first scan is done;
// a timer that fires while the window is in the background waits for focus.
func TestFocusSyncsOnlyWhenTimedSyncIsOn(t *testing.T) {
	defer func(d time.Duration) { syncEvery = d }(syncEvery)
	m := lookModel(false)
	m.synced = true
	syncEvery = 0
	if got, cmd := m.onFocus(true); got.(model).syncing || cmd != nil {
		t.Fatal("focus must not sync when background syncing is off")
	}
	syncEvery = time.Minute
	blurred, _ := m.onFocus(false)
	if !blurred.(model).blurred {
		t.Fatal("blur must be remembered")
	}
	ticked, cmd := blurred.(model).Update(syncTick{})
	if ticked.(model).syncing || cmd != nil {
		t.Fatal("a tick in the background must not start a sync")
	}
	back, cmd := blurred.(model).onFocus(true)
	if !back.(model).syncing || cmd == nil || back.(model).blurred {
		t.Fatal("focus-in must start a sync")
	}
}

// A click outside the help popup closes it; a click inside leaves it open.
func TestHelpClosesOnOutsideClick(t *testing.T) {
	m := lookModel(false)
	m.mouse, m.help = true, true
	r := m.helpRect()
	if r.w == 0 || !r.contains(r.x+1, r.y+1) || r.contains(0, 0) {
		t.Fatalf("bad popup rect %+v", r)
	}
	in, _ := m.Update(tea.MouseClickMsg{X: r.x + 2, Y: r.y + 2, Button: tea.MouseLeft})
	if !in.(model).help {
		t.Fatal("click inside must keep help open")
	}
	out, _ := m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if out.(model).help {
		t.Fatal("click outside must close help")
	}
}

// The preview title links the project to its directory even when a topic replaces the path text.
func TestProjectNameLinksToDirectory(t *testing.T) {
	m := lookModel(false)
	m.rows[0].CWD = "/work/My Projects/中文"
	m.topics = map[string]string{"a": "a topic"}
	m.messages = nil
	joined := strings.Join(m.pageLines(m.width), "\n")
	if !strings.Contains(joined, "\x1b]8;;file:///work/My%20Projects/%E4%B8%AD%E6%96%87") {
		t.Fatalf("no OSC 8 link on the title: %q", joined)
	}
}

func TestHelpOpensOverFrames(t *testing.T) {
	m := lookModel(false)
	m.width, m.height, m.help = 100, 30, true
	full := strings.Count(ansi.Strip(m.View().Content), "Results")
	m.helpAnim = helpFrames
	if strings.Contains(ansi.Strip(m.View().Content), "Transcript") {
		t.Fatal("first frame must show only the top of the popup")
	}
	for i := 0; i < helpFrames; i++ {
		next, _ := m.Update(helpTickMsg{})
		m = next.(model)
	}
	if m.helpAnim != 0 || strings.Count(ansi.Strip(m.View().Content), "Results") != full {
		t.Fatalf("popup must be fully open after %d ticks (anim=%d)", helpFrames, m.helpAnim)
	}
}

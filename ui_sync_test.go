package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func syncModel() model {
	m := loadModel()
	m.synced = true
	m.sel = 1
	m.progress = ""
	return m
}

func withSyncEvery(t *testing.T, d time.Duration) {
	t.Helper()
	old := syncEvery
	syncEvery = d
	t.Cleanup(func() { syncEvery = old })
}

func envelope(x syncMsg) syncEnvelope { return syncEnvelope{x: x, ch: make(chan syncMsg)} }

func TestPeriodicSyncShowsNewCountWithoutMovingSelection(t *testing.T) {
	withSyncEvery(t, 15*time.Second)
	m := syncModel()
	rows := m.rows
	next, cmd := m.Update(envelope(syncMsg{done: true, periodic: true, added: 7}))
	got := next.(model)
	if got.fresh != 7 || got.sel != 1 || len(got.rows) != len(rows) || got.messages == nil {
		t.Fatalf("results must stay put: fresh=%d sel=%d rows=%d", got.fresh, got.sel, len(got.rows))
	}
	if cmd == nil {
		t.Fatal("the next sync must be scheduled")
	}
	next, _ = got.Update(envelope(syncMsg{done: true, periodic: true, added: 3}))
	if next.(model).fresh != 10 {
		t.Fatal("new messages accumulate until the view is refreshed")
	}
}

func TestPeriodicSyncIsQuiet(t *testing.T) {
	withSyncEvery(t, 15*time.Second)
	m := syncModel()
	next, _ := m.Update(envelope(syncMsg{done: true, periodic: true}))
	if got := next.(model); got.fresh != 0 || got.progress != "" || got.status != "" {
		t.Fatalf("an idle sync must change nothing: %+v", got.fresh)
	}
	next, _ = m.Update(envelope(syncMsg{done: true, periodic: true, err: errors.New("database is locked"), added: 4}))
	if got := next.(model); got.fresh != 0 || got.status != "" {
		t.Fatal("a failed background sync must not report or count anything")
	}
	next, _ = m.Update(envelope(syncMsg{periodic: true, progress: "indexing 3/9"}))
	if next.(model).progress != "" {
		t.Fatal("background progress must not show")
	}
}

func TestSyncCompletionInvalidatesOnlyForegroundPreview(t *testing.T) {
	withSyncEvery(t, 0)
	for _, tt := range []struct {
		name             string
		synced, periodic bool
	}{
		{"first sync", false, false},
		{"manual sync", true, false},
		{"periodic sync", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := syncModel()
			m.synced = tt.synced
			next, _ := m.Update(envelope(syncMsg{done: true, periodic: tt.periodic}))
			want := ""
			if tt.periodic {
				want = m.loadedKey
			}
			if got := next.(model).loadedKey; got != want {
				t.Fatalf("loadedKey = %q, want %q", got, want)
			}
		})
	}
}

func TestFirstSyncRequeriesAndArmsTickOnlyWhenEnabled(t *testing.T) {
	for _, c := range []struct {
		every time.Duration
		armed bool
	}{{0, false}, {15 * time.Second, true}} {
		withSyncEvery(t, c.every)
		m := syncModel()
		m.synced, m.syncing = false, true
		next, cmd := m.Update(envelope(syncMsg{done: true}))
		got := next.(model)
		if !got.synced || got.syncing || cmd == nil || got.tick != c.armed {
			t.Fatalf("every=%v: synced=%v syncing=%v tick=%v", c.every, got.synced, got.syncing, got.tick)
		}
	}
}

func TestTickRunsOneSyncAtATime(t *testing.T) {
	withSyncEvery(t, 15*time.Second)
	m := syncModel()
	m.tick = true
	next, cmd := m.Update(syncTick{})
	got := next.(model)
	if cmd == nil || !got.syncing || got.tick {
		t.Fatalf("a tick must start a sync: syncing=%v tick=%v", got.syncing, got.tick)
	}
	next, cmd = got.Update(syncTick{})
	again := next.(model)
	if cmd == nil || !again.tick {
		t.Fatal("a tick during a sync must only reschedule itself")
	}
	next, _ = again.Update(envelope(syncMsg{done: true, periodic: true}))
	if !next.(model).tick || next.(model).syncing {
		t.Fatal("finishing a sync keeps exactly one tick pending")
	}
}

func TestOnlyOneTickChain(t *testing.T) {
	withSyncEvery(t, 15*time.Second)
	m := syncModel()
	m.tick = true
	next, _ := m.Update(envelope(syncMsg{done: true, periodic: true}))
	if !next.(model).tick {
		t.Fatal("tick must stay armed")
	}
	// done twice while a tick is already pending must not stack another timer
	if cmd := (&model{tick: true}).armTick(); cmd != nil {
		t.Fatal("armTick must be a no-op while a tick is pending")
	}
}

func TestRefreshKeySyncsThenRequeries(t *testing.T) {
	withSyncEvery(t, 0)
	m := syncModel()
	m.focus = false
	m.fresh = 5
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	got := next.(model)
	if cmd == nil || !got.syncing {
		t.Fatal("r must start a sync even when automatic sync is off")
	}
	next, cmd = got.Update(envelope(syncMsg{done: true}))
	done := next.(model)
	if cmd == nil || done.syncing || done.fresh != 0 || done.tick {
		t.Fatalf("after r's sync: requery, count cleared, no tick when off: syncing=%v fresh=%d tick=%v", done.syncing, done.fresh, done.tick)
	}
	// ignored while a sync is already running
	got.fresh = 3
	if next, cmd = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); cmd != nil || next.(model).fresh != 3 {
		t.Fatal("r during a running sync must do nothing")
	}
	// in the search box r is just a letter
	m.focus = true
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if g := next.(model); g.q != m.q+"r" || g.syncing {
		t.Fatalf("in the search box r is a letter: q=%q", g.q)
	}
}

func TestParseSyncEvery(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false}, {"0", 0, false}, {"0s", 0, false},
		{"15s", 15 * time.Second, false}, {"2m", 2 * time.Minute, false}, {"5s", 5 * time.Second, false},
		{"4s", 0, true}, {"1500ms", 0, true}, {"-30s", 0, true}, {"abc", 0, true}, {"15", 0, true},
	} {
		got, err := parseSyncEvery(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%q: got %v, %v", c.in, got, err)
		}
		if err != nil && !strings.Contains(err.Error(), "KIOKU_SYNC_EVERY") {
			t.Errorf("%q: error must name the variable: %v", c.in, err)
		}
	}
}

func TestNewCountShowsInHeader(t *testing.T) {
	m := syncModel()
	m.fresh = 12
	if v := m.View(); !strings.Contains(v, "12 new") {
		t.Fatalf("header must show the new count:\n%s", v)
	}
	m.fresh = 0
	if v := m.View(); strings.Contains(v, " new") {
		t.Fatal("no new count when nothing is new")
	}
}

func writeClaudeSession(t *testing.T, home, id, text string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "-work-demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":%q},"uuid":"%s","timestamp":"2026-09-27T10:00:00.000Z","cwd":"/work/demo","sessionId":"%s","version":"1.0.0"}`+"\n", text, id, id)
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAddedReturnsBaselineError(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	added, err := syncAdded(db, nil)
	if added != 0 || err == nil || !strings.Contains(err.Error(), "no such table: sessions") {
		t.Fatalf("missing baseline must return 0 and the sessions query error, got %d, %v", added, err)
	}
}

// A background sync reports only what it added, never the index total.
func TestSyncAddedCountsOnlyNewMessages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KIOKU_INDEX", filepath.Join(home, "index.db"))
	writeClaudeSession(t, home, "11111111-1111-4111-8111-111111111111", "first session")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if added, err := syncAdded(db, nil); err != nil || added != 1 {
		t.Fatalf("first sync: added=%d err=%v", added, err)
	}
	if added, err := syncAdded(db, nil); err != nil || added != 0 {
		t.Fatalf("an idle sync must add nothing, got %d (err=%v)", added, err)
	}
	writeClaudeSession(t, home, "22222222-2222-4222-8222-222222222222", "second session")
	if added, err := syncAdded(db, nil); err != nil || added != 1 {
		t.Fatalf("one new message, got %d (err=%v)", added, err)
	}
}

// Any query that consumes background-synced data must also reload the preview,
// or the same session keeps showing its old transcript.
func TestQueryAfterBackgroundSyncInvalidatesPreview(t *testing.T) {
	m := syncModel()
	m.fresh = 3
	if key := m.loadedKey; key == "" {
		t.Fatal("test needs a loaded preview")
	}
	m.requestQuery()
	if m.loadedKey != "" || m.fresh != 0 {
		t.Fatalf("new data was consumed, preview must reload: loadedKey=%q fresh=%d", m.loadedKey, m.fresh)
	}
	m = syncModel()
	keep := m.loadedKey
	m.requestQuery()
	if m.loadedKey != keep {
		t.Fatal("with nothing new the preview cache must be kept (no flicker)")
	}
}

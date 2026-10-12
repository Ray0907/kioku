package main

import "testing"

func loadModel() model {
	m := model{width: 100, height: 30, pal: dark, q: "checkout", hitIndex: map[int]bool{}, cursor: -1}
	m.rows = []hit{{UID: "a", Index: 1}, {UID: "b", Index: 2}}
	m.messages = []message{{Index: 0, Text: "old"}}
	m.hitIndex = map[int]bool{0: true}
	m.loadedKey = m.loadKey(m.rows[0])
	return m
}

func TestRequestLoadKeepsPreviewWhileLoading(t *testing.T) {
	m := loadModel()
	m.sel = 1
	if m.requestLoad() == nil {
		t.Fatal("a different session must start a load")
	}
	if len(m.messages) != 1 || !m.hitIndex[0] {
		t.Fatalf("preview was cleared before the new one arrived: %v %v", m.messages, m.hitIndex)
	}
}

func TestRequestLoadSkipsSameSession(t *testing.T) {
	m := loadModel()
	if cmd := m.requestLoad(); cmd != nil {
		t.Fatal("same session and query is already loaded")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor must still follow the selected hit, got %d", m.cursor)
	}
}

func TestRequestLoadReloadsWhenQueryChanges(t *testing.T) {
	m := loadModel()
	m.q = "refund"
	if m.requestLoad() == nil {
		t.Fatal("hits depend on the query, a new query must reload")
	}
}

func TestRequestLoadClearsWhenNoRows(t *testing.T) {
	m := loadModel()
	m.rows = nil
	if m.requestLoad() != nil || m.messages != nil || len(m.hitIndex) != 0 {
		t.Fatal("no rows must leave an empty preview")
	}
}

func TestLoadMsgRecordsKey(t *testing.T) {
	m := loadModel()
	m.sel = 1
	m.requestLoad()
	next, _ := m.Update(loadMsg{revision: m.loadRevision, key: m.loadKey(m.rows[1]), messages: []message{{Text: "new"}}})
	if got := next.(model); got.loadedKey != m.loadKey(m.rows[1]) || got.messages[0].Text != "new" {
		t.Fatalf("loadMsg not applied: %+v", got.loadedKey)
	}
}

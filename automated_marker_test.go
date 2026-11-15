package main

import (
	"encoding/json"
	"testing"
)

func markerTotal(t *testing.T, args ...string) int {
	t.Helper()
	out, err := runArgs(t, append([]string{"--json", "mk9probe"}, args...)...)
	var p hitPage
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	return p.Total
}

// A session opened by a prompt that starts with a backpass marker is automated; an index
// classified by an older rule is classified again once, without a rebuild.
func TestAutomatedMarkerPrompt(t *testing.T) {
	doctorHome(t)
	appendAutomatedMessage(t, "audit", "user", "<!-- backpass:self-session -->\nYou are mk9probe auditing")
	appendAutomatedMessage(t, "plain", "user", "mk9probe fix the login page")
	appendAutomatedMessage(t, "quoted", "user", "mk9probe saw <!-- backpass:self-session --> in a log")
	if out, err := runArgs(t, "index", "--rebuild"); err != nil {
		t.Fatalf("index: %s: %v", out, err)
	}
	if got := markerTotal(t); got != 2 {
		t.Fatalf("default search: %d hits, want 2", got)
	}
	if got := markerTotal(t, "--include-automated"); got != 3 {
		t.Fatalf("--include-automated: %d hits, want 3", got)
	}
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	var ids int64
	if err = db.QueryRow(`SELECT max(id) FROM messages`).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE sessions SET automated=0; UPDATE meta SET schema_version=6`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if got := markerTotal(t); got != 2 {
		t.Fatalf("after upgrade: %d hits, want 2", got)
	}
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var after int64
	if err = db.QueryRow(`SELECT schema_version,(SELECT max(id) FROM messages) FROM meta`).Scan(&version, &after); err != nil || version != 7 || after != ids {
		t.Fatalf("version=%d max id %d -> %d err=%v, want 7 and no rebuild", version, ids, after, err)
	}
}

package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKeySession(t *testing.T, path, uid, text string, appendOnly bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"type": "user", "timestamp": "2026-10-01T00:00:00Z", "cwd": "/work/key", "sessionId": uid, "message": map[string]any{"role": "user", "content": text}}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if appendOnly {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	} else {
		next := path + ".next"
		if err := os.WriteFile(next, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(next, path); err != nil {
			t.Fatal(err)
		}
	}
}

func syncKeyIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := syncIndex(db, false, nil); err != nil {
		t.Fatal(err)
	}
	checkIntegerIndex(t, db)
}

func TestSessionKeyReuseAndAppend(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, ".claude/projects/key/session.jsonl")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writeKeySession(t, path, "first", "oldsecret", false)
	syncKeyIndex(t, db)
	var first int64
	if err = db.QueryRow(`SELECT session_id FROM sessions`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	syncKeyIndex(t, db)
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("removed messages=%d %v", count, err)
	}
	writeKeySession(t, path, "second", "newmessage", false)
	syncKeyIndex(t, db)
	var second int64
	if err = db.QueryRow(`SELECT session_id FROM sessions`).Scan(&second); err != nil || second != first {
		t.Fatalf("fixture must reuse id: %d vs %d %v", first, second, err)
	}
	writeKeySession(t, path, "second", "appendedmessage", true)
	syncKeyIndex(t, db)
	got := indexRows(t, db, `SELECT text FROM messages ORDER BY id`)
	if strings.Contains(got, "oldsecret") || !strings.Contains(got, "newmessage") || !strings.Contains(got, "appendedmessage") {
		t.Fatalf("wrong messages: %s", got)
	}
	if err = db.QueryRow(`SELECT count(*) FROM messages WHERE session_id=?`, second).Scan(&count); err != nil || count != 2 {
		t.Fatalf("append id: %d %v", count, err)
	}
	// Replace the same path with a different uid without an intervening deletion scan.
	writeKeySession(t, path, "third", "replacementmessage", false)
	syncKeyIndex(t, db)
	if got = indexRows(t, db, `SELECT text FROM messages ORDER BY id`); strings.Contains(got, "newmessage") || strings.Contains(got, "appendedmessage") || !strings.Contains(got, "replacementmessage") {
		t.Fatalf("uid replacement leaked: %s", got)
	}
	if err = db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("sessions=%d %v", count, err)
	}
}

func TestSessionKeyRemoveRollback(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, ".claude/projects/key/session.jsonl")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writeKeySession(t, path, "first", "rollbackmessage", false)
	syncKeyIndex(t, db)
	before := indexRows(t, db, integerMessages) + indexRows(t, db, publicSessions)
	// The next statement after deleting messages fails, so both deletions must roll back.
	if _, err = db.Exec(`CREATE TRIGGER fail_session_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'after messages delete'); END;`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = removeSource(tx, path)
	if err == nil || !strings.Contains(err.Error(), "after messages delete") {
		tx.Rollback()
		t.Fatalf("missing injected failure: %v", err)
	}
	var n int
	if err = tx.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n != 0 {
		tx.Rollback()
		t.Fatalf("failure was not after message deletion: %d %v", n, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := indexRows(t, db, integerMessages) + indexRows(t, db, publicSessions); got != before {
		t.Fatal("rollback did not preserve session and messages")
	}
	checkIntegerIndex(t, db)
}

func TestSessionKeyNoReplace(t *testing.T) {
	source, err := os.ReadFile("index.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(string(source)), "INSERT OR REPLACE INTO SESSIONS") {
		t.Fatal("session replacement can invalidate message ids")
	}
}

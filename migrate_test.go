package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This is the version-5 layout, including external-content FTS and explicit rowids.
func legacyIndex(t *testing.T, version int) *sql.DB {
	t.Helper()
	doctorHome(t)
	db := doctorDB(t, indexPath(), `PRAGMA journal_mode=WAL;
CREATE TABLE meta(schema_version INTEGER NOT NULL);
CREATE TABLE sessions(uid TEXT PRIMARY KEY,harness TEXT,native_id TEXT,path TEXT,cwd TEXT,project TEXT,model TEXT,started TEXT,updated TEXT,msg_count INTEGER,automated INTEGER NOT NULL DEFAULT 0);
CREATE TABLE messages(id INTEGER PRIMARY KEY,session_uid TEXT,idx INTEGER,ts TEXT,role TEXT,text TEXT,self INTEGER DEFAULT 0);
CREATE INDEX messages_session ON messages(session_uid,idx);
CREATE INDEX messages_ts ON messages(ts);
CREATE TABLE sources(path TEXT PRIMARY KEY,mtime INTEGER,size INTEGER,offset INTEGER DEFAULT 0,inode INTEGER DEFAULT 0,tool_calls TEXT DEFAULT '[]',updated INTEGER DEFAULT 0);
CREATE TABLE dirs(path TEXT PRIMARY KEY,mtime INTEGER);
CREATE VIRTUAL TABLE messages_fts USING fts5(text,content='messages',content_rowid='id',tokenize='cjk unigram 1 remove_diacritics 2');
CREATE VIRTUAL TABLE messages_vocab USING fts5vocab(messages_fts,'row');
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid,text) VALUES(new.id,new.text); END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,text) VALUES('delete',old.id,old.text); END;
INSERT INTO sessions(rowid,uid,harness,native_id,path,cwd,project,model,started,updated,msg_count,automated) VALUES
 (11,'a','claude','a','/fixture/a','/w/a','a','model','2026-10-01','2026-10-02',3,0),
 (20,'b','pi','b','/fixture/b','/w/b','b','model','2026-10-01','2026-10-02',1,1),
 (21,'empty','pi','empty','/fixture/empty','/w/b','b','','','',0,0);
INSERT INTO messages(id,session_uid,idx,ts,role,text,self) VALUES
 (2,'a',1,'2026-10-01T00:00:00Z','user','migrationneedle 茶葉',0),
 (5,'a',1,'2026-10-01T00:00:00Z','asst','migrationneedle tied',1),
 (8,'a',3,'2026-10-02T00:00:00Z','tool','migrationneedle tool',0),
 (9,'b',0,'2026-10-01T00:00:00Z','user','Review this change migrationneedle',0);`)
	if _, err := db.Exec("INSERT INTO meta VALUES(?)", version); err != nil {
		t.Fatal(err)
	}
	return db
}

func indexRows(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range values {
			if s, ok := v.([]byte); ok {
				values[i] = string(s)
			}
		}
		fmt.Fprintf(&b, "%#v\n", values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

const publicSessions = `SELECT uid,harness,native_id,path,cwd,project,model,started,updated,msg_count,automated FROM sessions ORDER BY uid`
const integerMessages = `SELECT m.id,s.uid,m.idx,m.ts,m.role,m.text,m.self FROM messages m JOIN sessions s ON s.session_id=m.session_id ORDER BY m.id`
const legacyMessages = `SELECT id,session_uid,idx,ts,role,text,self FROM messages ORDER BY id`

func checkIntegerIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	var version, orphans, messages, docs int
	if err := db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 6 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages m LEFT JOIN sessions s ON s.session_id=m.session_id WHERE s.session_id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("orphans=%d err=%v", orphans, err)
	}
	if _, err := db.Exec(`INSERT INTO messages_fts(messages_fts,rank) VALUES('integrity-check',1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages_fts_docsize`).Scan(&docs); err != nil || docs != messages {
		t.Fatalf("FTS docs=%d messages=%d err=%v", docs, messages, err)
	}
}

func TestIntegerMigrationContent(t *testing.T) {
	old := legacyIndex(t, 5)
	sessions := indexRows(t, old, publicSessions)
	messages := indexRows(t, old, legacyMessages)
	matches := indexRows(t, old, `SELECT rowid FROM messages_fts WHERE messages_fts MATCH 'migrationneedle' ORDER BY rowid`)
	ids := indexRows(t, old, `SELECT rowid,uid FROM sessions ORDER BY rowid`)
	old.Close()
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	checkIntegerIndex(t, db)
	if got := indexRows(t, db, publicSessions); got != sessions {
		t.Fatalf("sessions changed: %s", got)
	}
	if got := indexRows(t, db, integerMessages); got != messages {
		t.Fatalf("messages changed: %s", got)
	}
	if got := indexRows(t, db, `SELECT session_id,uid FROM sessions ORDER BY session_id`); got != ids {
		t.Fatalf("session rowids changed: %s", got)
	}
	if got := indexRows(t, db, `SELECT rowid FROM messages_fts WHERE messages_fts MATCH 'migrationneedle' ORDER BY rowid`); got != matches {
		t.Fatal("MATCH rowids changed")
	}
	// A fresh index populated with the same public rows has the same logical content.
	path := filepath.Join(t.TempDir(), "fresh.db")
	t.Setenv("KIOKU_INDEX", path)
	fresh, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err = fresh.Exec(`ATTACH DATABASE ? AS migrated`, dbPath(db, t)); err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Exec(`INSERT INTO sessions(uid,harness,native_id,path,cwd,project,model,started,updated,msg_count,automated) SELECT uid,harness,native_id,path,cwd,project,model,started,updated,msg_count,automated FROM migrated.sessions ORDER BY session_id;
INSERT INTO messages(id,session_id,idx,ts,role,text,self) SELECT m.id,s.session_id,m.idx,m.ts,m.role,m.text,m.self FROM migrated.messages m JOIN migrated.sessions old ON old.session_id=m.session_id JOIN sessions s ON s.uid=old.uid ORDER BY m.id;
UPDATE meta SET schema_version=6;`); err != nil {
		t.Fatal(err)
	}
	checkIntegerIndex(t, fresh)
	if indexRows(t, fresh, integerMessages) != messages || indexRows(t, fresh, publicSessions) != sessions {
		t.Fatal("fresh and migrated differ")
	}
}

func dbPath(db *sql.DB, t *testing.T) string {
	t.Helper()
	var seq int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}

var migrationSteps = []string{"columns", "drop-triggers", "create-tables", "copy-sessions", "copy-messages", "drop-tables", "rename-tables", "indexes", "triggers", "rebuild", "version"}

func TestIntegerMigrationRollback(t *testing.T) {
	for _, step := range migrationSteps {
		t.Run(step, func(t *testing.T) {
			old := legacyIndex(t, 5)
			before := indexRows(t, old, legacyMessages) + indexRows(t, old, publicSessions)
			schema := indexRows(t, old, `SELECT name,sql FROM sqlite_master ORDER BY name`)
			injected := errors.New("injected migration failure")
			migrateHook = func(at string) error {
				if at == step {
					return injected
				}
				return nil
			}
			defer func() { migrateHook = nil }()
			db, err := openDB()
			if db != nil {
				db.Close()
			}
			if !errors.Is(err, injected) {
				t.Fatalf("step %s: %v", step, err)
			}
			if got := indexRows(t, old, legacyMessages) + indexRows(t, old, publicSessions); got != before {
				t.Fatal("rollback changed rows")
			}
			if got := indexRows(t, old, `SELECT name,sql FROM sqlite_master ORDER BY name`); got != schema {
				t.Fatal("rollback changed schema")
			}
			var version int
			if err := old.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 5 {
				t.Fatalf("version=%d err=%v", version, err)
			}
			if _, err := old.Exec(`INSERT INTO messages_fts(messages_fts,rank) VALUES('integrity-check',1)`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIntegerMigrationVacuumWarning(t *testing.T) {
	old := legacyIndex(t, 5)
	old.Close()
	var steps []string
	migrateHook = func(step string) error {
		if step == "vacuum" {
			db, err := sql.Open("sqlite3", indexPath())
			if err != nil {
				return err
			}
			defer db.Close()
			var version int
			if err = db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 6 {
				return fmt.Errorf("vacuum ran before commit: %d %v", version, err)
			}
			return errors.New("vacuum unavailable")
		}
		steps = append(steps, step)
		return nil
	}
	defer func() { migrateHook = nil }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	db, openErr := openDB()
	os.Stderr = original
	w.Close()
	warning, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer db.Close()
	if !reflect.DeepEqual(steps, migrationSteps) {
		t.Fatalf("steps=%v", steps)
	}
	if strings.Count(string(warning), "\n") != 1 || !strings.Contains(string(warning), "vacuum unavailable") {
		t.Fatalf("warning=%q", warning)
	}
	checkIntegerIndex(t, db)
}

func TestIntegerMigrationOldColumnsAndRebuild(t *testing.T) {
	old := legacyIndex(t, 3)
	if _, err := old.Exec(`DROP TRIGGER messages_ai; DROP TRIGGER messages_ad; ALTER TABLE messages DROP COLUMN self; ALTER TABLE sessions DROP COLUMN automated; ALTER TABLE sessions DROP COLUMN msg_count`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	fixture := filepath.Join(os.Getenv("HOME"), "fixture")
	if out, err := exec.Command("test/fixture.sh", fixture).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", out, err)
	}
	entries, err := os.ReadDir(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(fixture, entry.Name()), filepath.Join(os.Getenv("HOME"), entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("premature version: %d %v", version, err)
	}
	stats, err := syncIndex(db, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Changed != 3 {
		t.Fatalf("forced rebuild changed %d sources", stats.Changed)
	}
	checkIntegerIndex(t, db)
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n != 15 {
		t.Fatalf("full rebuilt content=%d err=%v", n, err)
	}
}

func TestIntegerMigrationSecondProcess(t *testing.T) {
	old := legacyIndex(t, 5)
	reached, release := make(chan struct{}), make(chan struct{})
	migrateHook = func(step string) error {
		if step == "copy-messages" {
			close(reached)
			<-release
		}
		return nil
	}
	defer func() { migrateHook = nil }()
	done := make(chan error, 1)
	go func() {
		db, err := openDB()
		if db != nil {
			db.Close()
		}
		done <- err
	}()
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("migration did not reach hook: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("migration timed out")
	}
	var version int
	if err := old.QueryRow(`SELECT schema_version FROM meta`).Scan(&version); err != nil || version != 5 {
		close(release)
		<-done
		t.Fatalf("WAL reader: %d %v", version, err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegerMigrationChild$")
	cmd.Env = append(os.Environ(), "KIOKU_MIGRATION_CHILD=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		close(release)
		<-done
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		close(release)
		<-done
		t.Fatalf("second process did not wait: %v %s", err, output.String())
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatalf("second open: %v %s", err, output.String())
	}
}

func TestIntegerMigrationChild(t *testing.T) {
	if os.Getenv("KIOKU_MIGRATION_CHILD") != "1" {
		return
	}
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	checkIntegerIndex(t, db)
}

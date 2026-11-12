package main

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const hermesSchema = `CREATE TABLE sessions(id TEXT PRIMARY KEY, source TEXT, model TEXT, parent_session_id TEXT, started_at REAL, title TEXT, cwd TEXT, message_count INTEGER, last_activity_at REAL);
CREATE TABLE messages(id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, tool_name TEXT, timestamp REAL, active INTEGER DEFAULT 1, _compressed_summary INTEGER DEFAULT 0, reasoning TEXT);
INSERT INTO sessions VALUES('coding','cli','synthetic-model',NULL,1700000000.125,NULL,'/invented/tea project',0,1700000001.75);
INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(20,'coding','assistant','synthetic answer',1700000001.75),(10,'coding','user','synthetic question',1700000000.125);`

func hermesFixture(t *testing.T) (*sql.DB, *sql.DB, string) {
	t.Helper()
	home := doctorHome(t)
	t.Setenv("KIOKU_INDEX", filepath.Join(home, "index.db"))
	t.Setenv("HERMES_HOME", "")
	path := filepath.Join(home, ".hermes", "state.db")
	t.Setenv("KIOKU_HERMES_DB", path)
	src := doctorDB(t, path, hermesSchema)
	t.Cleanup(func() { src.Close() })
	idx, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return src, idx, path
}

func hermesExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func hermesSync(t *testing.T, db *sql.DB, rebuild bool) syncStats {
	t.Helper()
	s, err := syncIndex(db, rebuild, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hermesCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sessions WHERE harness='hermes'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHermesCodingSession(t *testing.T) {
	src, idx, path := hermesFixture(t)
	hermesExec(t, src, `INSERT INTO messages(id,session_id,role,content,tool_name,timestamp) VALUES(30,'coding','tool','synthetic result','bash',1700000002.5),(40,'coding','reasoning','private reasoning',NULL,1700000003);
INSERT INTO sessions VALUES('gateway','telegram',NULL,NULL,0,NULL,'',0,0),('scheduled','cron',NULL,NULL,0,NULL,NULL,0,0),('child','cli',NULL,'coding',0,NULL,'/invented/child',0,0),('empty-parent','cli',NULL,'',0,NULL,'/invented/child',0,0);
INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(50,'gateway','user','gateway text',0),(60,'scheduled','user','scheduled text',0),(70,'child','user','child text',0),(80,'empty-parent','user','child text',0);`)
	if s := hermesSync(t, idx, false); s.Changed != 1 {
		t.Fatalf("stats: %+v", s)
	}
	if n := hermesCount(t, idx); n != 1 {
		t.Fatalf("sessions = %d", n)
	}
	var uid, id, cwd, project, model, started, updated string
	if err := idx.QueryRow("SELECT uid,native_id,cwd,project,model,started,updated FROM sessions WHERE harness='hermes'").Scan(&uid, &id, &cwd, &project, &model, &started, &updated); err != nil {
		t.Fatal(err)
	}
	if uid != "hermes:"+path+"#coding" || id != "coding" || cwd != "/invented/tea project" || project != "tea project" || model != "synthetic-model" || started != "2023-11-14T22:13:20.125Z" || updated != "2023-11-14T22:13:22.5Z" {
		t.Fatalf("metadata: %q", []string{uid, id, cwd, project, model, started, updated})
	}
	rows, err := idx.Query("SELECT role,text,ts FROM messages ORDER BY idx")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var r [3]string
		if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := [][3]string{{"user", "synthetic question", "2023-11-14T22:13:20.125Z"}, {"asst", "synthetic answer", "2023-11-14T22:13:21.75Z"}, {"tool", "bash · synthetic result", "2023-11-14T22:13:22.5Z"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages: %#v", got)
	}
	page := roleCLI(t, "--harness", "hermes", "--role", "tool", "--all-time", "synthetic")
	if page.Total != 1 || len(page.Hits) != 1 || page.Hits[0].Role != "tool" {
		t.Fatalf("tool filter: %+v", page)
	}
	var conversation, tools int
	if err := idx.QueryRow("SELECT count(CASE WHEN role IN ('user','asst') THEN 1 END),count(CASE WHEN role='tool' THEN 1 END) FROM messages").Scan(&conversation, &tools); err != nil || conversation != 2 || tools != 1 {
		t.Fatalf("roles: %d %d %v", conversation, tools, err)
	}
}

func TestHermesSkippedRowsAndNulls(t *testing.T) {
	src, idx, _ := hermesFixture(t)
	hermesExec(t, src, `UPDATE sessions SET model=NULL,title=NULL;
INSERT INTO messages(id,session_id,role,content,timestamp,active,_compressed_summary) VALUES(30,'coding','user','undone',0,0,0),(40,'coding','assistant','summary',0,1,1),(50,'coding','assistant',NULL,0,1,0),(60,'coding','user','',0,1,0),(70,'coding','reasoning','reason',0,1,0);`)
	hermesSync(t, idx, false)
	var n int
	var model string
	if err := idx.QueryRow("SELECT msg_count,model FROM sessions WHERE harness='hermes'").Scan(&n, &model); err != nil || n != 2 || model != "" {
		t.Fatalf("messages=%d model=%q err=%v", n, model, err)
	}
}

func TestHermesChangeDetectionAndDeletion(t *testing.T) {
	src, idx, _ := hermesFixture(t)
	hermesSync(t, idx, false)
	hermesExec(t, idx, `CREATE TRIGGER hermes_no_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'unchanged session rewritten'); END;
CREATE TRIGGER hermes_no_update BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'unchanged session updated'); END;`)
	if s := hermesSync(t, idx, false); s.Changed != 0 {
		t.Fatalf("unchanged: %+v", s)
	}
	hermesExec(t, idx, "DROP TRIGGER hermes_no_delete; DROP TRIGGER hermes_no_update;")
	// Session metadata deliberately stays stale: count and newest id come from messages.
	hermesExec(t, src, `INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(30,'coding','user','appended',1700000004.25)`)
	if s := hermesSync(t, idx, false); s.Changed != 1 {
		t.Fatalf("append: %+v", s)
	}
	var n int
	if err := idx.QueryRow("SELECT count(*) FROM messages").Scan(&n); err != nil || n != 3 {
		t.Fatalf("append count=%d err=%v", n, err)
	}
	// Equal count, different newest id must also replace the whole session.
	hermesExec(t, src, `DELETE FROM messages WHERE id=30; INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(40,'coding','user','replacement',1700000004.5)`)
	if s := hermesSync(t, idx, false); s.Changed != 1 {
		t.Fatalf("newest id: %+v", s)
	}
	hermesExec(t, src, "DELETE FROM messages WHERE id=40")
	if s := hermesSync(t, idx, false); s.Changed != 1 {
		t.Fatalf("count: %+v", s)
	}
	hermesExec(t, src, "DELETE FROM sessions")
	hermesSync(t, idx, false)
	if n := hermesCount(t, idx); n != 0 {
		t.Fatalf("deleted session remains: %d", n)
	}
}

func TestHermesOldSchema(t *testing.T) {
	for _, cwd := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing cwd", true: "missing message flags"}[cwd], func(t *testing.T) {
			_, idx, path := hermesFixture(t)
			old := filepath.Join(filepath.Dir(path), "old.db")
			column, value := "", ""
			if cwd {
				column = ",cwd TEXT"
				value = ",'/invented/old'"
			}
			src := doctorDB(t, old, `CREATE TABLE sessions(id TEXT,model TEXT,parent_session_id TEXT,started_at REAL`+column+`); CREATE TABLE messages(id INTEGER,session_id TEXT,role TEXT,content TEXT,tool_name TEXT,timestamp REAL); INSERT INTO sessions VALUES('old',NULL,NULL,1700000000.125`+value+`); INSERT INTO messages VALUES(1,'old','user','old schema',NULL,1700000000.125);`)
			defer src.Close()
			t.Setenv("KIOKU_HERMES_DB", old)
			s := hermesSync(t, idx, false)
			want := 0
			if cwd {
				want = 1
			}
			if s.Skipped != 0 || hermesCount(t, idx) != want {
				t.Fatalf("old schema: %+v count=%d", s, hermesCount(t, idx))
			}
		})
	}
}

type hermesFingerprint struct {
	data  []byte
	mtime time.Time
}

func hermesFiles(t *testing.T, path string) map[string]hermesFingerprint {
	t.Helper()
	out := map[string]hermesFingerprint{}
	for _, p := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = hermesFingerprint{data, info.ModTime()}
	}
	return out
}

func TestHermesWALReadOnly(t *testing.T) {
	src, idx, path := hermesFixture(t)
	hermesExec(t, src, "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;")
	mainBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hermesExec(t, src, `INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(30,'coding','user','wal-only',1700000005.5)`)
	mainAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mainBefore, mainAfter) {
		t.Fatal("fixture checkpointed before sync")
	}
	before := hermesFiles(t, path)
	if len(before[path+"-wal"].data) == 0 {
		t.Fatal("fixture has no live WAL")
	}
	hermesSync(t, idx, false)
	if !reflect.DeepEqual(before, hermesFiles(t, path)) {
		t.Fatal("source bytes or mtime changed")
	}
	var n int
	if err := idx.QueryRow("SELECT count(*) FROM messages WHERE text='wal-only'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("WAL visibility: %d %v", n, err)
	}
	// An open snapshot must not block a Hermes writer in WAL mode.
	store, err := loadHermes()
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	hermesExec(t, src, `INSERT INTO messages(id,session_id,role,content,timestamp) VALUES(40,'coding','user','concurrent writer',1700000006)`)
	if _, err := store.tx.Exec("DELETE FROM messages"); err == nil {
		t.Fatal("source connection is writable")
	}
}

func TestHermesFailedLoadRetainsCache(t *testing.T) {
	for _, kind := range []string{"corrupt", "locked"} {
		t.Run(kind, func(t *testing.T) {
			src, idx, path := hermesFixture(t)
			hermesSync(t, idx, false)
			if kind == "corrupt" {
				t.Setenv("KIOKU_HERMES_DB", path+".bad")
				if err := os.WriteFile(path+".bad", []byte("not sqlite"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				hermesExec(t, src, "BEGIN EXCLUSIVE")
				defer src.Exec("ROLLBACK")
			}
			for _, rebuild := range []bool{false, true} {
				other := filepath.Join(roots()["claude"], "project", "other.jsonl")
				doctorFile(t, other)
				if err := os.WriteFile(other, []byte(`{"type":"user","sessionId":"coding","cwd":"/invented/other","message":{"content":"other harness survives"}}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				s := hermesSync(t, idx, rebuild)
				if hermesCount(t, idx) != 1 || len(s.Warnings) == 0 || !strings.HasPrefix(s.Warnings[0], "hermes:") {
					t.Fatalf("cache lost: %+v", s)
				}
				var n int
				if err := idx.QueryRow("SELECT count(*) FROM sessions WHERE harness='claude'").Scan(&n); err != nil || n != 1 {
					t.Fatalf("other harness blocked: %d %v", n, err)
				}
			}
		})
	}
}

func TestHermesResumeAndDoctor(t *testing.T) {
	if os.Getenv("A5_HERMES_RESUME_HELPER") == "1" {
		if err := execSession(hit{Harness: "hermes", SessionID: "coding", CWD: os.Getenv("A5_HERMES_RESUME_CWD")}); err != nil {
			t.Fatal(err)
		}
		return
	}
	src, idx, _ := hermesFixture(t)
	dir := filepath.Join(t.TempDir(), "resume project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	dir = resolved
	if _, err := src.Exec("UPDATE sessions SET cwd=?", dir); err != nil {
		t.Fatal(err)
	}
	hermesSync(t, idx, false)
	var cwd string
	if err := idx.QueryRow("SELECT cwd FROM sessions WHERE harness='hermes'").Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	argv := resumeArgv("hermes", "coding", "", cwd)
	if !reflect.DeepEqual(argv, []string{"hermes", "--resume", "coding"}) || cwd != dir {
		t.Fatalf("resume: %q in %q", argv, cwd)
	}
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "hermes"), []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestHermesResumeAndDoctor$")
	cmd.Env = append(os.Environ(), "A5_HERMES_RESUME_HELPER=1", "A5_HERMES_RESUME_CWD="+dir, "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != dir+"\n--resume\ncoding\n" {
		t.Fatalf("execSession: %q %v", got, err)
	}
	checks, err := doctorJSON(t)
	if err != nil || !checks["Hermes Agent"].OK || !strings.Contains(checks["Hermes Agent"].Detail, "1 sessions") {
		t.Fatalf("doctor: %+v %v", checks["Hermes Agent"], err)
	}
}

func TestHermesDoctorMissingTables(t *testing.T) {
	_, _, path := hermesFixture(t)
	bad := path + ".empty"
	db := doctorDB(t, bad, "CREATE TABLE unrelated(id TEXT)")
	defer db.Close()
	t.Setenv("KIOKU_HERMES_DB", bad)
	checks, err := doctorJSON(t)
	if err == nil || checks["Hermes Agent"].OK {
		t.Fatalf("invalid Hermes schema reported healthy: %+v, %v", checks["Hermes Agent"], err)
	}
}

func TestHermesPathPrecedence(t *testing.T) {
	_, idx, path := hermesFixture(t)
	t.Setenv("KIOKU_HERMES_DB", "")
	hermesSync(t, idx, false)
	if hermesCount(t, idx) != 1 {
		t.Fatal("default home not found")
	}
	home := filepath.Join(t.TempDir(), "hermes-home")
	src := doctorDB(t, filepath.Join(home, "state.db"), strings.ReplaceAll(hermesSchema, "'coding'", "'alternate'"))
	defer src.Close()
	t.Setenv("HERMES_HOME", home)
	hermesSync(t, idx, false)
	var id string
	if err := idx.QueryRow("SELECT native_id FROM sessions WHERE harness='hermes'").Scan(&id); err != nil || id != "alternate" {
		t.Fatalf("HERMES_HOME: %q %v", id, err)
	}
	t.Setenv("KIOKU_HERMES_DB", path)
	hermesSync(t, idx, false)
	if err := idx.QueryRow("SELECT native_id FROM sessions WHERE harness='hermes'").Scan(&id); err != nil || id != "coding" {
		t.Fatalf("explicit path: %q %v", id, err)
	}
	t.Setenv("KIOKU_HERMES_DB", "~/.hermes/state.db")
	hermesSync(t, idx, false)
	if hermesCount(t, idx) != 1 {
		t.Fatal("tilde override not expanded")
	}
	t.Setenv("KIOKU_HERMES_DB", path+".missing")
	hermesSync(t, idx, false)
	if hermesCount(t, idx) != 0 {
		t.Fatal("missing explicit path fell back")
	}
	t.Setenv("KIOKU_HERMES_DB", "")
	t.Setenv("HERMES_HOME", home+".missing")
	hermesSync(t, idx, false)
	if hermesCount(t, idx) != 0 {
		t.Fatal("missing HERMES_HOME fell back")
	}
}

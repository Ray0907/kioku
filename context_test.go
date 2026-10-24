package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctxNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func ctxDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("KIOKU_INDEX", filepath.Join(t.TempDir(), "index.db"))
	db, e := openDB()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// addSession inserts a session whose messages are (role, text) pairs, one minute apart ending at ago before ctxNow.
func addSession(t *testing.T, db *sql.DB, uid, harness, cwd string, ago time.Duration, msgs ...string) {
	t.Helper()
	end := ctxNow.Add(-ago)
	ts := func(i int) string {
		return end.Add(-time.Duration(len(msgs)/2-i) * time.Minute).Format(time.RFC3339Nano)
	}
	if _, e := db.Exec(`INSERT INTO sessions(uid,harness,native_id,path,cwd,project,model,started,updated,msg_count) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		uid, harness, uid, "/p/"+uid, cwd, filepath.Base(cwd), "", ts(0), end.Format(time.RFC3339Nano), len(msgs)/2); e != nil {
		t.Fatal(e)
	}
	for i := 0; i+1 < len(msgs); i += 2 {
		if _, e := db.Exec(`INSERT INTO messages(session_uid,idx,ts,role,text,self) VALUES(?,?,?,?,?,0)`, uid, i/2, ts(i/2), msgs[i], msgs[i+1]); e != nil {
			t.Fatal(e)
		}
	}
}

func TestContextEmptyWhenNothingRecent(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "old", "claude", "/w/app", 20*24*time.Hour, "user", "fix login")
	addSession(t, db, "other", "claude", "/w/other", time.Hour, "user", "unrelated")
	got, e := contextText(context.Background(), db, "/w/app", ctxNow)
	if e != nil || got != "" {
		t.Fatalf("want empty, got %q err=%v", got, e)
	}
}

func TestContextShowsLastAskAndReply(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "a", "claude", "/w/app", 2*time.Hour,
		"user", "fix the login redirect", "asst", "looking", "user", "push it", "asst", "Pushed to main.\nMore detail here.")
	got, e := contextText(context.Background(), db, "/w/app", ctxNow)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{`"fix the login redirect"`, `last asked: "push it"`, `last reply: "Pushed to main."`, "2h", "claude"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "More detail") {
		t.Error("reply must be first line only")
	}
}

func TestContextSubfolderAndHome(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "a", "codex", "/w/app", time.Hour, "user", "q")
	if got, _ := contextText(context.Background(), db, "/w/app/server", ctxNow); !strings.Contains(got, "codex") {
		t.Fatalf("subfolder must count as its project, got %q", got)
	}
	t.Setenv("HOME", "/w")
	if got, _ := contextText(context.Background(), db, "/w", ctxNow); got != "" {
		t.Fatalf("home must never be a project, got %q", got)
	}
}

func TestContextSkipsSessionsWithoutQuestionAndKeepsEveryHarness(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "noq", "claude", "/w/app", time.Minute, "asst", "only a reply")
	for i := 0; i < 5; i++ {
		addSession(t, db, "c"+string(rune('a'+i)), "claude", "/w/app", time.Duration(i+1)*time.Hour, "user", "claude q")
	}
	addSession(t, db, "x", "codex", "/w/app", 9*24*time.Hour, "user", "codex q")
	got, _ := contextText(context.Background(), db, "/w/app", ctxNow)
	if strings.Contains(got, "only a reply") {
		t.Error("session without a question must be skipped")
	}
	if !strings.Contains(got, "codex q") {
		t.Errorf("each harness gets its latest session in:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != 5 {
		t.Errorf("want 5 sessions, got %d:\n%s", n, got)
	}
}

func TestContextSkipsReviewerSessions(t *testing.T) {
	db := ctxDB(t)
	addSession(t, db, "r1", "claude", "/w/app", time.Minute, "user", "Review this change for security vulnerabilities. Changed files: a.go")
	addSession(t, db, "r2", "pi", "/w/app", 2*time.Minute, "user", "# Review Guidelines\nYou are acting as a code reviewer")
	addSession(t, db, "real", "claude", "/w/app", time.Hour, "user", "review the login page copy")
	got, _ := contextText(context.Background(), db, "/w/app", ctxNow)
	if strings.Contains(got, "security vulnerabilities") || strings.Contains(got, "Review Guidelines") {
		t.Errorf("automated reviewer sessions must be skipped:\n%s", got)
	}
	if !strings.Contains(got, "review the login page copy") {
		t.Errorf("a person's own review request must stay:\n%s", got)
	}
}

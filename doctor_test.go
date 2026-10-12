package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

type doctorResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
}

func doctorHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, env := range []string{"KIOKU_INDEX", "KIOKU_CLAUDE_DIR", "KIOKU_CODEX_DIR", "KIOKU_PI_DIR", "KIOKU_GROK_DIR", "KIOKU_OPENCODE_DB", "KIOKU_CURSOR_DIR", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "PI_CODING_AGENT_DIR", "XDG_CACHE_HOME", "XDG_DATA_HOME", "KIOKU_DEBUG_TIMING"} {
		t.Setenv(env, "")
	}
	return home
}

func doctorRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	oldArgs, oldOutput := os.Args, output
	defer func() { os.Args, output = oldArgs, oldOutput }()
	var b bytes.Buffer
	os.Args, output = append([]string{"kioku", "doctor"}, args...), &b
	err := run()
	return b.String(), err
}

func doctorJSON(t *testing.T) (map[string]doctorResult, error) {
	t.Helper()
	out, err := doctorRun(t, "--json")
	var checks []doctorResult
	if e := json.Unmarshal([]byte(out), &checks); e != nil {
		t.Fatalf("expected JSON array, got %q: %v", out, e)
	}
	got := map[string]doctorResult{}
	for _, c := range checks {
		if _, exists := got[c.Name]; exists {
			t.Fatalf("duplicate check %q", c.Name)
		}
		got[c.Name] = c
	}
	if len(got) != 8 {
		t.Fatalf("expected six sources, index and sqlite; got %v", got)
	}
	if c := got["sqlite"]; !c.OK || c.Optional || !strings.Contains(c.Detail, "FTS5") || !strings.Contains(c.Detail, "cjk") {
		t.Fatalf("sqlite probe failed: %+v", c)
	}
	return got, err
}

func doctorFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func doctorDB(t *testing.T, path, schema string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDoctorSources(t *testing.T) {
	for _, tc := range []struct {
		name, harness, label, file string
		empty, missing             bool
	}{
		{name: "missing source", harness: "claude", label: "Claude Code", missing: true},
		{name: "empty source", harness: "claude", label: "Claude Code", empty: true},
		{name: "present claude", harness: "claude", label: "Claude Code", file: "project/session.jsonl"},
		{name: "present codex", harness: "codex", label: "Codex", file: "2026/10/rollout.jsonl"},
		{name: "present pi", harness: "pi", label: "Pi", file: "project/session.jsonl"},
		{name: "present grok", harness: "grok", label: "Grok", file: "project/session/chat_history.jsonl"},
		{name: "present cursor", harness: "cursor", label: "Cursor CLI", file: "project/session/meta.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorHome(t)
			root := roots()[tc.harness]
			if !tc.missing {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				doctorFile(t, filepath.Join(root, "ignored.txt"))
				if !tc.empty {
					doctorFile(t, filepath.Join(root, tc.file))
				}
			}
			got, err := doctorJSON(t)
			c := got[tc.label]
			if c.OK == tc.missing || c.Optional != tc.missing {
				t.Fatalf("source status: %+v", c)
			}
			if tc.missing {
				if !strings.HasPrefix(c.Detail, "not found (optional:") || !strings.Contains(c.Detail, root) {
					t.Fatalf("missing source detail: %+v", c)
				}
			} else {
				count := 1
				if tc.empty {
					count = 0
				}
				if !strings.Contains(c.Detail, root) || !strings.Contains(c.Detail, fmt.Sprintf("%d session files", count)) {
					t.Fatalf("source count: %+v", c)
				}
			}
			present := !tc.missing && !tc.empty
			if (err != nil) != present || got["index"].Optional == present {
				t.Fatalf("missing index status: %+v, err=%v", got["index"], err)
			}
			if present && !strings.Contains(got["index"].Detail, "run kioku") {
				t.Fatalf("missing index should suggest running kioku: %+v", got["index"])
			}
			if _, err := os.Stat(filepath.Dir(indexPath())); !os.IsNotExist(err) {
				t.Fatalf("doctor created index directory: %v", err)
			}
		})
	}
}

func TestDoctorIndex(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		ok           bool
	}{
		{"valid", `CREATE TABLE meta(schema_version INTEGER); INSERT INTO meta VALUES(3); CREATE TABLE sessions(uid TEXT); INSERT INTO sessions VALUES('a'),('b'); CREATE TABLE messages(id INTEGER); INSERT INTO messages VALUES(1),(2),(3);`, true},
		{"invalid schema", `CREATE TABLE unrelated(id INTEGER);`, false},
		{"corrupt", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorHome(t)
			path := filepath.Join(t.TempDir(), "index ?#.db")
			t.Setenv("KIOKU_INDEX", path)
			if tc.schema == "" {
				doctorFile(t, path)
			} else {
				db := doctorDB(t, path, tc.schema)
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			before := doctorSnapshot(t, filepath.Dir(path))
			info, _ := os.Stat(path)
			got, err := doctorJSON(t)
			c := got["index"]
			if c.OK != tc.ok || c.Optional || (err == nil) != tc.ok || !strings.Contains(c.Detail, path) {
				t.Fatalf("index status: %+v, err=%v", c, err)
			}
			if tc.ok {
				for _, detail := range []string{fmt.Sprintf("%d bytes", info.Size()), "schema_version=3", "2 sessions", "3 messages"} {
					if !strings.Contains(c.Detail, detail) {
						t.Errorf("missing %q: %s", detail, c.Detail)
					}
				}
			}
			if after := doctorSnapshot(t, filepath.Dir(path)); !reflect.DeepEqual(before, after) {
				t.Fatal("doctor changed the index or its sidecars")
			}
		})
	}
}

func doctorSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		out[path] = fmt.Sprintf("%v %v %x", info.Mode(), info.ModTime(), data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDoctorWALReadOnly(t *testing.T) {
	for _, source := range []string{"index", "opencode"} {
		t.Run(source, func(t *testing.T) {
			doctorHome(t)
			path := filepath.Join(t.TempDir(), "live.db")
			schema := `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE meta(schema_version INTEGER); INSERT INTO meta VALUES(1); CREATE TABLE sessions(uid TEXT); INSERT INTO sessions VALUES('a'); CREATE TABLE messages(id INTEGER); INSERT INTO messages VALUES(1);`
			env, label, count := "KIOKU_INDEX", "index", "1 sessions"
			if source == "opencode" {
				env, label, count = "KIOKU_OPENCODE_DB", "OpenCode", "2 sessions"
				schema = `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE session(id TEXT, parent_id TEXT); INSERT INTO session VALUES('a',NULL),('b',NULL),('child','a'); CREATE TABLE session_v2(id TEXT, parent_id TEXT); INSERT INTO session_v2 VALUES('a',NULL),('child','a');`
			}
			doctorDB(t, path, schema)
			t.Setenv(env, path)
			before := doctorSnapshot(t, filepath.Dir(path))
			got, err := doctorJSON(t)
			if c := got[label]; !c.OK || !strings.Contains(c.Detail, count) {
				t.Fatalf("must read committed WAL data: %+v, err=%v", c, err)
			}
			if (err != nil) != (source == "opencode") {
				t.Fatalf("missing index with DB sessions: %v", err)
			}
			after := doctorSnapshot(t, filepath.Dir(path))
			if source == "index" {
				delete(before, path+"-shm")
				delete(after, path+"-shm")
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("doctor changed database data or harness sidecars")
			}
		})
	}
}

func TestDoctorIndexReadOnly(t *testing.T) {
	doctorHome(t)
	path := indexPath()
	db := doctorDB(t, path, `CREATE TABLE messages(id INTEGER);`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before := doctorSnapshot(t, filepath.Dir(path))
	reader, err := doctorOpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var queryOnly int
	if err = reader.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil || queryOnly != 1 {
		t.Fatalf("query_only=%d, err=%v", queryOnly, err)
	}
	if _, err = reader.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Exec("INSERT INTO messages VALUES(1)"); err == nil {
		t.Fatal("mode=ro allowed a data write")
	}
	if after := doctorSnapshot(t, filepath.Dir(path)); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only connection changed the database")
	}
}

func TestDoctorFingerprint(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		for _, change := range []string{"size", "mtime", "create", "remove"} {
			t.Run(suffix+"/"+change, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "store.db")
				file := path + suffix
				if change != "create" {
					doctorFile(t, file)
				}
				before, err := doctorFingerprint(path)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "size":
					err = os.WriteFile(file, []byte("more data\n"), 0600)
				case "mtime":
					stamp := time.Unix(123, 0)
					err = os.Chtimes(file, stamp, stamp)
				case "create":
					doctorFile(t, file)
				case "remove":
					err = os.Remove(file)
				}
				if err != nil {
					t.Fatal(err)
				}
				after, err := doctorFingerprint(path)
				if err != nil || before == after {
					t.Fatalf("missed %s change to %s: before=%v, after=%v, err=%v", change, file, before, after, err)
				}
			})
		}
	}
}

func TestDoctorCopyFailure(t *testing.T) {
	for _, suffix := range []string{"", "-wal"} {
		t.Run("db"+suffix, func(t *testing.T) {
			doctorHome(t)
			path := filepath.Join(t.TempDir(), "store.db")
			t.Setenv("KIOKU_OPENCODE_DB", path)
			doctorDB(t, path, `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE session(id TEXT, parent_id TEXT); INSERT INTO session VALUES('a',NULL);`)
			copyFile := doctorCopy
			t.Cleanup(func() { doctorCopy = copyFile })
			failures := 0
			doctorCopy = func(src, dst string) error {
				if src != path+suffix {
					return copyFile(src, dst)
				}
				failures++
				stamp := time.Unix(int64(failures), 0)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				return &os.PathError{Op: "write", Path: dst, Err: syscall.ENOSPC}
			}
			got, err := doctorJSON(t)
			c := got["OpenCode"]
			if err == nil || c.OK || c.Optional || !strings.Contains(c.Detail, syscall.ENOSPC.Error()) {
				t.Errorf("copy failure hidden by changing fingerprint: %+v, err=%v", c, err)
			}
			if failures != 1 {
				t.Errorf("copy error retried %d times, want one attempt", failures)
			}
		})
	}
}

func TestDoctorConcurrentWriters(t *testing.T) {
	for _, source := range []string{"index", "opencode"} {
		t.Run(source, func(t *testing.T) {
			doctorHome(t)
			path := filepath.Join(t.TempDir(), "live.db")
			schema := `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; PRAGMA busy_timeout=5000;
CREATE TABLE meta(schema_version INTEGER); INSERT INTO meta VALUES(3);
CREATE TABLE sessions(uid INTEGER);
CREATE TABLE filler(data BLOB); INSERT INTO filler VALUES(zeroblob(64*1024*1024));
CREATE TABLE messages(id INTEGER); PRAGMA wal_checkpoint(TRUNCATE);`
			label := "index"
			t.Setenv("KIOKU_INDEX", path)
			if source == "opencode" {
				label = "OpenCode"
				t.Setenv("KIOKU_OPENCODE_DB", path)
				t.Setenv("KIOKU_INDEX", filepath.Join(t.TempDir(), "index.db"))
				doctorDB(t, indexPath(), `CREATE TABLE meta(schema_version INTEGER); INSERT INTO meta VALUES(3); CREATE TABLE sessions(uid TEXT); CREATE TABLE messages(id INTEGER);`)
				schema = `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; PRAGMA busy_timeout=5000;
CREATE TABLE session(id TEXT, parent_id TEXT); INSERT INTO session VALUES('v1',NULL);
CREATE TABLE filler(data BLOB); INSERT INTO filler VALUES(zeroblob(64*1024*1024));
CREATE TABLE session_v2(id TEXT, parent_id TEXT); PRAGMA wal_checkpoint(TRUNCATE);`
			}
			db := doctorDB(t, path, schema)
			db.SetMaxOpenConns(1)
			stop, ready, done := make(chan struct{}), make(chan error, 1), make(chan error, 1)
			go func() {
				for i := 1; ; i++ {
					select {
					case <-stop:
						done <- nil
						return
					default:
					}
					q := fmt.Sprintf(`BEGIN; INSERT INTO sessions VALUES(%d); INSERT INTO messages VALUES(%d); COMMIT; PRAGMA wal_checkpoint(TRUNCATE);`, i, i)
					if source == "opencode" {
						table := "session"
						if i%2 == 1 {
							table = "session_v2"
						}
						q = fmt.Sprintf(`BEGIN; DELETE FROM session; DELETE FROM session_v2; INSERT INTO %s VALUES('%s',NULL); COMMIT; PRAGMA wal_checkpoint(TRUNCATE);`, table, table)
					}
					_, err := db.Exec(q)
					if i == 1 {
						ready <- err
					}
					if err != nil {
						done <- err
						return
					}
				}
			}()
			stopped := false
			stopWriter := func() {
				if !stopped {
					close(stop)
					stopped = true
					if err := <-done; err != nil {
						t.Errorf("writer failed: %v", err)
					}
				}
			}
			t.Cleanup(stopWriter)
			if err := <-ready; err != nil {
				t.Fatal(err)
			}
			counts := regexp.MustCompile(`schema_version=3, (\d+) sessions, (\d+) messages`)
			busy := 0
			for i := 0; i < 8; i++ {
				got, err := doctorJSON(t)
				c := got[label]
				if err != nil || !c.OK || c.Optional {
					t.Errorf("healthy database reported a failure: %+v, err=%v", c, err)
					continue
				}
				if source == "index" {
					match := counts.FindStringSubmatch(c.Detail)
					if len(match) != 3 || match[1] != match[2] {
						t.Errorf("counts never existed in a committed state: %s", c.Detail)
					}
				} else if strings.Contains(c.Detail, "busy, rerun when the agent is idle") {
					busy++
					if strings.Contains(c.Detail, " sessions") {
						t.Errorf("busy source reported unverified counts: %s", c.Detail)
					}
				} else if !strings.HasSuffix(c.Detail, ", 1 sessions") {
					t.Errorf("source count never existed in a committed state: %s", c.Detail)
				}
			}
			stopWriter()
			if source == "opencode" && busy == 0 {
				t.Error("concurrent writer did not exercise the non-failing busy result")
			}
			var integrity string
			if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("source is not healthy: %q, err=%v", integrity, err)
			}
			got, err := doctorJSON(t)
			if err != nil || !got[label].OK || strings.Contains(got[label].Detail, "busy") {
				t.Fatalf("idle database should report counts: %+v, err=%v", got[label], err)
			}
		})
	}
}

func TestDoctorOutput(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", jsonMode), func(t *testing.T) {
			doctorHome(t)
			args := []string{}
			if jsonMode {
				args = append(args, "--json")
			}
			out, err := doctorRun(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "\x1b") {
				t.Fatalf("unexpected ANSI in non-TTY/JSON output: %q", out)
			}
			if jsonMode {
				var checks []map[string]any
				if err := json.Unmarshal([]byte(out), &checks); err != nil || len(checks) != 8 {
					t.Fatalf("JSON shape: %q, err=%v", out, err)
				}
				for _, c := range checks {
					_, nameOK := c["name"].(string)
					_, okOK := c["ok"].(bool)
					_, optionalOK := c["optional"].(bool)
					_, detailOK := c["detail"].(string)
					if len(c) != 4 || !nameOK || !okOK || !optionalOK || !detailOK {
						t.Fatalf("wrong check shape: %v", c)
					}
				}
			} else {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				if len(lines) != 8 || !strings.HasPrefix(lines[0], "- Claude Code  not found (optional:") || !strings.HasPrefix(lines[7], "✓ sqlite  ") {
					t.Fatalf("text shape: %q", out)
				}
			}
		})
	}
}

func TestDoctorExitCode(t *testing.T) {
	if os.Getenv("KIOKU_TEST_DOCTOR_MAIN") == "1" {
		os.Args = []string{"kioku", "doctor", "--json"}
		main()
		os.Exit(0)
	}
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("sources=%v", present), func(t *testing.T) {
			doctorHome(t)
			if present {
				doctorFile(t, filepath.Join(roots()["pi"], "session.jsonl"))
			}
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "-test.run=^TestDoctorExitCode$")
			cmd.Env = append(os.Environ(), "KIOKU_TEST_DOCTOR_MAIN=1")
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err = cmd.Run()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			want := 0
			if present {
				want = 1
			}
			if code != want {
				t.Fatalf("exit code = %d, want %d; stdout=%s", code, want, stdout.String())
			}
			if !strings.HasPrefix(stdout.String(), "[") {
				t.Fatalf("doctor did not emit checks: %s", stdout.String())
			}
		})
	}
}

func TestDoctorRequiredFailures(t *testing.T) {
	for _, tc := range []struct{ name, label string }{
		{"source is a file", "Claude Code"},
		{"missing index with sessions", "index"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorHome(t)
			path := roots()["claude"]
			if tc.label == "index" {
				path = filepath.Join(path, "session.jsonl")
			}
			doctorFile(t, path)
			before := doctorSnapshot(t, os.Getenv("HOME"))
			out, err := doctorRun(t)
			if err == nil || !strings.Contains(out, "✗ "+tc.label+"  ") || strings.Contains(out, "\x1b") {
				t.Fatalf("required failure output: %q, err=%v", out, err)
			}
			if after := doctorSnapshot(t, os.Getenv("HOME")); !reflect.DeepEqual(before, after) {
				t.Fatal("doctor changed a source or created an index")
			}
		})
	}
}

func TestDoctorOpencode(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		count        int
		ok           bool
	}{
		{"v1", `CREATE TABLE session(id TEXT, parent_id TEXT); INSERT INTO session VALUES('a',NULL),('child','a');`, 1, true},
		{"v2", `CREATE TABLE session_v2(id TEXT, parent_id TEXT); INSERT INTO session_v2 VALUES('a',''),('child','a');`, 1, true},
		{"empty", `CREATE TABLE session(id TEXT, parent_id TEXT);`, 0, true},
		{"invalid", `CREATE TABLE unrelated(id TEXT);`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := doctorHome(t)
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
			path := opencodePath()
			db := doctorDB(t, path, tc.schema)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before := doctorSnapshot(t, home)
			got, err := doctorJSON(t)
			c := got["OpenCode"]
			if c.OK != tc.ok || c.Optional || !strings.Contains(c.Detail, fmt.Sprintf("%d sessions", tc.count)) {
				t.Fatalf("OpenCode count/status: %+v", c)
			}
			if (err != nil) != (!tc.ok || tc.count > 0) {
				t.Fatalf("exit status: %v", err)
			}
			if after := doctorSnapshot(t, home); !reflect.DeepEqual(before, after) {
				t.Fatal("doctor changed OpenCode or created an index")
			}
		})
	}
}

func TestDoctorArguments(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--rebuild"}, {"--unknown"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			doctorHome(t)
			if _, err := doctorRun(t, args...); err == nil {
				t.Fatal("expected argument error")
			}
			if _, err := os.Stat(filepath.Dir(indexPath())); !os.IsNotExist(err) {
				t.Fatalf("doctor created index directory: %v", err)
			}
		})
	}
}

func TestDoctorEnvOverrides(t *testing.T) {
	for _, tc := range []struct{ harness, label, env, child string }{
		{"claude", "Claude Code", "CLAUDE_CONFIG_DIR", "projects"},
		{"codex", "Codex", "CODEX_HOME", "sessions"},
		{"pi", "Pi", "PI_CODING_AGENT_DIR", "sessions"},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			home := doctorHome(t)
			t.Setenv(tc.env, "~/native")
			doctorFile(t, filepath.Join(home, "native", tc.child, "session.jsonl"))
			got, _ := doctorJSON(t)
			if !strings.Contains(got[tc.label].Detail, "1 session files") {
				t.Fatalf("native override ignored: %+v", got[tc.label])
			}
			t.Setenv("KIOKU_"+strings.ToUpper(tc.harness)+"_DIR", "~/explicit")
			got, err := doctorJSON(t)
			if !got[tc.label].Optional || err != nil {
				t.Fatalf("missing explicit override fell back: %+v, err=%v", got[tc.label], err)
			}
		})
	}
}

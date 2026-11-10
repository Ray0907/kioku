package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func warningAgyFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	doctorHome(t)
	meta := doctorDB(t, filepath.Join(agyDir(), "conversation_summaries.db"), `CREATE TABLE conversation_summaries(conversation_id TEXT, workspace_uris TEXT, last_modified_time TEXT, step_count INTEGER, parent_conversation_id TEXT);
INSERT INTO conversation_summaries VALUES('b2-session','["file:///work/b2"]','2026-09-27T10:00:00Z',1,'');`)
	path := filepath.Join(agyDir(), "conversations", "b2-session.db")
	conv := doctorDB(t, path, `CREATE TABLE steps(idx INTEGER, step_type INTEGER, step_payload BLOB); INSERT INTO steps VALUES(0,14,X'9a010912076232746f6b656e');`)
	if err := conv.Close(); err != nil {
		t.Fatal(err)
	}
	return meta, path
}

func warningStderr(t *testing.T, f func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	old := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = old }()
	f()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func warningRun(t *testing.T, args ...string) (string, string) {
	t.Helper()
	oldArgs, oldOutput := os.Args, output
	defer func() { os.Args, output = oldArgs, oldOutput }()
	var stdout bytes.Buffer
	os.Args, output = append([]string{"kioku"}, args...), &stdout
	stderr := warningStderr(t, func() {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	})
	return stdout.String(), stderr
}

func TestSyncWarningsQuietInTUI(t *testing.T) {
	for _, source := range []string{"metadata", "agy", "opencode"} {
		t.Run(source, func(t *testing.T) {
			if source == "metadata" {
				meta, _ := warningAgyFixture(t)
				if _, err := meta.Exec(`UPDATE conversation_summaries SET workspace_uris='bad JSON'`); err != nil {
					t.Fatal(err)
				}
			} else {
				doctorHome(t)
				path := opencodePath()
				if source == "agy" {
					path = filepath.Join(agyDir(), "conversation_summaries.db")
				}
				doctorFile(t, path)
			}
			stderr := warningStderr(t, func() {
				msg := startSync(false)()
				for {
					env, ok := msg.(syncEnvelope)
					if !ok {
						t.Fatalf("unexpected TUI sync message: %T", msg)
					}
					if env.x.done {
						if env.x.err != nil {
							t.Fatal(env.x.err)
						}
						return
					}
					msg = waitSync(env.ch)()
				}
			})
			if stderr != "" {
				t.Fatalf("TUI background sync wrote stderr: %q", stderr)
			}
			_, stderr = warningRun(t, "index")
			if stderr == "" || !strings.Contains(stderr, "kioku: ") {
				t.Fatalf("index lost source warning: %q", stderr)
			}
		})
	}
}

func TestSyncWarningsIndexAndSearch(t *testing.T) {
	meta, path := warningAgyFixture(t)
	warningRun(t, "index")
	if _, err := meta.Exec(`UPDATE conversation_summaries SET workspace_uris='bad JSON'`); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"index"}, {"index", "--rebuild"}} {
		stdout, stderr := warningRun(t, args...)
		if !strings.Contains(stdout, "1 skipped") || !strings.Contains(stdout, "agy: 1 messages") || !strings.Contains(stderr, path) || !strings.Contains(stderr, "workspace_uris") {
			t.Fatalf("index warning/count/cache changed: stdout=%q stderr=%q", stdout, stderr)
		}
		stdout, stderr = warningRun(t, "--json", "--all-time", "--harness", "agy", "b2token")
		var page struct{ Total int }
		if err := json.Unmarshal([]byte(stdout), &page); err != nil || page.Total != 1 || stderr != "" {
			t.Fatalf("search must keep cached rows and stay quiet: stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
	}
}

func TestDoctorAgyWALReadOnly(t *testing.T) {
	for _, mode := range []string{"writable", "read-only without shm"} {
		for _, problem := range []string{"healthy", "metadata"} {
			t.Run(mode+"/"+problem, func(t *testing.T) {
				meta, path := warningAgyFixture(t)
				warningRun(t, "index")
				doctorDB(t, path, "PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; UPDATE steps SET idx=1;")
				if _, err := meta.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; UPDATE conversation_summaries SET step_count=2;`); err != nil {
					t.Fatal(err)
				}
				if problem == "metadata" {
					if _, err := meta.Exec(`UPDATE conversation_summaries SET workspace_uris='bad JSON'`); err != nil {
						t.Fatal(err)
					}
				}
				root := agyDir()
				if mode == "read-only without shm" {
					frozen := t.TempDir()
					conversations := filepath.Join(frozen, "conversations")
					if err := os.Mkdir(conversations, 0700); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						os.Chmod(frozen, 0700)
						os.Chmod(conversations, 0700)
					})
					for _, name := range []string{"conversation_summaries.db", filepath.Join("conversations", filepath.Base(path))} {
						for _, suffix := range []string{"", "-wal"} {
							dst := filepath.Join(frozen, name) + suffix
							if err := doctorCopy(filepath.Join(root, name)+suffix, dst); err != nil {
								t.Fatal(err)
							}
							if err := os.Chmod(dst, 0400); err != nil {
								t.Fatal(err)
							}
						}
					}
					for _, dir := range []string{conversations, frozen} {
						if err := os.Chmod(dir, 0500); err != nil {
							t.Fatal(err)
						}
					}
					t.Setenv("KIOKU_AGY_DIR", frozen)
					root = frozen
					path = filepath.Join(root, "conversations", filepath.Base(path))
				}
				before := doctorSnapshot(t, root)
				checks, err := doctorJSON(t)
				check := checks["Antigravity CLI"]
				if err != nil || !check.OK || check.Optional || !strings.Contains(check.Detail, "1 sessions") {
					t.Errorf("must read committed WAL data: %+v, err=%v", check, err)
				}
				if problem == "healthy" {
					if strings.Contains(check.Detail, "skipped") {
						t.Errorf("healthy WAL session marked skipped: %+v", check)
					}
				} else if !strings.Contains(check.Detail, "1 skipped: "+path) || !strings.Contains(check.Detail, "workspace_uris") {
					t.Errorf("doctor hid WAL metadata warning: %+v", check)
				}
				if after := doctorSnapshot(t, root); !reflect.DeepEqual(before, after) {
					t.Error("doctor changed Agy databases or harness sidecars")
				}
			})
		}
	}
}

func TestDoctorSkippedAgySessions(t *testing.T) {
	for _, problem := range []string{"healthy", "metadata", "missing", "empty"} {
		t.Run(problem, func(t *testing.T) {
			meta, path := warningAgyFixture(t)
			warningRun(t, "index")
			var err error
			switch problem {
			case "metadata":
				_, err = meta.Exec(`UPDATE conversation_summaries SET workspace_uris='bad JSON'`)
			case "missing":
				err = os.Rename(path, path+".saved")
			case "empty":
				_, err = meta.Exec(`UPDATE conversation_summaries SET step_count=0`)
			}
			if err != nil {
				t.Fatal(err)
			}
			checks, err := doctorJSON(t)
			check := checks["Antigravity CLI"]
			if err != nil || !check.OK || check.Optional || !strings.Contains(check.Detail, "1 sessions") {
				t.Fatalf("readable source must stay OK: %+v, err=%v", check, err)
			}
			if problem == "healthy" {
				if strings.Contains(check.Detail, "skipped") {
					t.Fatalf("healthy session marked skipped: %+v", check)
				}
			} else if !strings.Contains(check.Detail, "1 skipped:") || !strings.Contains(check.Detail, path) || (problem == "metadata" && !strings.Contains(check.Detail, "workspace_uris")) {
				t.Fatalf("doctor hid skipped session: %+v", check)
			}
		})
	}
}

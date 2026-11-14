package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sourceFixture(t *testing.T, kind string) (*sql.DB, *sql.DB, string) {
	t.Helper()
	if kind == "hermes" {
		return hermesFixture(t)
	}
	var src *sql.DB
	var path string
	if kind == "agy" {
		src, path = warningAgyFixture(t)
	} else {
		doctorHome(t)
		switch kind {
		case "file":
			path = filepath.Join(roots()["claude"], "project", "session.jsonl")
			doctorFile(t, path)
			if err := os.WriteFile(path, []byte(`{"type":"user","message":{"role":"user","content":"synthetic question"},"timestamp":"2026-09-27T10:00:00Z","cwd":"/work/demo","sessionId":"session"}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		case "cursor":
			path = filepath.Join(roots()["cursor"], "project", "session", "meta.json")
			doctorFile(t, path)
			if err := os.WriteFile(path, []byte(`{"cwd":"/work/demo","createdAtMs":1700000000000}`), 0600); err != nil {
				t.Fatal(err)
			}
			src = doctorDB(t, filepath.Join(filepath.Dir(path), "store.db"), `CREATE TABLE blobs(data BLOB); INSERT INTO blobs VALUES('{"role":"user","content":"synthetic question"}');`)
		case "opencode":
			path = opencodePath()
			src = doctorDB(t, path, `CREATE TABLE session(id TEXT, parent_id TEXT, directory TEXT, time_created INTEGER, time_updated INTEGER);
CREATE TABLE message(id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part(id TEXT, message_id TEXT, data TEXT);
INSERT INTO session VALUES('session',NULL,'/work/demo',1700000000000,1700000000000);
INSERT INTO message VALUES('message','session',1700000000000,'{"role":"user"}');
INSERT INTO part VALUES('part','message','{"type":"text","text":"synthetic question"}');`)
		default:
			t.Fatalf("unknown source kind %q", kind)
		}
	}
	idx, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return src, idx, path
}

func TestSyncSourceFingerprints(t *testing.T) {
	for _, kind := range []struct {
		name    string
		changed []string
		ignored []string
	}{
		{"file", []string{"mtime", "size"}, []string{"inode", "updated"}},
		{"cursor", []string{"mtime", "size", "updated", "inode"}, []string{"offset"}},
		{"opencode", []string{"updated", "inode"}, []string{"mtime", "size"}},
		{"agy", []string{"updated", "size"}, []string{"mtime", "inode"}},
		{"hermes", []string{"updated", "size"}, []string{"mtime", "inode"}},
	} {
		t.Run(kind.name, func(t *testing.T) {
			_, idx, _ := sourceFixture(t, kind.name)
			if s := hermesSync(t, idx, false); s.Changed != 1 || s.Skipped != 0 {
				t.Fatalf("initial sync: %+v", s)
			}
			if s := hermesSync(t, idx, false); s.Changed != 0 || s.Skipped != 0 {
				t.Fatalf("unchanged sync: %+v", s)
			}
			for _, group := range []struct {
				fields  []string
				changed int
			}{{kind.changed, 1}, {kind.ignored, 0}} {
				for _, field := range group.fields {
					t.Run(field, func(t *testing.T) {
						// Perturb only the stored comparison field; leave source contents and other fields intact.
						hermesExec(t, idx, "UPDATE sources SET "+field+"="+field+"+1")
						if s := hermesSync(t, idx, false); s.Changed != group.changed || s.Skipped != 0 {
							t.Fatalf("%s: want %d changed, got %+v", field, group.changed, s)
						}
						if s := hermesSync(t, idx, false); s.Changed != 0 {
							t.Fatalf("second sync: %+v", s)
						}
					})
				}
			}
		})
	}
}

func sourceMessageIDs(t *testing.T, idx *sql.DB) []int64 {
	t.Helper()
	rows, err := idx.Query("SELECT id FROM messages ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestSyncFileAppendIDs(t *testing.T) {
	_, idx, path := sourceFixture(t, "file")
	hermesSync(t, idx, false)
	before := sourceMessageIDs(t, idx)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"synthetic answer"}]},"timestamp":"2026-09-27T10:01:00Z"}` + "\n")
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v, close: %v", err, closeErr)
	}
	if s := hermesSync(t, idx, false); s.Changed != 1 {
		t.Fatalf("append sync: %+v", s)
	}
	after := sourceMessageIDs(t, idx)
	var count int
	if err := idx.QueryRow("SELECT msg_count FROM sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || len(after) != 2 || !reflect.DeepEqual(before, after[:1]) || count != 2 {
		t.Fatalf("ids %v -> %v, msg_count=%d", before, after, count)
	}
}

func TestSyncFailedSourceRebuild(t *testing.T) {
	for _, tc := range []struct{ kind, table, warning string }{
		{"opencode", "session", ": neither session nor session_v2 table found"},
		{"agy", "conversation_summaries", "no such table: conversation_summaries"},
		{"hermes", "sessions", "sessions table not found"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			src, idx, path := sourceFixture(t, tc.kind)
			hermesSync(t, idx, false)
			before := sourceMessageIDs(t, idx)
			hermesExec(t, src, "DROP TABLE "+tc.table)
			warning := tc.warning
			if tc.kind == "opencode" {
				warning = path + warning
			}
			for _, rebuild := range []bool{false, true} {
				s := hermesSync(t, idx, rebuild)
				if s.Changed != 0 || s.Skipped != 1 || s.Files != 1 || !reflect.DeepEqual(s.Warnings, []string{tc.kind + ": " + warning}) {
					t.Fatalf("rebuild=%v: %+v", rebuild, s)
				}
				if after := sourceMessageIDs(t, idx); !reflect.DeepEqual(before, after) {
					t.Fatalf("rebuild=%v: ids %v -> %v", rebuild, before, after)
				}
				var sessions, sources, messages int
				if err := idx.QueryRow("SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM sources),(SELECT sum(msg_count) FROM sessions)").Scan(&sessions, &sources, &messages); err != nil {
					t.Fatal(err)
				}
				if sessions != 1 || sources != 1 || messages != len(before) || s.Messages[tc.kind] != len(before) {
					t.Fatalf("lost rows: sessions=%d sources=%d messages=%d stats=%+v", sessions, sources, messages, s)
				}
			}
		})
	}
}

func TestSyncCursorMissingStore(t *testing.T) {
	src, idx, path := sourceFixture(t, "cursor")
	hermesSync(t, idx, false)
	before := sourceMessageIDs(t, idx)
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(filepath.Dir(path), "store.db")
	if err := os.Rename(store, store+".saved"); err != nil {
		t.Fatal(err)
	}
	if s := hermesSync(t, idx, false); s.Changed != 0 || s.Skipped != 1 || s.Files != 1 {
		t.Fatalf("missing store: %+v", s)
	}
	if ids := sourceMessageIDs(t, idx); !reflect.DeepEqual(ids, before) {
		t.Fatalf("missing store changed cached rows: %v -> %v", before, ids)
	}
	if err := os.Rename(store+".saved", store); err != nil {
		t.Fatal(err)
	}
	if s := hermesSync(t, idx, false); s.Changed != 0 || s.Skipped != 0 {
		t.Fatalf("restored store: %+v", s)
	}
}

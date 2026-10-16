package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/Ray0907/kioku/internal/cjk"
	_ "github.com/mattn/go-sqlite3"
)

func roots() map[string]string {
	home := os.Getenv("HOME")
	out := map[string]string{}
	for _, p := range []struct{ h, env, config, child, rel string }{{"claude", "KIOKU_CLAUDE_DIR", "CLAUDE_CONFIG_DIR", "projects", ".claude/projects"}, {"codex", "KIOKU_CODEX_DIR", "CODEX_HOME", "sessions", ".codex/sessions"}, {"pi", "KIOKU_PI_DIR", "PI_CODING_AGENT_DIR", "sessions", ".pi/agent/sessions"}, {"grok", "KIOKU_GROK_DIR", "", "", ".grok/sessions"}, {"cursor", "KIOKU_CURSOR_DIR", "", "", ".cursor/chats"}} {
		dir, child := os.Getenv(p.env), ""
		if dir == "" {
			dir, child = os.Getenv(p.config), p.child
		}
		if dir == "" {
			dir, child = home, p.rel
		}
		if strings.HasPrefix(dir, "~/") {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
		out[p.h] = filepath.Join(dir, child)
	}
	return out
}
func indexPath() string {
	if p := os.Getenv("KIOKU_INDEX"); p != "" {
		return p
	}
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(root, "kioku", "index.db")
}
func openDB() (*sql.DB, error) {
	path := indexPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, e := sql.Open("sqlite3", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(3)
	_, e = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS meta (schema_version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (uid TEXT PRIMARY KEY, harness TEXT, native_id TEXT, path TEXT, cwd TEXT, project TEXT, model TEXT, started TEXT, updated TEXT);
CREATE TABLE IF NOT EXISTS messages (id INTEGER PRIMARY KEY, session_uid TEXT, idx INTEGER, ts TEXT, role TEXT, text TEXT);
CREATE INDEX IF NOT EXISTS messages_session ON messages(session_uid,idx);
CREATE INDEX IF NOT EXISTS messages_ts ON messages(ts);
CREATE TABLE IF NOT EXISTS sources (path TEXT PRIMARY KEY, mtime INTEGER, size INTEGER);
CREATE TABLE IF NOT EXISTS dirs (path TEXT PRIMARY KEY, mtime INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(text, content='messages',content_rowid='id',tokenize='cjk unigram 1 remove_diacritics 2');
CREATE VIRTUAL TABLE IF NOT EXISTS messages_vocab USING fts5vocab(messages_fts,'row');
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid,text) VALUES(new.id,new.text); END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,text) VALUES('delete',old.id,old.text); END;
INSERT INTO meta(schema_version) SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM meta);`)
	if e == nil {
		e = ensureColumn(db, "sessions", "msg_count", "INTEGER")
	}
	if e == nil {
		e = ensureColumn(db, "sources", "offset", "INTEGER DEFAULT 0")
	}
	if e == nil {
		e = ensureColumn(db, "sources", "inode", "INTEGER DEFAULT 0")
	}
	if e == nil {
		e = ensureColumn(db, "messages", "self", "INTEGER DEFAULT 0")
	}
	if e == nil {
		e = ensureColumn(db, "sources", "tool_calls", "TEXT DEFAULT '[]'")
	}
	if e == nil {
		e = ensureColumn(db, "sources", "updated", "INTEGER DEFAULT 0")
	}
	if e == nil {
		_, e = db.Exec(`UPDATE sessions SET msg_count=(SELECT count(*) FROM messages WHERE session_uid=sessions.uid) WHERE msg_count IS NULL`)
	}
	if e == nil {
		_, e = db.Exec(`UPDATE meta SET schema_version=2 WHERE schema_version<2`)
	}
	if e == nil {
		e = os.Chmod(path, 0600)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}

func ensureColumn(db *sql.DB, table, col, definition string) error {
	rows, e := db.Query("PRAGMA table_info(" + table + ")")
	if e != nil {
		return e
	}
	found := false
	for rows.Next() {
		var id, notnull, pk int
		var name, typ string
		var def sql.NullString
		if e = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); e != nil {
			break
		}
		if name == col {
			found = true
		}
	}
	rows.Close()
	if e != nil {
		return e
	}
	if found {
		return nil
	}
	_, e = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + definition)
	return e
}

type fileStat struct {
	path string
	info os.FileInfo
	err  error
}

func statPaths(paths []string) (map[string]os.FileInfo, int) {
	jobs := make(chan string)
	results := make(chan fileStat, len(paths))
	for i := 0; i < 24; i++ {
		go func() {
			for p := range jobs {
				info, err := os.Stat(p)
				results <- fileStat{p, info, err}
			}
		}()
	}
	go func() {
		for _, p := range paths {
			jobs <- p
		}
		close(jobs)
	}()
	infos := make(map[string]os.FileInfo, len(paths))
	failed := 0
	for range paths {
		r := <-results
		if r.err != nil {
			failed++
		} else {
			infos[r.path] = r.info
		}
	}
	return infos, failed
}
func discover(db *sql.DB, rebuild bool) (map[string]string, map[string]int64, bool, int, error) {
	roots := roots()
	known := map[string]int64{}
	if !rebuild {
		rows, e := db.Query("SELECT path,mtime FROM dirs")
		if e != nil {
			return nil, nil, false, 0, e
		}
		for rows.Next() {
			var p string
			var m int64
			if rows.Scan(&p, &m) == nil {
				known[p] = m
			}
		}
		rows.Close()
	}
	stable := len(known) > 0
	for _, root := range roots {
		if _, ok := known[root]; !ok {
			stable = false
		}
	}
	if stable {
		paths := make([]string, 0, len(known))
		for p := range known {
			paths = append(paths, p)
		}
		infos, _ := statPaths(paths)
		for p, mtime := range known {
			now := int64(-1)
			if info := infos[p]; info != nil {
				now = info.ModTime().UnixNano()
			}
			if now != mtime {
				stable = false
				break
			}
		}
	}
	files := map[string]string{}
	dirs := map[string]int64{}
	skipped := 0
	if stable {
		rows, e := db.Query("SELECT path FROM sources")
		if e != nil {
			return nil, nil, false, 0, e
		}
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				for h, root := range roots {
					if strings.HasPrefix(p, root+string(os.PathSeparator)) {
						files[p] = h
						break
					}
				}
			}
		}
		e = rows.Err()
		rows.Close()
		return files, dirs, false, 0, e
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for h, root := range roots {
		wg.Add(1)
		go func(h, root string) {
			defer wg.Done()
			local := map[string]string{}
			localDirs := map[string]int64{}
			bad := 0
			err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					if !errors.Is(e, os.ErrNotExist) {
						bad++
					}
					return nil
				}
				if d.IsDir() {
					if info, e := d.Info(); e == nil {
						localDirs[p] = info.ModTime().UnixNano()
					}
					return nil
				}
				if h == "cursor" && d.Name() == "meta.json" || h == "grok" && d.Name() == "chat_history.jsonl" || h != "grok" && h != "cursor" && strings.HasSuffix(p, ".jsonl") {
					local[p] = h
				}
				return nil
			})
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				bad++
			}
			if _, ok := localDirs[root]; !ok {
				localDirs[root] = -1
			}
			mu.Lock()
			for p, h := range local {
				files[p] = h
			}
			for p, m := range localDirs {
				dirs[p] = m
			}
			skipped += bad
			mu.Unlock()
		}(h, root)
	}
	wg.Wait()
	return files, dirs, true, skipped, nil
}

type syncStats struct {
	Files, Changed, Skipped int
	Messages                map[string]int
}

func syncIndex(db *sql.DB, rebuild bool, progress func(int, int)) (syncStats, error) {
	started := time.Now()
	stats := syncStats{Messages: map[string]int{}}
	var schemaVersion int
	if e := db.QueryRow("SELECT schema_version FROM meta").Scan(&schemaVersion); e != nil {
		return stats, e
	}
	migrating := schemaVersion < 3
	rebuild = rebuild || migrating
	files, dirs, walked, skipped, e := discover(db, rebuild)
	if e != nil {
		return stats, e
	}
	stats.Skipped = skipped
	stats.Files = len(files)
	timing("sync_walk", started)
	started = time.Now()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	statFiles := append([]string(nil), paths...)
	for _, p := range paths {
		if files[p] == "cursor" {
			statFiles = append(statFiles, filepath.Join(filepath.Dir(p), "store.db"))
		}
	}
	infos, failed := statPaths(statFiles)
	stats.Skipped += failed
	if migrating && stats.Skipped > 0 {
		return stats, fmt.Errorf("index migration: %d sources could not be discovered or read", stats.Skipped)
	}
	// DB sources are discovered by session timestamps, never by file size/mtime.
	opencode, err := loadOpencode()
	opencodeFailed := err != nil
	agy, agyErr := loadAgy()
	agyFailed := agyErr != nil
	for _, source := range []struct {
		harness string
		err     error
	}{{"opencode", err}, {"agy", agyErr}} {
		if source.err == nil {
			continue
		}
		fmt.Fprintln(os.Stderr, "kioku: "+source.harness+":", source.err)
		stats.Skipped++
		// A failed scan is not evidence that previously indexed sessions disappeared.
		rows, e := db.Query("SELECT path FROM sessions WHERE harness=?", source.harness)
		if e != nil {
			return stats, e
		}
		for rows.Next() {
			var p string
			if e = rows.Scan(&p); e != nil {
				break
			}
			files[p] = source.harness
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return stats, e
		}
	}
	opSessions := map[string]opencodeSession{}
	if opencode != nil {
		defer opencode.close()
		opSessions = opencode.sessions
		for p := range opSessions {
			files[p] = "opencode"
			paths = append(paths, p)
		}
		sort.Strings(paths)
	}
	agySessions := map[string]agySession{}
	if agy != nil {
		agySessions = agy.sessions
		for p, src := range agySessions {
			files[p] = "agy"
			if src.err != nil {
				fmt.Fprintf(os.Stderr, "kioku: agy: %s: %v\n", p, src.err)
				stats.Skipped++
				agyFailed = true // Keep cached rows for invalid metadata, also on rebuild.
				continue
			}
			paths = append(paths, p)
		}
		sort.Strings(paths)
	}
	stats.Files = len(files)
	timing("sync_stat", started)
	started = time.Now()
	tx, e := db.Begin()
	if e != nil {
		return stats, e
	}
	defer tx.Rollback()
	if walked {
		if _, e = tx.Exec("DELETE FROM dirs"); e != nil {
			return stats, e
		}
		for p, m := range dirs {
			if _, e = tx.Exec("INSERT INTO dirs(path,mtime) VALUES(?,?)", p, m); e != nil {
				return stats, e
			}
		}
	}
	if rebuild && !opencodeFailed && !agyFailed {
		for _, table := range []string{"messages", "sessions", "sources"} {
			if _, e = tx.Exec("DELETE FROM " + table); e != nil {
				return stats, e
			}
		}
	}
	type sourceState struct {
		mtime, size, offset, inode, updated int64
		toolCalls                           string
	}
	old := map[string]sourceState{}
	rows, e := tx.Query("SELECT path,mtime,size,offset,inode,updated,tool_calls FROM sources")
	if e != nil {
		return stats, e
	}
	for rows.Next() {
		var p string
		var v sourceState
		if rows.Scan(&p, &v.mtime, &v.size, &v.offset, &v.inode, &v.updated, &v.toolCalls) == nil {
			old[p] = v
		}
	}
	rows.Close()
	timing("sync_sources", started)
	started = time.Now()
	for p := range old {
		if _, ok := files[p]; !ok {
			if e = removeSource(tx, p); e != nil {
				return stats, e
			}
		}
	}
	for i, p := range paths {
		if progress != nil {
			progress(i+1, len(paths))
		}
		src, opencodeSource := opSessions[p]
		agySrc, agySource := agySessions[p]
		dbSource := opencodeSource || agySource
		cursorSource := files[p] == "cursor"
		mtime, size, inode, updated := int64(0), int64(0), int64(0), int64(0)
		v, ok := old[p]
		ok = ok && !rebuild // Failed DB scans retain rows; other sources still rebuild.
		if agySource {
			updated, size = agySrc.updated, agySrc.steps
			if ok && v.updated == updated && v.size == size {
				continue
			}
		} else if opencodeSource {
			updated = src.updated
			inode = 1 // DB sources use this slot for layout, so v1 -> v2 also reparses.
			if src.v2 {
				inode = 2
			}
			if ok && v.updated == updated && v.inode == inode {
				continue
			}
		} else {
			info := infos[p]
			if info == nil {
				continue
			}
			mtime, size = info.ModTime().UnixNano(), info.Size()
			inode = int64(info.Sys().(*syscall.Stat_t).Ino)
			if cursorSource {
				store := infos[filepath.Join(filepath.Dir(p), "store.db")]
				if store == nil {
					continue
				}
				// Cursor tracks both files: mtime/size are metadata; updated/inode are DB mtime/size.
				updated, inode = store.ModTime().UnixNano(), store.Size()
			}
			if ok && v.mtime == mtime && v.size == size && (!cursorSource || v.updated == updated && v.inode == inode) {
				continue
			}
		}
		appendOnly := !dbSource && !cursorSource && ok && v.inode == inode && v.offset > 0 && size > v.size && v.offset <= v.size
		var base session
		startIdx := 0
		offset := int64(0)
		if appendOnly {
			if e = json.Unmarshal([]byte(v.toolCalls), &base.ToolCalls); e != nil {
				return stats, e
			}
			offset = v.offset
			e = tx.QueryRow("SELECT uid,harness,native_id,path,cwd,project,model,started,updated,msg_count FROM sessions WHERE path=?", p).Scan(&base.UID, &base.Harness, &base.NativeID, &base.Path, &base.CWD, &base.Project, &base.Model, &base.Started, &base.Updated, &startIdx)
			if e != nil {
				appendOnly = false
				offset = 0
				startIdx = 0
			}
		}
		var s session
		var nextOffset int64
		var err error
		if agySource {
			s, err = agy.parse(agySrc)
		} else if opencodeSource {
			s, err = opencode.parse(src)
		} else if cursorSource {
			s, err = parseCursor(p, updated)
		} else {
			s, nextOffset, err = parseFile(p, files[p], offset, size, base, startIdx)
		}
		if err != nil {
			// No messages means a successful read with nothing to index.
			// Agy has no old schema rows; a missing conversation must not block migration.
			if migrating && !agySource && !errors.Is(err, errNoMessages) {
				return stats, fmt.Errorf("index migration: %s: %w", p, err)
			}
			stats.Skipped++
			if errors.Is(err, errNoMessages) {
				if dbSource || cursorSource || rebuild {
					if e = removeSource(tx, p); e != nil {
						return stats, e
					}
				}
				if _, e = tx.Exec("INSERT INTO sources(path,mtime,size,offset,inode,updated) VALUES(?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime,size=excluded.size,offset=excluded.offset,inode=excluded.inode,updated=excluded.updated", p, mtime, size, nextOffset, inode, updated); e != nil {
					return stats, e
				}
			}
			continue
		}
		if !appendOnly {
			if e = removeSource(tx, p); e != nil {
				return stats, e
			}
			if _, e = tx.Exec("INSERT INTO sessions(uid,harness,native_id,path,cwd,project,model,started,updated,msg_count) VALUES(?,?,?,?,?,?,?,?,?,?)", s.UID, s.Harness, s.NativeID, s.Path, s.CWD, s.Project, s.Model, s.Started, s.Updated, len(s.Messages)); e != nil {
				return stats, e
			}
		} else {
			if _, e = tx.Exec("UPDATE sessions SET cwd=?,project=?,model=?,updated=?,msg_count=msg_count+? WHERE uid=?", s.CWD, s.Project, s.Model, s.Updated, len(s.Messages), s.UID); e != nil {
				return stats, e
			}
		}
		for _, m := range s.Messages {
			if _, e = tx.Exec("INSERT INTO messages(session_uid,idx,ts,role,text,self) VALUES(?,?,?,?,?,?)", s.UID, m.Index, m.TS, m.Role, m.Text, m.Self); e != nil {
				return stats, e
			}
		}
		calls, err := json.Marshal(s.ToolCalls)
		if err != nil {
			return stats, err
		}
		if _, e = tx.Exec("INSERT INTO sources(path,mtime,size,offset,inode,updated,tool_calls) VALUES(?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime,size=excluded.size,offset=excluded.offset,inode=excluded.inode,updated=excluded.updated,tool_calls=excluded.tool_calls", p, mtime, size, nextOffset, inode, updated, string(calls)); e != nil {
			return stats, e
		}
		stats.Changed++
	}
	timing("sync_changes", started)
	started = time.Now()
	if migrating {
		if _, e = tx.Exec("UPDATE meta SET schema_version=3"); e != nil {
			return stats, e
		}
	}
	if e = tx.Commit(); e != nil {
		return stats, e
	}
	rows, e = db.Query("SELECT harness,sum(msg_count) FROM sessions GROUP BY harness")
	if e != nil {
		return stats, e
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		var n int
		if rows.Scan(&h, &n) == nil {
			stats.Messages[h] = n
		}
	}
	timing("sync_counts", started)
	return stats, rows.Err()
}
func removeSource(tx *sql.Tx, p string) error {
	for _, q := range []string{"DELETE FROM messages WHERE session_uid IN (SELECT uid FROM sessions WHERE path=?)", "DELETE FROM sessions WHERE path=?", "DELETE FROM sources WHERE path=?"} {
		if _, e := tx.Exec(q, p); e != nil {
			return e
		}
	}
	return nil
}
func age(ts string) string {
	t, e := time.Parse(time.RFC3339Nano, ts)
	if e != nil {
		return ""
	}
	d := time.Since(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm", max(0, int(d.Minutes())))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dw", int(d.Hours()/168))
}

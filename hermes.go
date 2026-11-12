package main

import (
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func hermesPath() string {
	p := os.Getenv("KIOKU_HERMES_DB")
	if p == "" {
		home := os.Getenv("HERMES_HOME")
		if home == "" {
			home = filepath.Join(os.Getenv("HOME"), ".hermes")
		}
		// ponytail: only the home store is read; add profiles/<name>/state.db discovery when profile support is requested.
		p = filepath.Join(home, "state.db")
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(p, "~/"))
	}
	return p
}

type hermesSession struct {
	session
	count, newest int64
}

type hermesStore struct {
	db       *sql.DB
	tx       *sql.Tx
	sessions map[string]hermesSession
	filter   string
}

func (h *hermesStore) close() {
	h.tx.Rollback()
	h.db.Close()
}

func loadHermes() (*hermesStore, error) {
	path, err := filepath.Abs(hermesPath())
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	// Like OpenCode, read the live WAL, never immutable snapshots or checkpoints.
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_query_only=1&_busy_timeout=5000"}
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	h := &hermesStore{db: db, tx: tx, sessions: map[string]hermesSession{}}
	if err = h.discover(path); err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}

func (h *hermesStore) discover(path string) error {
	var exists, cwd, active, summary int
	if err := h.tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sessions'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("sessions table not found")
	}
	if err := h.tx.QueryRow("SELECT count(*) FROM pragma_table_info('sessions') WHERE name='cwd'").Scan(&cwd); err != nil {
		return err
	}
	if cwd == 0 {
		return nil
	}
	if err := h.tx.QueryRow("SELECT count(CASE WHEN name='active' THEN 1 END),count(CASE WHEN name='_compressed_summary' THEN 1 END) FROM pragma_table_info('messages')").Scan(&active, &summary); err != nil {
		return err
	}
	if active > 0 {
		h.filter += " AND COALESCE(active,1)<>0"
	}
	if summary > 0 {
		h.filter += " AND COALESCE(_compressed_summary,0)<>1"
	}
	// shortcut: count/max-id miss in-place edits; add a revision signal if Hermes exposes one.
	rows, err := h.tx.Query(`SELECT s.id,s.cwd,COALESCE(s.model,''),s.started_at,
COALESCE((SELECT count(*) FROM messages WHERE session_id=s.id),0),
COALESCE((SELECT max(id) FROM messages WHERE session_id=s.id),0)
FROM sessions s WHERE COALESCE(s.cwd,'')<>'' AND s.parent_session_id IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, cwd, model string
		var started float64
		var count, newest int64
		if err = rows.Scan(&id, &cwd, &model, &started, &count, &newest); err != nil {
			return err
		}
		p := path + "#" + id
		h.sessions[p] = hermesSession{session: session{UID: "hermes:" + p, Harness: "hermes", NativeID: id, Path: p, CWD: cwd, Project: filepath.Base(cwd), Model: model, Started: hermesTime(started)}, count: count, newest: newest}
	}
	return rows.Err()
}

func doctorHermes(path string) (int, error) {
	db, cleanup, err := doctorReadDB(path)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	var cwd, count int
	if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('sessions') WHERE name='cwd'").Scan(&cwd); err != nil {
		return 0, err
	}
	if cwd == 0 {
		err = db.QueryRow("SELECT count(*) FROM sessions").Scan(&count)
		return 0, err
	}
	err = db.QueryRow("SELECT count(*) FROM sessions WHERE COALESCE(cwd,'')<>'' AND parent_session_id IS NULL").Scan(&count)
	return count, err
}

func hermesTime(seconds float64) string {
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e9))).UTC().Format(time.RFC3339Nano)
}

func (h *hermesStore) parse(src hermesSession) (session, error) {
	s := src.session
	rows, err := h.tx.Query("SELECT role,COALESCE(content,''),COALESCE(tool_name,''),timestamp FROM messages WHERE session_id=?"+h.filter+" ORDER BY id", s.NativeID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	s.Updated = s.Started
	for rows.Next() {
		var role, content, tool string
		var seconds float64
		if err = rows.Scan(&role, &content, &tool, &seconds); err != nil {
			return s, err
		}
		switch role {
		case "assistant":
			role = "asst"
		case "user", "tool":
		default:
			continue
		}
		text := clean(content)
		if text == "" {
			continue
		}
		if role == "tool" {
			if tool == "" {
				tool = "tool"
			}
			text = tool + " · " + first(text)
		}
		stamp := hermesTime(seconds)
		s.Updated = stamp
		s.Messages = append(s.Messages, message{Index: len(s.Messages), TS: stamp, Role: role, Text: text})
	}
	if err = rows.Err(); err != nil {
		return s, err
	}
	if len(s.Messages) == 0 {
		return s, errNoMessages
	}
	return s, nil
}

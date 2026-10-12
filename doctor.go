package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
)

var errDoctorBusy = errors.New("busy, rerun when the agent is idle")

type doctorCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("kioku doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonFlag := fs.Bool("json", false, "print checks as a JSON array")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("doctor takes no positional arguments")
	}
	checks := []doctorCheck{}
	sourceRoots := roots()
	sessions := 0
	for _, source := range []struct{ harness, name string }{{"claude", "Claude Code"}, {"codex", "Codex"}, {"pi", "Pi"}, {"grok", "Grok"}, {"opencode", "OpenCode"}, {"cursor", "Cursor CLI"}} {
		path := sourceRoots[source.harness]
		if source.harness == "opencode" {
			path = opencodePath()
		}
		c := doctorCheck{Name: source.name}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			c.Optional = true
			c.Detail = fmt.Sprintf("not found (optional: %s at %s)", source.name, path)
		} else {
			count := 0
			unit := "session files"
			if err == nil {
				if source.harness == "opencode" {
					unit = "sessions"
					count, err = doctorOpencode(path)
				} else if !info.IsDir() {
					err = fmt.Errorf("not a directory")
				} else {
					err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if !d.IsDir() && (source.harness == "cursor" && d.Name() == "meta.json" || source.harness == "grok" && d.Name() == "chat_history.jsonl" || source.harness != "cursor" && source.harness != "grok" && strings.HasSuffix(p, ".jsonl")) {
							count++
						}
						return nil
					})
				}
			}
			sessions += count
			c.OK = err == nil || errors.Is(err, errDoctorBusy)
			c.Detail = fmt.Sprintf("%s, %d %s", path, count, unit)
			if errors.Is(err, errDoctorBusy) {
				c.Detail = path
			}
			if err != nil {
				c.Detail += ": " + err.Error()
			}
		}
		checks = append(checks, c)
	}
	checks = append(checks, doctorIndex(sessions > 0))
	err := doctorSQLite()
	c := doctorCheck{Name: "sqlite", OK: err == nil, Detail: "FTS5 available, cjk tokenizer MATCH passed"}
	if err != nil {
		c.Detail = err.Error()
	}
	checks = append(checks, c)
	failed := false
	for i := range checks {
		checks[i].Detail = strings.NewReplacer("\n", "\\n", "\r", "\\r", "\x1b", "\\x1b").Replace(checks[i].Detail)
		failed = failed || !checks[i].OK && !checks[i].Optional
	}
	if *jsonFlag {
		if err := json.NewEncoder(output).Encode(checks); err != nil {
			return err
		}
	} else {
		f, isFile := output.(*os.File)
		color := isFile && isatty.IsTerminal(f.Fd())
		for _, c := range checks {
			mark, ansi := "✗", "\x1b[31m"
			if c.OK {
				mark, ansi = "✓", "\x1b[32m"
			} else if c.Optional {
				mark, ansi = "-", ""
			}
			line := fmt.Sprintf("%s %s  %s", mark, c.Name, c.Detail)
			if color && ansi != "" {
				line = ansi + line + "\x1b[0m"
			}
			if _, err := fmt.Fprintln(output, line); err != nil {
				return err
			}
		}
	}
	if failed {
		return fmt.Errorf("doctor: required checks failed")
	}
	return nil
}

func doctorIndex(hasSessions bool) doctorCheck {
	path := indexPath()
	c := doctorCheck{Name: "index", Detail: path}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		c.Optional = !hasSessions
		if hasSessions {
			c.Detail += ": not found; run kioku to build the index"
		} else {
			c.Detail = fmt.Sprintf("not found (optional: no source sessions; %s)", path)
		}
		return c
	}
	if err == nil {
		c.Detail += fmt.Sprintf(", %d bytes", info.Size())
		var db *sql.DB
		db, err = doctorOpenReadOnly(path)
		if err == nil {
			defer db.Close()
			var version, sessions, messages int
			err = db.QueryRow(`SELECT schema_version, (SELECT count(*) FROM sessions), (SELECT count(*) FROM messages) FROM meta`).Scan(&version, &sessions, &messages)
			if err == nil {
				c.Detail += fmt.Sprintf(", schema_version=%d, %d sessions, %d messages", version, sessions, messages)
			}
		}
	}
	c.OK = err == nil
	if err != nil {
		c.Detail += ": " + err.Error()
	}
	return c
}

func doctorOpencode(path string) (int, error) {
	db, closeDB, err := doctorReadDB(path)
	if err != nil {
		return 0, err
	}
	defer closeDB()
	var v1, v2 int
	if err = db.QueryRow(`SELECT count(CASE WHEN name='session' THEN 1 END), count(CASE WHEN name='session_v2' THEN 1 END) FROM sqlite_master WHERE type='table'`).Scan(&v1, &v2); err != nil {
		return 0, err
	}
	if v1+v2 == 0 {
		return 0, fmt.Errorf("neither session nor session_v2 table found")
	}
	queries := []string{}
	if v1 > 0 {
		q := `SELECT id FROM session WHERE (parent_id IS NULL OR parent_id='')`
		if v2 > 0 {
			q += ` AND NOT EXISTS(SELECT 1 FROM session_v2 WHERE session_v2.id=session.id)`
		}
		queries = append(queries, q)
	}
	if v2 > 0 {
		queries = append(queries, `SELECT id FROM session_v2 WHERE (parent_id IS NULL OR parent_id='')`)
	}
	var count int
	err = db.QueryRow("SELECT count(*) FROM (" + strings.Join(queries, " UNION ALL ") + ")").Scan(&count)
	return count, err
}

func doctorSQLite() error {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE VIRTUAL TABLE probe USING fts5(text, tokenize='cjk unigram 1 remove_diacritics 2'); INSERT INTO probe VALUES('hello 結帳');`); err != nil {
		return err
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM probe WHERE probe MATCH 'hello AND 結帳'`).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("FTS5 cjk MATCH returned %d rows, want 1", count)
	}
	return nil
}

func doctorOpenReadOnly(path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_query_only=1&_busy_timeout=5000"}
	db, err := sql.Open("sqlite3", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

type doctorFileStamp struct {
	size, mtime int64
	exists      bool
}

func doctorFingerprint(path string) ([3]doctorFileStamp, error) {
	var stamps [3]doctorFileStamp
	for i, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return stamps, err
		}
		stamps[i] = doctorFileStamp{info.Size(), info.ModTime().UnixNano(), true}
	}
	return stamps, nil
}

func doctorReadDB(path string) (*sql.DB, func(), error) {
	// Harness readers can write WAL coordination files; only query a stable private copy.
	for attempt := 0; attempt < 3; attempt++ {
		before, err := doctorFingerprint(path)
		if err != nil {
			return nil, nil, err
		}
		dir, err := os.MkdirTemp("", "kioku-doctor-")
		if err != nil {
			return nil, nil, err
		}
		cleanup := func() { os.RemoveAll(dir) }
		copyPath := filepath.Join(dir, "snapshot.db")
		var copyErr error
		for _, suffix := range []string{"", "-wal"} {
			copyErr = doctorCopy(path+suffix, copyPath+suffix)
			if suffix == "-wal" && os.IsNotExist(copyErr) {
				copyErr = nil
			}
			if copyErr != nil {
				break
			}
		}
		if copyErr != nil {
			cleanup()
			return nil, nil, copyErr
		}
		after, err := doctorFingerprint(path)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		if before != after {
			cleanup()
			continue
		}
		db, err := doctorOpenReadOnly(copyPath)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		return db, func() { db.Close(); cleanup() }, nil
	}
	return nil, nil, errDoctorBusy
}

var doctorCopy = func(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

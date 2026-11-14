package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

type sourceState struct {
	mtime, size, offset, inode, updated int64
	toolCalls                           string
}

type sourceRead struct {
	state            sourceState
	session          session
	skip, appendOnly bool
	err              error
}

func readHermesSource(store *hermesStore, src hermesSession, old sourceState, ok bool) sourceRead {
	r := sourceRead{state: sourceState{updated: src.newest, size: src.count}}
	if ok && old.updated == r.state.updated && old.size == r.state.size {
		r.skip = true
		return r
	}
	r.session, r.err = store.parse(src)
	return r
}

func readAgySource(store *agyStore, src agySession, old sourceState, ok bool) sourceRead {
	r := sourceRead{state: sourceState{updated: src.updated, size: src.steps}}
	if ok && old.updated == r.state.updated && old.size == r.state.size {
		r.skip = true
		return r
	}
	r.session, r.err = store.parse(src)
	return r
}

func readOpencodeSource(store *opencodeStore, src opencodeSession, old sourceState, ok bool) sourceRead {
	r := sourceRead{state: sourceState{updated: src.updated, inode: 1}}
	// DB sources use this slot for layout, so v1 -> v2 also reparses.
	if src.v2 {
		r.state.inode = 2
	}
	if ok && old.updated == r.state.updated && old.inode == r.state.inode {
		r.skip = true
		return r
	}
	r.session, r.err = store.parse(src)
	return r
}

func readCursorSource(path string, info, store os.FileInfo, old sourceState, ok bool) sourceRead {
	r := sourceRead{}
	if info == nil {
		r.skip = true
		return r
	}
	r.state.mtime, r.state.size = info.ModTime().UnixNano(), info.Size()
	r.state.inode = int64(info.Sys().(*syscall.Stat_t).Ino)
	if store == nil {
		r.skip = true
		return r
	}
	// Cursor tracks both files: mtime/size are metadata; updated/inode are DB mtime/size.
	r.state.updated, r.state.inode = store.ModTime().UnixNano(), store.Size()
	if ok && old.mtime == r.state.mtime && old.size == r.state.size && old.updated == r.state.updated && old.inode == r.state.inode {
		r.skip = true
		return r
	}
	r.session, r.err = parseCursor(path, r.state.updated)
	return r
}

func readFileSource(tx *sql.Tx, path, harness string, info os.FileInfo, old sourceState, ok bool) (sourceRead, error) {
	r := sourceRead{}
	if info == nil {
		r.skip = true
		return r, nil
	}
	r.state.mtime, r.state.size = info.ModTime().UnixNano(), info.Size()
	r.state.inode = int64(info.Sys().(*syscall.Stat_t).Ino)
	if ok && old.mtime == r.state.mtime && old.size == r.state.size {
		r.skip = true
		return r, nil
	}
	r.appendOnly = ok && old.inode == r.state.inode && old.offset > 0 && r.state.size > old.size && old.offset <= old.size
	var base session
	startIdx := 0
	offset := int64(0)
	if r.appendOnly {
		if err := json.Unmarshal([]byte(old.toolCalls), &base.ToolCalls); err != nil {
			return r, err
		}
		offset = old.offset
		err := tx.QueryRow("SELECT uid,harness,native_id,path,cwd,project,model,started,updated,msg_count FROM sessions WHERE path=?", path).Scan(&base.UID, &base.Harness, &base.NativeID, &base.Path, &base.CWD, &base.Project, &base.Model, &base.Started, &base.Updated, &startIdx)
		if err != nil {
			r.appendOnly = false
			offset = 0
			startIdx = 0
		}
	}
	r.session, r.state.offset, r.err = parseFile(path, harness, offset, r.state.size, base, startIdx)
	return r, nil
}

type sourceScan struct {
	harness string
	err     error
}

func keepFailedSources(db *sql.DB, files map[string]string, stats *syncStats, scans []sourceScan) error {
	for _, source := range scans {
		if source.err == nil {
			continue
		}
		stats.Warnings = append(stats.Warnings, fmt.Sprintf("%s: %v", source.harness, source.err))
		stats.Skipped++
		// A failed scan is not evidence that previously indexed sessions disappeared.
		rows, e := db.Query("SELECT path FROM sessions WHERE harness=?", source.harness)
		if e != nil {
			return e
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
			return e
		}
	}
	return nil
}

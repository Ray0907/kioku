package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

var migrateHook func(step string) error

func migrateIndex(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return err
	}
	legacy := func() (bool, error) {
		var n int
		err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('messages') WHERE name='session_uid'`).Scan(&n)
		return n != 0, err
	}
	old, err := legacy()
	if err != nil || !old {
		return err
	}
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `ROLLBACK`)
	old, err = legacy()
	if err != nil || !old {
		return err
	}
	for _, col := range []struct{ table, name, definition string }{
		{"messages", "self", "INTEGER DEFAULT 0"},
		{"sessions", "automated", "INTEGER NOT NULL DEFAULT 0"},
		{"sessions", "msg_count", "INTEGER"},
	} {
		var n int
		if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, col.table, col.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err = conn.ExecContext(ctx, "ALTER TABLE "+col.table+" ADD COLUMN "+col.name+" "+col.definition); err != nil {
				return err
			}
		}
	}
	steps := []struct{ name, sql string }{
		{"columns", `UPDATE sessions SET msg_count=(SELECT count(*) FROM messages WHERE session_uid=sessions.uid) WHERE msg_count IS NULL`},
		{"drop-triggers", `DROP TRIGGER IF EXISTS messages_ai; DROP TRIGGER IF EXISTS messages_ad;`},
		{"create-tables", `CREATE TABLE sessions_new(session_id INTEGER PRIMARY KEY,uid TEXT NOT NULL UNIQUE,harness TEXT,native_id TEXT,path TEXT,cwd TEXT,project TEXT,model TEXT,started TEXT,updated TEXT,msg_count INTEGER,automated INTEGER NOT NULL DEFAULT 0);
CREATE TABLE messages_new(id INTEGER PRIMARY KEY,session_id INTEGER NOT NULL,idx INTEGER,ts TEXT,role TEXT,text TEXT,self INTEGER DEFAULT 0);`},
		{"copy-sessions", `INSERT INTO sessions_new SELECT rowid,uid,harness,native_id,path,cwd,project,model,started,updated,msg_count,automated FROM sessions ORDER BY rowid`},
		{"copy-messages", `INSERT INTO messages_new SELECT m.id,s.rowid,m.idx,m.ts,m.role,m.text,m.self FROM messages m JOIN sessions s ON s.uid=m.session_uid ORDER BY m.id`},
		{"drop-tables", `DROP TABLE messages; DROP TABLE sessions;`},
		{"rename-tables", `ALTER TABLE sessions_new RENAME TO sessions; ALTER TABLE messages_new RENAME TO messages;`},
		{"indexes", `CREATE INDEX messages_session_id ON messages(session_id,idx); CREATE INDEX messages_ts ON messages(ts);`},
		{"triggers", `CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid,text) VALUES(new.id,new.text); END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,text) VALUES('delete',old.id,old.text); END;`},
		{"rebuild", `INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`},
		{"version", `UPDATE meta SET schema_version=6 WHERE schema_version=5`},
	}
	for _, step := range steps {
		if _, err = conn.ExecContext(ctx, step.sql); err != nil {
			return err
		}
		if migrateHook != nil {
			if err = migrateHook(step.name); err != nil {
				return err
			}
		}
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	if migrateHook != nil {
		err = migrateHook("vacuum")
	}
	if err == nil {
		_, err = conn.ExecContext(ctx, `VACUUM`)
	}
	if err != nil {
		// The index is valid, only larger.
		fmt.Fprintf(os.Stderr, "warning: index vacuum: %v\n", err)
	}
	return nil
}

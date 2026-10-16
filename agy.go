package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func agyDir() string {
	p := os.Getenv("KIOKU_AGY_DIR")
	if p == "" {
		p = filepath.Join(os.Getenv("HOME"), ".gemini/antigravity-cli")
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(p, "~/"))
	}
	return p
}

func openAgyDB(path string) (*sql.DB, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	// Do not use immutable=1: new steps can be in the WAL.
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_query_only=1&_busy_timeout=5000"}
	return sql.Open("sqlite3", u.String())
}

type agySession struct {
	session
	updated, steps int64
	err            error
}
type agyStore struct {
	sessions map[string]agySession
}

func loadAgy() (*agyStore, error) {
	root, e := filepath.Abs(agyDir())
	if e != nil {
		return nil, e
	}
	path := filepath.Join(root, "conversation_summaries.db")
	if _, e = os.Stat(path); os.IsNotExist(e) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	db, e := openAgyDB(path)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	rows, e := db.Query(`SELECT conversation_id, COALESCE(workspace_uris,'[]'),
COALESCE(last_modified_time,''), COALESCE(step_count,0) FROM conversation_summaries
WHERE parent_conversation_id IS NULL OR parent_conversation_id=''`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	a := &agyStore{sessions: map[string]agySession{}}
	for rows.Next() {
		var id, workspaces, modified string
		var count int64
		if e = rows.Scan(&id, &workspaces, &modified, &count); e != nil {
			return nil, e
		}
		if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
			continue
		}
		var uris []string
		cwd := ""
		if json.Unmarshal([]byte(workspaces), &uris) == nil && len(uris) > 0 {
			if u, err := url.Parse(uris[0]); err == nil && u.Scheme == "file" && u.Opaque == "" &&
				(u.Host == "" || strings.EqualFold(u.Host, "localhost")) && u.User == nil &&
				filepath.IsAbs(u.Path) && !strings.ContainsRune(u.Path, 0) &&
				u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" {
				cwd = u.Path
			}
		}
		var cwdErr error
		if cwd == "" {
			cwdErr = fmt.Errorf("workspace_uris: first entry must be an absolute local file URI")
		}
		stamp, e := time.Parse(time.RFC3339Nano, strings.Replace(modified, " ", "T", 1))
		if e != nil {
			return nil, e
		}
		p := filepath.Join(root, "conversations", id+".db")
		a.sessions[p] = agySession{session: session{UID: "agy:" + p, Harness: "agy", NativeID: id, Path: p, CWD: cwd, Project: filepath.Base(cwd), Started: stamp.UTC().Format(time.RFC3339Nano), Updated: stamp.UTC().Format(time.RFC3339Nano)}, updated: stamp.UnixNano(), steps: count, err: cwdErr}
	}
	return a, rows.Err()
}

// Decode one wire level. Text and nested messages both use wire type 2.
type agyField struct {
	wire, number uint64
	data         []byte
}
type agyFields map[uint64][]agyField

func decodeAgy(data []byte) (agyFields, bool) {
	fields := agyFields{}
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 || tag>>3 > (1<<29)-1 {
			return nil, false
		}
		data = data[n:]
		f := agyField{wire: tag & 7}
		switch f.wire {
		case 0:
			f.number, n = binary.Uvarint(data)
			if n <= 0 {
				return nil, false
			}
			data = data[n:]
		case 2:
			length, n := binary.Uvarint(data)
			if n <= 0 || length > uint64(len(data)-n) {
				return nil, false
			}
			data = data[n:]
			f.data, data = data[:int(length)], data[int(length):]
		case 1, 5:
			n = 8
			if f.wire == 5 {
				n = 4
			}
			if len(data) < n {
				return nil, false
			}
			data = data[n:]
		default:
			return nil, false
		}
		fields[tag>>3] = append(fields[tag>>3], f)
	}
	return fields, true
}

func (f agyFields) sub(path ...uint64) (agyFields, bool) {
	for _, key := range path {
		values := f[key]
		if len(values) == 0 {
			return nil, true
		}
		if values[0].wire != 2 {
			return nil, false
		}
		var ok bool
		f, ok = decodeAgy(values[0].data)
		if !ok {
			return nil, false
		}
	}
	return f, true
}

func (f agyFields) text(key uint64) (string, bool) {
	values := f[key]
	if len(values) == 0 {
		return "", true
	}
	v := values[0]
	return string(v.data), v.wire == 2 && utf8.Valid(v.data)
}

func (a *agyStore) parse(src agySession) (session, error) {
	s := src.session
	if src.err != nil {
		return s, src.err
	}
	if src.steps == 0 {
		return s, errNoMessages
	}
	db, e := openAgyDB(s.Path)
	if e != nil {
		return s, e
	}
	defer db.Close()
	rows, e := db.Query("SELECT step_type,step_payload FROM steps ORDER BY idx")
	if e != nil {
		return s, e
	}
	defer rows.Close()
	for rows.Next() {
		var typ int
		var data []byte
		if e = rows.Scan(&typ, &data); e != nil {
			return s, e
		}
		if typ != 14 && typ != 15 && typ != 132 {
			continue
		}
		fields, ok := decodeAgy(data)
		if !ok {
			continue
		}
		clock, ok := fields.sub(5, 1)
		if !ok {
			continue
		}
		stamp := s.Updated
		if len(clock[1]) > 0 {
			sec, nano := clock[1][0], agyField{}
			if len(clock[2]) > 0 {
				nano = clock[2][0]
			}
			if sec.wire != 0 || nano.wire != 0 || sec.number > 1<<63-1 || nano.number >= 1e9 {
				continue
			}
			stamp = time.Unix(int64(sec.number), int64(nano.number)).UTC().Format(time.RFC3339Nano)
		}
		var messages []message
		add := func(role, text string, self bool) {
			if text = clean(text); text != "" && !(role == "user" && injectedUserText(text)) {
				messages = append(messages, message{TS: stamp, Role: role, Text: text, Self: self})
			}
		}
		switch typ {
		case 14:
			body, valid := fields.sub(19)
			text, validText := body.text(2)
			ok = valid && validText
			add("user", text, false)
		case 15:
			body, valid := fields.sub(20)
			text, validText := body.text(1)
			ok = valid && validText
			add("asst", text, false)
			for _, call := range body[7] {
				args, valid := decodeAgy(call.data)
				_, validID := args.text(1)
				name, validName := args.text(2)
				input, validInput := args.text(3)
				ok = ok && call.wire == 2 && valid && validID && validName && validInput
				command := toolCommand(input)
				self := selfCommandRE.MatchString(command)
				if command == "" {
					command = input
				}
				add("tool", name+" · "+first(command), self)
			}
		case 132:
			body, valid := fields.sub(140)
			result, validResult := body.sub(2)
			command, validCommand := body.sub(1)
			text, validText := result.text(1)
			cmd, validCmd := command.text(2)
			ok = valid && validResult && validCommand && validText && validCmd
			if cmd != "" && text != "" {
				text = cmd + " · " + text
			} else if text == "" {
				text = cmd
			}
			add("tool", text, selfCommandRE.MatchString(cmd))
		}
		if !ok {
			continue
		}
		for _, m := range messages {
			if len(s.Messages) == 0 {
				s.Started = m.TS
			}
			m.Index = len(s.Messages)
			s.Messages = append(s.Messages, m)
		}
	}
	if e = rows.Err(); e != nil {
		return s, e
	}
	if len(s.Messages) == 0 {
		return s, errNoMessages
	}
	return s, nil
}

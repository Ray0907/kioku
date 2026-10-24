package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	contextDays     = 14
	contextSessions = 5
)

type contextSession struct {
	uid, harness, updated, topic, asked, reply string
}

// inProject reports whether a session started in cwd belongs to the project at dir:
// the same folder or either one inside the other. Home and "/" are never a project.
func inProject(dir, cwd string) bool {
	home := os.Getenv("HOME")
	if dir == "" || cwd == "" || dir == "/" || dir == home || cwd == "/" || cwd == home {
		return false
	}
	return cwd == dir || strings.HasPrefix(dir, cwd+"/") || strings.HasPrefix(cwd, dir+"/")
}

// contextText lists where agents left off in the project at dir: up to five sessions from the
// last 14 days, each with its last question and the first line of its last reply. Every harness
// gets its latest session in before the rest fill by time. Empty when there are none.
func contextText(ctx context.Context, db *sql.DB, dir string, now time.Time) (string, error) {
	dir = filepath.Clean(dir)
	rows, e := db.QueryContext(ctx, `SELECT uid,harness,cwd,updated FROM sessions WHERE updated>=? ORDER BY updated DESC`,
		now.AddDate(0, 0, -contextDays).UTC().Format(time.RFC3339Nano))
	if e != nil {
		return "", e
	}
	var found []contextSession
	for rows.Next() {
		var s contextSession
		var cwd string
		if e = rows.Scan(&s.uid, &s.harness, &cwd, &s.updated); e != nil {
			rows.Close()
			return "", e
		}
		if inProject(dir, cwd) {
			found = append(found, s)
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return "", e
	}
	// Sessions nobody typed into (another agent's run, an empty start) say nothing about where work stopped.
	var asked []contextSession
	for _, s := range found {
		topic, err := sessionTopic(ctx, db, s.uid)
		if err != nil {
			return "", err
		}
		if automatedPrompt(topic) {
			continue
		}
		s.topic = topic
		err = db.QueryRowContext(ctx, `SELECT text FROM messages WHERE session_uid=? AND role='user' AND self=0 ORDER BY idx DESC LIMIT 1`, s.uid).Scan(&s.asked)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return "", err
		}
		asked = append(asked, s)
	}
	picked, seen := map[string]bool{}, map[string]bool{}
	for _, s := range asked {
		if !seen[s.harness] && len(picked) < contextSessions {
			seen[s.harness], picked[s.uid] = true, true
		}
	}
	for _, s := range asked {
		if len(picked) < contextSessions {
			picked[s.uid] = true
		}
	}
	var out []contextSession
	for _, s := range asked {
		if picked[s.uid] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].updated > out[j].updated })
	var b strings.Builder
	fmt.Fprintf(&b, "kioku: recent sessions in this project, across your coding agents (%d of %d from the last %d days):\n\n", len(out), len(asked), contextDays)
	for _, s := range out {
		var reply string
		err := db.QueryRowContext(ctx, `SELECT text FROM messages WHERE session_uid=? AND role='asst' AND self=0 ORDER BY idx DESC LIMIT 1`, s.uid).Scan(&reply)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
		fmt.Fprintf(&b, "- %s · %s ago · %q · `%s`\n  last asked: %q\n", s.harness, ageAt(s.updated, now), s.topic, shortID(s.uid), firstLine(s.asked))
		if reply = firstLine(reply); reply != "" {
			fmt.Fprintf(&b, "  last reply: %q\n", reply)
		}
	}
	b.WriteString("\nRead one with the kioku_show tool, or run `kioku show <ref>`.\n")
	return b.String(), nil
}

// automatedPrompt spots sessions another agent opened to review a change: they never say where
// your own work stopped. ponytail: a prefix list, extend it when a new reviewer shows up.
func automatedPrompt(topic string) bool {
	for _, p := range []string{"Review this change", "# Review Guidelines"} {
		if strings.HasPrefix(topic, p) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(displayInline(strings.TrimSpace(s)), 160)
}

// runContext prints contextText for the working directory. It never fails a session start:
// no index, no history, or any error prints nothing and exits 0.
func runContext(args []string) error {
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if len(args) > 0 {
		return fmt.Errorf("context takes no arguments")
	}
	if _, e := os.Stat(indexPath()); e != nil {
		return nil
	}
	db, e := openDB()
	if e != nil {
		return nil
	}
	defer db.Close()
	if _, e = syncIndex(db, false, nil); e != nil {
		return nil
	}
	text, e := contextText(context.Background(), db, dir, time.Now())
	if e != nil {
		return nil
	}
	fmt.Fprint(output, text)
	return nil
}

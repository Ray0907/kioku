package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

const statsGroupLimit = 20

type statRow struct {
	Value      string `json:"value,omitempty"`
	Sessions   int    `json:"sessions"`
	Messages   int    `json:"messages"`
	LastActive string `json:"last_active"`
}
type statsReport struct {
	Total   statRow   `json:"total"`
	Harness []statRow `json:"harness"`
	Project []statRow `json:"project"`
}

func validHarness(h string) bool {
	switch h {
	case "all", "claude", "codex", "pi", "grok", "opencode", "cursor", "agy":
		return true
	}
	return false
}

// datetime() gives UTC "YYYY-MM-DD HH:MM:SS" and orders correctly across the mixed offsets in stored stamps.
const lastActiveExpr = `coalesce(strftime('%Y-%m-%dT%H:%M:%SZ',max(datetime(m.ts))),'')`

func scanStat(r *sql.Rows) (s statRow, e error) {
	e = r.Scan(&s.Value, &s.Sessions, &s.Messages, &s.LastActive)
	return
}

func foldStats(rest []statRow) statRow {
	other := statRow{Value: "(other)"}
	for _, s := range rest {
		other.Sessions += s.Sessions
		other.Messages += s.Messages
		other.LastActive = max(other.LastActive, s.LastActive)
	}
	return other
}

// Counts messages inside the range, so a session appears only when it has a message there.
// Messages from kioku's own tool calls are left out, as in search.
func collectStats(ctx context.Context, db *sql.DB, harness string, projects []string, rng timeRange) (statsReport, error) {
	names, e := matchingProjects(ctx, db, projects)
	if e != nil {
		return statsReport{}, e
	}
	from := `FROM messages m JOIN sessions s ON s.uid=m.session_uid WHERE 1 ` + selfFilter(false)
	var args []any
	if harness != "all" {
		from += `AND s.harness=? `
		args = append(args, harness)
	}
	clause, values := projectPredicate(projects, names)
	from += clause
	args = append(args, values...)
	clause, values = rangePredicate(rng)
	from += clause
	args = append(args, values...)

	aggs := `count(DISTINCT m.session_uid),count(*),` + lastActiveExpr
	var r statsReport
	if e = db.QueryRowContext(ctx, `SELECT '',`+aggs+` `+from, args...).Scan(&r.Total.Value, &r.Total.Sessions, &r.Total.Messages, &r.Total.LastActive); e != nil {
		return r, e
	}
	group := func(col string) ([]statRow, error) {
		return groupLimited(ctx, db, `SELECT `+col+`,`+aggs+` `+from+`GROUP BY 1 ORDER BY 3 DESC,1`, args, statsGroupLimit, scanStat, foldStats)
	}
	if r.Harness, e = group(`s.harness`); e != nil {
		return r, e
	}
	r.Project, e = group(`s.project`)
	return r, e
}

func localStamp(utc string) string {
	t, e := time.Parse(time.RFC3339, utc)
	if e != nil {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func renderStats(r statsReport, asJSON bool) error {
	if asJSON {
		return writeJSON(r)
	}
	fmt.Fprintf(output, "%d sessions · %d messages", r.Total.Sessions, r.Total.Messages)
	if r.Total.Messages == 0 {
		fmt.Fprintln(output)
		return nil
	}
	fmt.Fprintf(output, " · last active %s\n", localStamp(r.Total.LastActive))
	table := func(head string, rows []statRow) {
		w := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "\n%s\tSESSIONS\tMESSAGES\tLAST ACTIVE\n", head)
		for _, s := range rows {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", displayInline(s.Value), s.Sessions, s.Messages, localStamp(s.LastActive))
		}
		w.Flush()
	}
	table("HARNESS", r.Harness)
	table("PROJECT", r.Project)
	return nil
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("kioku stats", flag.ContinueOnError)
	fs.SetOutput(output)
	jsonFlag := fs.Bool("json", false, "print JSON")
	since := fs.String("since", "", "only messages at or after this date")
	until := fs.String("until", "", "only messages before the end of this date")
	harness := fs.String("harness", "all", "all, claude, codex, pi, grok, opencode, cursor or agy")
	var projects projectFlags
	fs.Var(&projects, "project", "filter project basename (repeatable)")
	fs.Var(&projects, "p", "filter project basename (repeatable)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("stats takes no arguments, got %q", strings.Join(fs.Args(), " "))
	}
	setSince, setUntil := false, false
	fs.Visit(func(f *flag.Flag) {
		setSince = setSince || f.Name == "since"
		setUntil = setUntil || f.Name == "until"
	})
	rng, e := resolveRange(*since, *until, setSince, setUntil, time.Now())
	if e != nil {
		return e
	}
	if !validHarness(*harness) {
		return fmt.Errorf("invalid harness %q", *harness)
	}
	db, e := openDB()
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = syncIndex(db, false, nil); e != nil {
		return e
	}
	r, e := collectStats(context.Background(), db, *harness, projects, rng)
	if e != nil {
		return e
	}
	return renderStats(r, *jsonFlag)
}

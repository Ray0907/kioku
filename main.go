package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// version is overridden at release time via -ldflags "-X main.version=..."
var version = "dev"

func init() { version = resolveVersion(version, buildInfo()) }

// resolveVersion falls back to the module version Go records for
// `go install pkg@vX.Y.Z`, which has no ldflags. A local build reports "(devel)".
func resolveVersion(v string, info *debug.BuildInfo) string {
	if v == "dev" && info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return v
}

func buildInfo() *debug.BuildInfo {
	info, _ := debug.ReadBuildInfo()
	return info
}

var output io.Writer = os.Stdout

const usage = `Usage:
  kioku [flags] [query]           Search (TUI on a terminal, compact pages otherwise)
  kioku --sessions [query]        Matching sessions, ranked by best hit
  kioku show <ref|session-id>     Message context or start of session
  kioku index [--rebuild]         Sync sources and print counts
  kioku doctor [--json]           Check sources, index and SQLite without changes
  kioku update [--check]          Install the latest release over this binary
  kioku context                   Where agents left off in this project (for session-start hooks)
  kioku mcp                      Serve read-only MCP tools over stdio
  kioku help                      Show this help

Flags: --json (one JSON page), --limit N (default 10 for pages, 300 for TUI),
       --cursor TOKEN (next page), --harness all|claude|codex|pi|grok|opencode|cursor|agy,
       --project NAME, -p NAME (repeatable; project basename, case-insensitive),
       --context N (show: before/after, default 3), --query Q (center show hit),
       --all-time (search: include older short-prefix matches),
       --include-self (search: include kioku tool calls and results),
       --since D, --until D (search, --sessions: date range in local time; D is
         YYYY-MM-DD, RFC3339, Nd, Nw, today or yesterday; a date as --until
         covers that whole day; an explicit range replaces the short-prefix window),
       --facets (add hit counts per project, harness and week; sessions: project, harness),
       --max-per-session N (search: keep the N best hits of each session),
       --all (show: whole session), --full (show: untruncated hit),
       --no-mouse, --rebuild (index), --version, --help, -h.
Query: space means AND; "black tea" is a phrase; -word excludes;
       bare words are prefixes. Use -- to search flag-like text:
       kioku -- --help (or kioku '"help"' for the word help).
       Single unquoted 1-2 letter Latin prefixes search the 7 days before
       the index's latest timestamp; --all-time removes this restriction.
       Text reports N older matches omitted (--all-time) when applicable.
JSON: {shown,total,total_sessions,hits,next_cursor,omitted_older,facets} for search;
      {shown,total,sessions,next_cursor,omitted_older,facets} for --sessions;
      facets appears only with --facets: {project,harness,week:[{value,count}]};
      omitted_older counts excluded messages and appears only when nonzero;
      {harness,project,topic,cwd,date,resume_cmd,start,end,session_total,hit_index,messages,next_cursor} for show.
      Hit fields: ref,harness,project,age,role,snippet.
      Session fields: ref,harness,project,age,hits,roles{you,asst,tool},best_ref,best,topic.
      Show start,end,hit_index are zero-based like refs; session_total is a count.
      Show message fields: time,role,text,hit,full (when --full).
Environment: KIOKU_INDEX, KIOKU_CLAUDE_DIR, KIOKU_CODEX_DIR,
             KIOKU_PI_DIR, KIOKU_GROK_DIR, KIOKU_OPENCODE_DB, KIOKU_CURSOR_DIR, KIOKU_AGY_DIR,
             KIOKU_EDITOR, KIOKU_THEME=light|dark,
             KIOKU_DEBUG_TIMING=1; CLAUDE_CONFIG_DIR, CODEX_HOME,
             PI_CODING_AGENT_DIR; HOME, XDG_CACHE_HOME, XDG_DATA_HOME, VISUAL.
`

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "kioku:", e)
		os.Exit(1)
	}
}
func timing(label string, started time.Time) {
	if os.Getenv("KIOKU_DEBUG_TIMING") == "1" {
		fmt.Fprintf(os.Stderr, "timing %s=%.2fms\n", label, float64(time.Since(started).Microseconds())/1000)
	}
}

type projectFlags []string

func (p *projectFlags) String() string { return strings.Join(*p, ",") }
func (p *projectFlags) Set(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	*p = append(*p, filepath.Base(name))
	return nil
}

func run() error {
	started := time.Now()
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(args[1:])
	}
	if len(args) > 0 && args[0] == "update" {
		return runUpdate(args[1:])
	}
	if len(args) > 0 && args[0] == "mcp" {
		if len(args) != 1 {
			return fmt.Errorf("mcp takes no arguments")
		}
		return serveMCP(os.Stdin, output)
	}
	if len(args) > 0 && args[0] == "context" {
		return runContext(args[1:])
	}
	if len(args) > 0 && args[0] == "help" {
		fmt.Fprint(output, usage)
		return nil
	}
	index := len(args) > 0 && args[0] == "index"
	show := len(args) > 0 && args[0] == "show"
	if index || show {
		args = args[1:]
	}
	fs := flag.NewFlagSet("kioku", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rebuild := fs.Bool("rebuild", false, "rebuild index")
	jsonFlag := fs.Bool("json", false, "print JSON lines")
	limit := fs.Int("limit", 0, "maximum results")
	cursor := fs.String("cursor", "", "next page token")
	showQuery := fs.String("query", "", "locate the hit in show")
	contextSize := fs.Int("context", 3, "messages before and after")
	allTime := fs.Bool("all-time", false, "include older short-prefix matches")
	includeSelf := fs.Bool("include-self", false, "include kioku tool calls and results")
	all := fs.Bool("all", false, "show full session")
	full := fs.Bool("full", false, "show selected message without truncation")
	sessions := fs.Bool("sessions", false, "group by session")
	since := fs.String("since", "", "only messages at or after this date")
	until := fs.String("until", "", "only messages before the end of this date")
	facets := fs.Bool("facets", false, "add hit counts per project, harness and week")
	maxPerSession := fs.Int("max-per-session", 0, "keep at most N hits per session")
	noMouse := fs.Bool("no-mouse", false, "disable mouse")
	harness := fs.String("harness", "all", "all, claude, codex, pi, grok, opencode, cursor or agy")
	var projects projectFlags
	fs.Var(&projects, "project", "filter project basename (repeatable)")
	fs.Var(&projects, "p", "filter project basename (repeatable)")
	ver := fs.Bool("version", false, "print version")
	// Flags may precede or follow the query.
	valued := map[string]bool{"--limit": true, "--cursor": true, "--context": true, "--query": true, "--project": true, "-p": true, "--since": true, "--until": true, "--max-per-session": true}
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if a == "--help" || a == "-h" {
			fmt.Fprint(output, usage)
			return nil
		}
		if valued[a] {
			if i+1 == len(args) {
				return fmt.Errorf("%s requires a value", a)
			}
			if e := fs.Parse(args[i : i+2]); e != nil {
				return e
			}
			i++
			continue
		}
		if name, _, ok := strings.Cut(a, "="); ok && valued[name] {
			if e := fs.Parse([]string{a}); e != nil {
				return e
			}
			continue
		}
		if a == "--harness" && i+1 < len(args) {
			*harness = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--harness=") {
			*harness = strings.TrimPrefix(a, "--harness=")
			continue
		}
		switch a {
		case "--rebuild", "--json", "--no-mouse", "--version", "--sessions", "--all-time", "--include-self", "--all", "--full", "--facets":
			if e := fs.Parse([]string{a}); e != nil {
				return e
			}
		default:
			words = append(words, a)
		}
	}
	if *ver {
		fmt.Fprintln(output, version)
		return nil
	}
	setLimit, setSince, setUntil, setCap := false, false, false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "limit":
			setLimit = true
		case "since":
			setSince = true
		case "until":
			setUntil = true
		case "max-per-session":
			setCap = true
		}
	})
	rng, e := resolveRange(*since, *until, setSince, setUntil, time.Now())
	if e != nil {
		return e
	}
	// The TUI has no range, facet or cap controls; never launch it with flags it would ignore.
	filtered := rng.active() || *facets || setCap
	oneShot := show || *sessions || *jsonFlag || *cursor != "" || filtered || !isatty.IsTerminal(os.Stdout.Fd())
	if !setLimit {
		if oneShot {
			*limit = 10
		} else {
			*limit = 300
		}
	}
	if *limit < 1 {
		return fmt.Errorf("--limit must be a positive integer")
	}
	if *contextSize < 0 {
		return fmt.Errorf("--context must be nonnegative")
	}
	if (show || index) && len(projects) > 0 {
		return fmt.Errorf("--project applies to search and --sessions")
	}
	if (show || index) && filtered {
		return fmt.Errorf("--since, --until, --facets and --max-per-session apply to search and --sessions")
	}
	if setCap && (*maxPerSession < 1 || *sessions) {
		return fmt.Errorf("--max-per-session must be a positive integer and applies to search, not --sessions")
	}
	if show && (*sessions || len(words) != 1) {
		return fmt.Errorf("show requires one reference (and cannot use --sessions)")
	}
	if !show && *all {
		return fmt.Errorf("--all requires show")
	}
	if !show && *full {
		return fmt.Errorf("--full requires show")
	}
	if !show && *showQuery != "" {
		return fmt.Errorf("--query requires show")
	}
	if *harness != "all" && *harness != "claude" && *harness != "codex" && *harness != "pi" && *harness != "grok" && *harness != "opencode" && *harness != "cursor" && *harness != "agy" {
		return fmt.Errorf("invalid harness %q", *harness)
	}
	db, e := openDB()
	if e != nil {
		return e
	}
	defer db.Close()
	timing("startup", started)
	if index {
		syncStart := time.Now()
		s, e := syncIndex(db, *rebuild, nil)
		timing("sync", syncStart)
		if e != nil {
			return e
		}
		for _, h := range []string{"claude", "codex", "pi", "grok", "opencode", "cursor", "agy"} {
			fmt.Fprintf(output, "%s: %d messages\n", h, s.Messages[h])
		}
		fmt.Fprintf(output, "%d files, %d changed, %d skipped\n", s.Files, s.Changed, s.Skipped)
		return nil
	}
	q := strings.Join(words, " ")
	if oneShot {
		syncStart := time.Now()
		_, e = syncIndex(db, false, nil)
		timing("sync", syncStart)
		if e != nil {
			return e
		}
		mode := "hits"
		if *sessions {
			mode = "sessions"
		}
		if show {
			mode = "show"
			q = *showQuery
		}
		key := pageKey{Mode: mode, Query: q, Harness: *harness, Projects: projects, Limit: *limit, Context: *contextSize, JSON: *jsonFlag, AllTime: *allTime && shortLatin(q), IncludeSelf: *includeSelf, All: *all, Full: *full, NoMouse: *noMouse, Rebuild: *rebuild, Since: rng.Since, Until: rng.Until, Facets: *facets, MaxPerSession: *maxPerSession}
		if show {
			key.Ref = words[0]
		}
		offset, err := key.offset(*cursor)
		if err != nil {
			return err
		}
		ctx := context.Background()
		switch mode {
		case "show":
			page, err := compactShow(ctx, db, key, offset)
			if err != nil {
				return err
			}
			return renderShow(page, *jsonFlag)
		case "sessions":
			page, err := compactSessions(ctx, db, key, offset)
			if err != nil {
				return err
			}
			return renderSessions(page, *jsonFlag)
		default:
			page, err := compactSearch(ctx, db, key, offset)
			if err != nil {
				return err
			}
			return renderHits(page, *jsonFlag, q)
		}
	}
	rows, e := search(context.Background(), db, q, *harness, projects, *limit, *allTime, *includeSelf)
	if e != nil {
		return e
	}
	every, e := parseSyncEvery(os.Getenv("KIOKU_SYNC_EVERY"))
	if e != nil {
		return e
	}
	syncEvery = every
	m := newModel(db, q, *harness, projects, rows, !*noMouse, *limit, *includeSelf)
	m.allTime = *allTime
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if !*noMouse {
		options = append(options, tea.WithMouseCellMotion())
	}
	final, e := tea.NewProgram(m, options...).Run()
	if e != nil {
		return e
	}
	fm := final.(model)
	if fm.resume != nil {
		return execSession(*fm.resume)
	}
	return nil
}
func execSession(h hit) error {
	if e := os.Chdir(h.CWD); e != nil {
		return e
	}
	var name string
	var args []string
	switch h.Harness {
	case "claude":
		name = "claude"
		args = []string{"claude", "--resume", h.SessionID}
	case "codex":
		name = "codex"
		args = []string{"codex", "resume", h.SessionID}
	case "opencode":
		name = "opencode"
		args = []string{"opencode", "--session", h.SessionID}
	case "agy":
		name = "agy"
		args = []string{"agy", "--conversation", h.SessionID}
	case "cursor":
		name = "cursor-agent"
		args = []string{"cursor-agent", "--resume", h.SessionID}
	case "grok":
		fmt.Fprintln(output, h.ResumeCmd)
		return nil
	case "pi":
		name = "pi"
		args = []string{"pi", "--session", h.Path}
	}
	bin, e := exec.LookPath(name)
	if e != nil {
		return e
	}
	return syscall.Exec(bin, args, os.Environ())
}

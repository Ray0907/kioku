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

func usage() string {
	var envs []string
	for _, h := range harnesses {
		envs = append(envs, h.env)
	}
	return fmt.Sprintf(`Usage:
  kioku [flags] [query]           Search (TUI on a terminal, compact pages otherwise)
  kioku --sessions [query]        Matching sessions, ranked by best hit
  kioku show <ref|session-id>     Message context or start of session (--format md for Markdown)
  kioku index [--rebuild]         Sync sources and print counts
  kioku doctor [--json]           Check sources, index and SQLite without changes
  kioku update [--check]          Install the latest release over this binary
  kioku stats [flags]             Sessions, messages and last activity per harness and project
  kioku context                   Where agents left off in this project (for session-start hooks)
  kioku mcp                      Serve read-only MCP tools over stdio
  kioku mcp install [agent...] [--yes] [--dry-run]  Register with claude, codex, pi, grok
  kioku help                      Show this help

Flags: --json (one JSON page), --limit N (default 10 for pages, 300 for TUI),
       --cursor TOKEN (next page), --harness %s,
       --project NAME, -p NAME (repeatable; project basename, case-insensitive),
       --context N (show: before/after, default 3), --query Q (center show hit),
       --all-time (search: include older short-prefix matches),
       --include-self (search: include kioku tool calls and results),
       --include-automated (search: include automated review sessions),
       --role user|asst|tool (search, --sessions: comma-separated roles; prints results),
       --model TEXT (search, --sessions, stats: case-insensitive model substring),
       --since D, --until D (search, --sessions: date range in local time; D is
         YYYY-MM-DD, RFC3339, Nd, Nw, today or yesterday; a date as --until
         covers that whole day; an explicit range replaces the short-prefix window),
       --facets (add hit counts per project, harness and week; sessions: project, harness),
       --max-per-session N (search: keep the N best hits of each session),
       --all (show: whole session), --full (show: untruncated hit),
       --outline (show: user messages and each turn's last reply, no tools),
       --format md (show: Markdown to paste into an issue or PR; not with --json),
       --no-redact (CLI: show recognised credentials instead of ****),
       --no-mouse, --rebuild (index), --version, --help, -h.
Stats: kioku stats [--since D] [--until D] [-p NAME] [--harness H] [--model TEXT] [--json]
       counts messages in the range and the sessions that have them, per harness
       and per project (20 projects, the rest summed into (other)), with the last
       active time in local time. kioku's own tool calls are not counted.
Query: space means AND; "black tea" is a phrase; -word excludes;
       bare words are prefixes. Use -- to search flag-like text:
       kioku -- --help (or kioku '"help"' for the word help).
       Single unquoted 1-2 letter Latin prefixes search the 7 days before
       the index's latest timestamp; --all-time removes this restriction.
       Text reports N older matches omitted (--all-time) when applicable.
JSON: stats is {total,harness,project}; each row is {value,sessions,messages,last_active}.
      {shown,total,total_sessions,hits,next_cursor,omitted_older,facets} for search;
      {shown,total,sessions,next_cursor,omitted_older,facets} for --sessions;
      facets appears only with --facets: {project,harness,week:[{value,count}]};
      omitted_older counts excluded messages and appears only when nonzero;
      {harness,project,topic,cwd,date,resume_cmd,start,end,session_total,hit_index,messages,next_cursor} for show.
      Hit fields: ref,harness,project,age,role,snippet.
      Session fields: ref,harness,project,age,hits,roles{you,asst,tool},best_ref,best,topic.
      Show start,end,hit_index are zero-based like refs; session_total is a count.
      Show message fields: time,role,text,hit,full (when --full).
Environment: KIOKU_INDEX, %s,
             %s,
             KIOKU_EDITOR, KIOKU_THEME=light|dark,
             KIOKU_DEBUG_TIMING=1; CLAUDE_CONFIG_DIR, CODEX_HOME,
             PI_CODING_AGENT_DIR; HOME, XDG_CACHE_HOME, XDG_DATA_HOME, VISUAL.
`, strings.Join(harnessNames(), "|"), strings.Join(envs[:2], ", "), strings.Join(envs[2:], ", "))
}

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
		if len(args) > 1 && args[1] == "install" {
			return runMCPInstall(args[2:])
		}
		if len(args) != 1 {
			return fmt.Errorf("mcp takes no arguments")
		}
		return serveMCP(os.Stdin, output)
	}
	if len(args) > 0 && args[0] == "stats" {
		return runStats(args[1:])
	}
	if len(args) > 0 && args[0] == "context" {
		return runContext(args[1:])
	}
	if len(args) > 0 && args[0] == "help" {
		fmt.Fprint(output, usage())
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
	here := fs.Bool("here", false, "scope to the current project directory")
	allTime := fs.Bool("all-time", false, "include older short-prefix matches")
	includeSelf := fs.Bool("include-self", false, "include kioku tool calls and results")
	includeAutomated := fs.Bool("include-automated", false, "include automated review sessions")
	role := fs.String("role", "", "filter roles: user, asst or tool (comma-separated)")
	modelFlag := fs.String("model", "", "filter model by case-insensitive substring")
	all := fs.Bool("all", false, "show full session")
	full := fs.Bool("full", false, "show selected message without truncation")
	outline := fs.Bool("outline", false, "show user messages and each turn's last reply, no tools")
	sessions := fs.Bool("sessions", false, "group by session")
	format := fs.String("format", "text", "show output: text or md")
	since := fs.String("since", "", "only messages at or after this date")
	until := fs.String("until", "", "only messages before the end of this date")
	facets := fs.Bool("facets", false, "add hit counts per project, harness and week")
	maxPerSession := fs.Int("max-per-session", 0, "keep at most N hits per session")
	noRedact := fs.Bool("no-redact", false, "do not redact credentials in output")
	noMouse := fs.Bool("no-mouse", false, "disable mouse")
	harness := fs.String("harness", "all", harnessFlagHelp())
	var projects projectFlags
	fs.Var(&projects, "project", "filter project basename (repeatable)")
	fs.Var(&projects, "p", "filter project basename (repeatable)")
	ver := fs.Bool("version", false, "print version")
	// Flags may precede or follow the query.
	valued := map[string]bool{"--limit": true, "--cursor": true, "--context": true, "--query": true, "--project": true, "-p": true, "--since": true, "--until": true, "--max-per-session": true, "--format": true, "--role": true, "--model": true}
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if a == "--help" || a == "-h" {
			fmt.Fprint(output, usage())
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
		case "--rebuild", "--json", "--no-mouse", "--no-redact", "--version", "--sessions", "--here", "--all-time", "--include-self", "--include-automated", "--all", "--full", "--outline", "--facets":
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
	setLimit, setSince, setUntil, setCap, setRole, setModel := false, false, false, false, false, false
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
		case "role":
			setRole = true
		case "model":
			setModel = true
		}
	})
	if setModel && *modelFlag == "" {
		return fmt.Errorf("--model cannot be empty")
	}
	var roles []string
	if setRole {
		var e error
		if roles, e = parseRoles(*role); e != nil {
			return e
		}
	}
	rng, e := resolveRange(*since, *until, setSince, setUntil, time.Now())
	if e != nil {
		return e
	}
	var root string
	if *here {
		if show || index {
			return fmt.Errorf("--here applies to search and --sessions")
		}
		root, e = scopeRoot(".")
		if e != nil {
			return e
		}
	}
	// The TUI has no range, facet, cap, role or model flags; never launch it with filters it would ignore.
	filtered := rng.active() || *facets || setCap || setRole || setModel
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
	if (show || index) && setModel {
		return fmt.Errorf("--model applies to search and --sessions")
	}
	if (show || index) && filtered {
		return fmt.Errorf("--since, --until, --facets, --max-per-session and --role apply to search and --sessions")
	}
	if setCap && (*maxPerSession < 1 || *sessions) {
		return fmt.Errorf("--max-per-session must be a positive integer and applies to search, not --sessions")
	}
	if show && (*sessions || len(words) != 1) {
		return fmt.Errorf("show requires one reference (and cannot use --sessions)")
	}
	if *format != "text" && *format != "md" {
		return fmt.Errorf("--format must be text or md")
	}
	if *format == "md" && (!show || *jsonFlag) {
		return fmt.Errorf("--format md applies to show and cannot be combined with --json")
	}
	if !show && *all {
		return fmt.Errorf("--all requires show")
	}
	if !show && *full {
		return fmt.Errorf("--full requires show")
	}
	if !show && *outline {
		return fmt.Errorf("--outline requires show")
	}
	if !show && *showQuery != "" {
		return fmt.Errorf("--query requires show")
	}
	if !validHarness(*harness) {
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
		for _, warning := range s.Warnings {
			fmt.Fprintln(os.Stderr, "kioku: "+warning)
		}
		timing("sync", syncStart)
		if e != nil {
			return e
		}
		for _, h := range harnesses {
			fmt.Fprintf(output, "%s: %d messages\n", h.name, s.Messages[h.name])
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
		key := pageKey{Mode: mode, Query: q, Harness: *harness, Projects: projects, Roles: roles, Model: *modelFlag, Root: root, Limit: *limit, Context: *contextSize, JSON: *jsonFlag, AllTime: *allTime && shortLatin(q), IncludeSelf: *includeSelf, IncludeAutomated: *includeAutomated, All: *all, Full: *full, Outline: *outline, NoMouse: *noMouse, Rebuild: *rebuild, Since: rng.Since, Until: rng.Until, Facets: *facets, MaxPerSession: *maxPerSession}
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
			page, err := compactShow(ctx, db, key, offset, *noRedact)
			if err != nil {
				return err
			}
			if *format == "md" {
				return renderShowMD(page)
			}
			return renderShow(page, *jsonFlag)
		case "sessions":
			page, err := compactSessions(ctx, db, key, offset, *noRedact)
			if err != nil {
				return err
			}
			return renderSessions(page, *jsonFlag)
		default:
			page, err := compactSearch(ctx, db, key, offset, *noRedact)
			if err != nil {
				return err
			}
			if !*noRedact {
				q = redact(q)
			}
			return renderHits(page, *jsonFlag, q)
		}
	}
	rows, e := search(context.Background(), db, q, *harness, projects, *limit, *allTime, *includeSelf, *includeAutomated, timeRange{}, root)
	if e != nil {
		return e
	}
	every, e := parseSyncEvery(os.Getenv("KIOKU_SYNC_EVERY"))
	if e != nil {
		return e
	}
	syncEvery = every
	m := newModel(db, q, *harness, projects, rows, !*noMouse, *limit, *includeSelf)
	m.root = root
	m.allTime = *allTime
	m.includeAutomated = *includeAutomated
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
	args := []string{""}
	if findHarness(h.Harness) != nil {
		args = resumeArgv(h.Harness, h.SessionID, h.Path, h.CWD)
	}
	if args[0] == "cd" {
		fmt.Fprintln(output, h.ResumeCmd)
		return nil
	}
	bin, e := exec.LookPath(args[0])
	if e != nil {
		return e
	}
	return syscall.Exec(bin, args, os.Environ())
}

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

	tea "charm.land/bubbletea/v2"
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
	opts := newFlagSet()
	fs := opts.fs
	words, help, e := parseArgs(fs, args)
	if e != nil {
		return e
	}
	if help {
		fmt.Fprint(output, usage())
		return nil
	}
	if *opts.ver {
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
	if setModel && *opts.modelFlag == "" {
		return fmt.Errorf("--model cannot be empty")
	}
	var roles []string
	if setRole {
		var e error
		if roles, e = parseRoles(*opts.role); e != nil {
			return e
		}
	}
	rng, e := resolveRange(*opts.since, *opts.until, setSince, setUntil, time.Now())
	if e != nil {
		return e
	}
	var root string
	if *opts.here {
		if show || index {
			return fmt.Errorf("--here applies to search and --sessions")
		}
		root, e = scopeRoot(".")
		if e != nil {
			return e
		}
	}
	// The TUI has no range, facet, cap, role or model flags; never launch it with filters it would ignore.
	filtered := rng.active() || *opts.facets || setCap || setRole || setModel
	oneShot := show || *opts.sessions || *opts.jsonFlag || *opts.cursor != "" || filtered || !isatty.IsTerminal(os.Stdout.Fd())
	if !setLimit {
		if oneShot {
			*opts.limit = 10
		} else {
			*opts.limit = 300
		}
	}
	if *opts.limit < 1 {
		return fmt.Errorf("--limit must be a positive integer")
	}
	if *opts.contextSize < 0 {
		return fmt.Errorf("--context must be nonnegative")
	}
	if (show || index) && len(opts.projects) > 0 {
		return fmt.Errorf("--project applies to search and --sessions")
	}
	if (show || index) && setModel {
		return fmt.Errorf("--model applies to search and --sessions")
	}
	if (show || index) && filtered {
		return fmt.Errorf("--since, --until, --facets, --max-per-session and --role apply to search and --sessions")
	}
	if setCap && (*opts.maxPerSession < 1 || *opts.sessions) {
		return fmt.Errorf("--max-per-session must be a positive integer and applies to search, not --sessions")
	}
	if show && (*opts.sessions || len(words) != 1) {
		return fmt.Errorf("show requires one reference (and cannot use --sessions)")
	}
	if *opts.format != "text" && *opts.format != "md" {
		return fmt.Errorf("--format must be text or md")
	}
	if *opts.format == "md" && (!show || *opts.jsonFlag) {
		return fmt.Errorf("--format md applies to show and cannot be combined with --json")
	}
	if !show && *opts.all {
		return fmt.Errorf("--all requires show")
	}
	if !show && *opts.full {
		return fmt.Errorf("--full requires show")
	}
	if !show && *opts.outline {
		return fmt.Errorf("--outline requires show")
	}
	if !show && *opts.showQuery != "" {
		return fmt.Errorf("--query requires show")
	}
	if !validHarness(*opts.harness) {
		return fmt.Errorf("invalid harness %q", *opts.harness)
	}
	db, e := openDB()
	if e != nil {
		return e
	}
	defer db.Close()
	timing("startup", started)
	if index {
		syncStart := time.Now()
		s, e := syncIndex(db, *opts.rebuild, nil)
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
		if *opts.sessions {
			mode = "sessions"
		}
		if show {
			mode = "show"
			q = *opts.showQuery
		}
		key := pageKey{Mode: mode, Query: q, Harness: *opts.harness, Projects: opts.projects, Roles: roles, Model: *opts.modelFlag, Root: root, Limit: *opts.limit, Context: *opts.contextSize, JSON: *opts.jsonFlag, AllTime: *opts.allTime && shortLatin(q), IncludeSelf: *opts.includeSelf, IncludeAutomated: *opts.includeAutomated, All: *opts.all, Full: *opts.full, Outline: *opts.outline, NoMouse: *opts.noMouse, Rebuild: *opts.rebuild, Since: rng.Since, Until: rng.Until, Facets: *opts.facets, MaxPerSession: *opts.maxPerSession}
		if show {
			key.Ref = words[0]
		}
		offset, err := key.offset(*opts.cursor)
		if err != nil {
			return err
		}
		ctx := context.Background()
		switch mode {
		case "show":
			page, err := compactShow(ctx, db, key, offset, *opts.noRedact)
			if err != nil {
				return err
			}
			if *opts.format == "md" {
				return renderShowMD(page)
			}
			return renderShow(page, *opts.jsonFlag)
		case "sessions":
			page, err := compactSessions(ctx, db, key, offset, *opts.noRedact)
			if err != nil {
				return err
			}
			return renderSessions(page, *opts.jsonFlag)
		default:
			page, err := compactSearch(ctx, db, key, offset, *opts.noRedact)
			if err != nil {
				return err
			}
			if !*opts.noRedact {
				q = redact(q)
			}
			return renderHits(page, *opts.jsonFlag, q)
		}
	}
	rows, e := search(context.Background(), db, pageKey{Query: q, Harness: *opts.harness, Projects: opts.projects, Limit: *opts.limit, AllTime: *opts.allTime, IncludeSelf: *opts.includeSelf, IncludeAutomated: *opts.includeAutomated, Root: root})
	if e != nil {
		return e
	}
	every, e := parseSyncEvery(os.Getenv("KIOKU_SYNC_EVERY"))
	if e != nil {
		return e
	}
	syncEvery = every
	m := newModel(db, q, *opts.harness, opts.projects, rows, !*opts.noMouse, *opts.limit, *opts.includeSelf)
	m.root = root
	m.allTime = *opts.allTime
	m.includeAutomated = *opts.includeAutomated
	final, e := tea.NewProgram(m).Run()
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

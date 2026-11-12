# Changelog

## v0.11.0 - 2026-11-12

### Added
- `--model TEXT` (MCP `kioku_search` and `kioku_sessions` `model`) keeps only sessions whose model name contains the text, ignoring case. It applies to search, `--sessions`, counts, facets, paging and `kioku stats`.
- `--here` limits search, `--sessions`, counts, facets, paging and `kioku stats` to the current project: the nearest parent directory that holds a `.git` entry, or the working directory when there is none. The home directory and `/` are rejected. MCP `kioku_search` and `kioku_sessions` take `cwd` for the same scope. In the TUI, `p` turns the scope on and off and the header shows the project name while it is on.
- `kioku mcp install [agent...] [--yes] [--dry-run]` registers the kioku MCP server with Claude Code, Codex, Pi and Grok through each agent's own `mcp add` command. With no agent named it uses every known agent on `PATH` and asks once before running.
- Hermes Agent sessions are indexed (harness `hermes`) from `KIOKU_HERMES_DB`, `$HERMES_HOME/state.db` or `~/.hermes/state.db`, read-only. Only sessions that have a working directory and no parent session are indexed, so gateway chats, scheduled jobs and subagents are left out. Resume runs `hermes --resume <id>`.

### Changed
- One-shot text and `--json` pages, `--sessions` topics, `kioku show`, `kioku context` and every MCP tool result now replace recognised credentials with `****`: PEM private keys, `Authorization: Bearer` tokens, `sk-` keys, GitHub, Slack and Stripe live tokens, AWS access key ids, Google API keys and JSON Web Tokens. `--no-redact` turns this off for one CLI run; MCP has no way to turn it off. The TUI, the index and `kioku stats` are unchanged.

## v0.10.4 - 2026-11-12

### Security
- Built with Go 1.26.11, which fixes 25 standard-library vulnerabilities reported by `govulncheck` (`net/http`, `crypto/tls`, `crypto/x509`, `archive/tar` and others) that `kioku update` could reach. `golang.org/x/text` and `golang.org/x/sys` are updated to their fixed versions. `govulncheck` now reports none.

## v0.10.3 - 2026-11-11

### Changed
- fts5-cjk now comes from its Go module (`github.com/Ray0907/fts5-cjk` v0.3.0) instead of a copied `cjk.c`; update it with `go get github.com/Ray0907/fts5-cjk@latest`. The tokenizer handles run-edge characters and kana and Hangul composition differently, so the index moves to schema version 5 and rebuilds once on first run.

## v0.10.2 - 2026-11-11

v0.10.0 and v0.10.1 were tagged but not published: the new CI test gate failed, first because the e2e suite ran with `CI` set (which turns off TUI colors), then because shared runners missed the local 200 ms latency budget. The suite now unsets `CI`, and CI uses a 600 ms budget (`KIOKU_E2E_P95_MS`). v0.10.2 contains all of the changes below.

### Added
- `--role user|asst|tool` (a comma list is allowed; MCP `kioku_search` `role`) limits search, `--sessions`, counts, facets and paging to those message roles. An invalid role is an error.
- `kioku show <ref> --outline` and MCP `kioku_show` `outline: true` return each user message with the complete final assistant reply of its turn and skip tool rows. MCP `kioku_show` also takes `limit` (default 10, at most 100).
- MCP `kioku_context` returns the same text as `kioku context`, so agents without the SessionStart hook can ask for it. Index failures are reported as tool errors.
- TUI: PgDn / PgUp scroll the transcript by a page and the mouse wheel over it moves three lines, so a message taller than the pane can be read to its end. Ctrl-W deletes the previous word in the query (CJK runs stay whole).
- CI runs `go vet`, the unit tests and all three e2e suites on every push and pull request; the release workflow runs them before building.

### Changed
- Sessions whose first user message starts with an automated review prompt (`Review this change`, `# Review Guidelines`) are left out of search, `--sessions`, the TUI, totals and facets. `--include-automated` (MCP `include_automated`) brings them back; `show` still opens them. The index moves to schema version 4 and rebuilds once on first run, then runs VACUUM to return free pages.
- TUI: when a search matches more than the 300 loaded rows, the header reads `300 of N messages · S sessions`.
- TUI: the help screen lists every key, including `d` / `D`, and its footer names the keys that close it.
- `kioku doctor` shows how many Antigravity sessions were skipped and the first error. It reads them from private snapshots and never writes to the source stores.
- Agent CLI and MCP documentation moved from the README to `docs/agents.md`.

### Fixed
- TUI: an empty query with a date range never finished on a large index and kept showing stale rows. The range now filters each session's last message, the same as the CLI, and the TUI and CLI build their search SQL from the same code.
- TUI: source warnings from a background sync were printed to stderr and pushed the screen up. They now appear only in `kioku index` output.

## v0.9.1 - 2026-11-06

### Fixed
- TUI: the space key was dropped in the query and in the highlight and range prompts, so `abc def` could not be typed.

## v0.9.0 - 2026-11-06

### Changed
- TUI: the query bar sits on a tinted band, agent glyphs take a per-harness colour (red stays for hits), and unselected rows dim the project. The list selection band shows only while focus is in the list; with the cursor in the query box the selected row keeps a quiet `›`. Transcript messages are separated by a blank line and marked by a speaker rule (`┃` you, `│` asst). Footer keys render as caps.
- TUI: list snippets drop leading bullets, heading marks, bold markers and table pipes (display only).

### Added
- TUI: `t` unfolds or folds tool output. Tool messages are folded to one line with a `(+N lines)` count unless selected or matching the query.

## v0.8.0 - 2026-11-02

### Changed
- `stats` is now a subcommand like `doctor` and `context`, so a bare `kioku stats` no longer searches for that word; use `kioku -- stats` or `kioku '"stats"'`.

### Added
- `--since` / `--until` (CLI) and `since` / `until` (MCP `kioku_search`, `kioku_sessions`) limit a search to a date range. Values are `YYYY-MM-DD`, RFC3339, `Nd`, `Nw`, `today` or `yesterday` in local time; a date as `--until` covers that whole day. An explicit range replaces the 7-day window for short Latin prefixes. Invalid values and `--since` after `--until` are errors. On a terminal these flags print a page instead of opening the TUI; set the TUI's range with `d`.
- `--facets` / `facets`: hit counts per project, harness and week (local time, Monday start, so a week bucket matches the same `--since`/`--until` dates) for the same search, so an agent can narrow the next query. `--sessions` returns project and harness counts. Lists past 20 projects or 52 weeks are summed into an `(other)` entry, so counts always add up to the total. Off by default.
- `--max-per-session N` / `max_per_session`: keep at most N hits of each session. Unlimited by default.
- `kioku show <ref> --format md` prints the same window as Markdown (heading, metadata, resume command, each message quoted, the hit marked) for pasting into an issue or PR. Text and `--json` output are unchanged. `--format` other than `text` or `md`, or `md` with `--json` or outside `show`, is an error.
- `kioku stats [--since D] [--until D] [-p NAME] [--harness H] [--json]`: sessions, messages and last active time per harness and per project, with a total line. Counts only messages inside the range (same `--since`/`--until` rules as search) and kioku's own tool calls are left out. Projects past 20 are summed into `(other)`.
- TUI: `d` (in the results) asks for a date range as `since..until`, e.g. `7d`, `yesterday` or `2026-10-01..2026-10-07`; either side may be empty and a lone value is a `since`. Values mean what they mean for `--since`/`--until` (local time, a date as the end covers that whole day, and an explicit range replaces the short-prefix window). The header shows the active range, `D` or an empty value clears it, and an invalid value keeps the current range and says why. Empty results in a range say so.
- `test/range_e2e.sh` checks these features and compares default output with v0.7.0; it writes `test/range-e2e-result.json`.
- `test/views_e2e.sh` checks `show --format md` against the previous commit's text and JSON output, `stats` against a direct count of the index, and the TUI range in a tmux session on the demo home (its counts must equal the CLI's for the same range, in two time zones); it writes `test/views-e2e-result.json`.

## v0.7.0 - 2026-10-24

### Added
- Antigravity CLI (`agy`) sessions are indexed and searchable with `--harness agy`; resume runs `agy --conversation <id>`.
- `kioku context`: prints where your agents left off in the current project (up to 5 sessions from the last 14 days, each with its last question and the first line of its last reply; every agent gets its latest session in first). Prints nothing when there is no history. The Claude Code plugin runs it from a SessionStart hook (`startup|clear`) so a new session starts knowing the project's recent work. Sessions another agent opened to review a change are left out.

## v0.6.0 - 2026-10-13

### Added
- `o` supports terminal editors (vim, nvim, vi, nano, hx, helix, micro, kak) when named by `KIOKU_EDITOR` or `VISUAL`, suspending the TUI until they exit; terminal editors are not auto-detected.
- `r` in the results syncs the index and re-runs the search, so sessions written while the TUI is open can be found without restarting. Optional background sync: set `KIOKU_SYNC_EVERY` (e.g. `30s`, minimum `5s`; off by default). New messages are counted in the header (`N new · r refreshes`) without moving the results; background syncs show no progress and ignore errors.

## v0.5.0 - 2026-10-12

### Added
- `kioku update [--check]`: install the latest release over the running binary. It verifies the archive against the release's `SHA256SUMS`, replaces the binary atomically, and leaves it untouched on any failure.
  Homebrew installs are pointed to `brew upgrade kioku` instead.

## v0.4.1 - 2026-10-12

### Fixed
- `kioku --version` and the MCP server info report the module version for binaries installed with `go install ...@vX.Y.Z` instead of `dev`.

## v0.4.0 - 2026-10-12

### Added
- `kioku doctor`: read-only source, index and SQLite/FTS5/CJK diagnostics, optional missing sources, JSON output and failure exit codes.
- `kioku mcp`: stdio MCP server (2025-06-18) with read-only search, sessions and show tools, compact JSON pages, cursor support and recoverable tool/protocol errors; no new dependencies.
- Claude Code plugin and marketplace packaging, using the existing kioku skill and MCP server, plus agent setup examples in the README.
- Mock-only MCP E2E coverage and a repeatable `test/mcp-e2e-result.json` artifact.

### Fixed
- The TUI keeps the current transcript while the next session loads instead of blanking it, and skips the reload when the same session under the same query is already shown, so moving the selection and re-queries after indexing no longer flicker.

## v0.3.1 - 2026-10-09

### Changed
- The transcript pane renders only the messages that reach the screen, starting from the selected one. A frame on a 140x45 terminal drops from about 6 ms to 0.8 ms (folded view) and from about 10 ms to 1 ms (full view of a long session), with the same visible output.

### Added
- `demo/record.sh` records the README demo GIF with VHS from the synthetic demo home.

## v0.3.0 - 2026-10-09

### Added
- macOS AppKit app in `app/`, targeting macOS 14 with the macOS 26 SDK: bundled Go engine, two-pane message search/conversation preview, agent counts, project filter, session mode, folded context and highlighted hits.
- Resume in a configurable terminal, copy resume command, and open project in a configurable editor. Finder-style Open With menus use Launch Services, installed known apps and an Other app picker, with persistent bundle-id choices.
- Liquid Glass chrome on macOS 26; visual-effect/solid fallbacks, live accessibility modes, mouse-down feedback, cancellable searches, 30 ms debounce, stale-while-revalidate results and interruptible, critically damped layer transitions.
- XCUITest-only app E2E suite: fixture HOME, launch recorder stubs, search/filter/order/preview/copy/resume checks, preference persistence, race/held-arrow/budget checks and accessibility-mode screenshots.
- JSON `resume_argv`, UTF-16 `highlights`, and conversation-message `matches` metadata, reusing Go query/display logic.

### Changed
- Minimal TUI layout: a single-color palette (text, gray, red for hits only), a one-cell agent mark at the start of each row (`✻` claude, `◇` codex, `π` pi, `✕` grok, `▢` opencode, `↖` cursor), project and age columns, and no rules, color bands or dimming.
- The detail pane has a tinted title bar showing the agent, project, date, path and `result N of M · K hits here`. The messages before and after each hit appear as gray context, and the rest fold into `⋯ N hidden · v shows all`.
- Hit words are bold, red and underlined on every row, not only the selected one.
- The footer shows five keys in results mode and four while searching; the `?` help lists the agent marks.
- `Makefile` pins the macOS 26 SDK when it is installed, because the macOS 27 command line tools SDK breaks the cgo link.

### Fixed
- Quote unsafe resume arguments and preserve raw JSON cwd paths; shell quoting preserves non-printing characters without changing ordinary text command formatting.

## v0.2.0 - 2026-10-03

### Added
- Linux support (arm64 and amd64), with `xdg-open` for projects and `wl-copy`/`xclip`/`xsel` for clipboard access.
- Index Grok (`~/.grok/sessions`), OpenCode (SQLite, v1 and v2 layouts, read-only) and Cursor CLI (`~/.cursor/chats`) sessions. Filter with `--harness grok|opencode|cursor`; `tab` in the TUI cycles them.
- `--include-self` to include kioku's own tool calls and results, which are now hidden from search by default.
- Native config directories are honored: `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `PI_CODING_AGENT_DIR`. `KIOKU_*_DIR` still wins; new `KIOKU_GROK_DIR`, `KIOKU_OPENCODE_DB`, `KIOKU_CURSOR_DIR`.

### Changed
- A missing explicit source root now indexes nothing for that harness instead of falling back to its default.
- Existing indexes are reparsed once to mark kioku's own tool calls.

### Known limits
- Grok has no resume flag: `resume:` prints `cd <cwd>` only.
- Cursor CLI message order is approximate (rowid), and a session whose `store.db` disappears keeps its cached messages.

## v0.1.3
See the [release notes](https://github.com/Ray0907/kioku/releases/tag/v0.1.3).

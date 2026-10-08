# Changelog

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

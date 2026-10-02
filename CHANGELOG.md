# Changelog

## Unreleased

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

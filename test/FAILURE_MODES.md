# E2E failure modes

Use a fresh synthetic `HOME` from `./test/fixture.sh` for every run. Run all commands from a working directory that is not the real project/session cwd. The real-store lower-bound sanity is opt-in (`KIOKU_E2E_REAL=1`); default suite runs never inspect real stores. These are user-visible failures, not implementation suggestions.

## Discovery, parsing, and privacy

- [ ] Indexes Claude Code files at `$HOME/.claude/projects/*/*.jsonl`, Codex rollouts at `$HOME/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, and Pi sessions at `$HOME/.pi/agent/sessions/<dir>/*.jsonl`.
- [ ] Honors `$HOME` and `KIOKU_CLAUDE_DIR`, `KIOKU_CODEX_DIR`, and `KIOKU_PI_DIR` overrides independently; does not accidentally fall back to the real stores when pointed at fixture roots.
- [ ] Parses the harness-native envelope: Claude `type` plus nested `message.role/content` and session metadata; Codex `session_meta.payload.id/cwd` and `response_item.payload`; Pi `session` header plus `message.message.role/content`.
- [ ] Extracts user and assistant text; emits tool calls/results as one `tool` line containing tool name plus first input/output line capped at 200 characters.
- [ ] Drops Claude meta/side-channel content, command caveats, attachments, system prompts, thinking, and injected context; drops Codex base instructions/environment context and equivalent Pi noise.
- [ ] Retains message timestamp, available model, session cwd, and project basename from cwd. Correctly maps assistant to `asst` and preserves user/tool role labels.
- [ ] Skips malformed JSONL lines and continues processing later valid lines in the same file. A malformed/unsupported file cannot abort indexing of other files; skipped-file count is reported.
- [ ] Empty, truncated, missing, unreadable, or structurally incomplete files do not panic, corrupt existing index data, or silently poison other sessions.
- [ ] Never writes, modifies, truncates, renames, or deletes anything in any harness store. `$KIOKU_INDEX` or the XDG/default cache path is the only product write target; fixture generation likewise writes only under its requested synthetic HOME.
- [ ] Source identity/metadata tracking is stable; no duplicate messages/sessions after repeated scans.

## Incremental sync and index command

- [ ] Initial launch sync indexes all three harnesses and shows current counts/progress (`indexing N/M`) without blocking the UI.
- [ ] Unchanged path + mtime + size is skipped; changed file is reindexed (not appended/duplicated); deleted source file removes its indexed session; a new file is added.
- [ ] Sync changes are atomic per sync: interruption/error does not leave half-replaced session/messages/source records.
- [ ] `kioku index` prints deterministic counts per harness. `kioku index --rebuild` replaces stale index contents and produces the same result as a clean index.
- [ ] `KIOKU_INDEX` takes precedence; otherwise uses `$XDG_CACHE_HOME/kioku/index.db`, then `~/.cache/kioku/index.db`. No DB/artifacts are written to the fixture session roots.

## Search and result semantics

- [ ] Bare term is a prefix (`resum` finds `resuming`; `sume` does not); phrase quotes enforce exact order (`"tea black"` does not find `black tea`); multiple terms are AND; `-word` excludes; `--flag` remains a literal, not a negation.
- [ ] Quotation marks inside terms are escaped safely; malformed/unclosed quote input does not crash or produce unintended broad results.
- [ ] Empty query and query containing only negations do not issue invalid FTS MATCH; they show the latest message of each recent session.
- [ ] All SPEC Expected behavior cases pass: `snapshot` = `SNAPSHOT`; `resum` matches `resuming`; `sume` does not; `"tea black"` does not match `black tea`; `naive` matches `naïve`; `json_extract`, `low-water`, `S3`, and `cjk.c` match; `魚池` matches inside `日月潭紅茶產於南投縣魚池鄉`; `南投魚池` does not; single-character `茶` matches `紅茶`.
- [ ] Chinese, Japanese, Korean, and English searches work over transcript message text, not only title/excerpt; CJK two-cell characters do not break snippet boundaries or highlight positions.
- [ ] Results are one row per matching message, ordered by conversation (user/assistant) hits before tool hits, then relevance/newness; common terms span files and the newest match is retained, capped at 300. Tool-only matches remain searchable.
- [ ] `--harness claude|codex|pi` and Tab/Shift-Tab filter consistently, including empty results and recent-session mode.
- [ ] Snippets include surrounding context, start roughly 12 cells before the first hit, do not cut Latin words, use `…` when clipped, and mark only actual hit text in red+underline.

## CLI and resume behavior

- [ ] Compact search pages default to 10 rows; cursor traversal has no skips/duplicates and agrees with a single `--limit 500` result; cursors reject a changed query or flags; text `cursor:` and JSON `next_cursor` appear only when another page exists and remain bound to their output mode.
- [ ] `--sessions` ranking follows the best matching hit and per-session hit counts sum to the message-level result total.
- [ ] `show <ref>` marks the selected hit, returns requested context, includes a usable resume command, and truncates long text; `show --all` cursor pages cover the whole session without duplication.
- [ ] `--help` exits successfully with usage before opening or searching the index.
- [ ] `kioku` opens the TUI; `kioku <query...>` pre-fills the query; `--no-mouse`, `--harness`, and `--version` are accepted as documented.
- [ ] `--json <query...>` emits one compact page object (`shown`, `total`, `total_sessions`, `hits`, optional `next_cursor`) and exits without terminal escapes or prompts; each hit exposes `ref`, `harness`, `project`, `age`, `role`, and `snippet`.
- [ ] JSON mode handles zero results, CJK, quotes, multiple query args, and cursor paging with valid JSON; hit ordering matches text/TUI search semantics.
- [ ] Resume action uses `claude --resume <sessionId>`, `codex resume <id>`, or `pi --session <file path>` as applicable, with the process cwd set to the session cwd.
- [ ] Resume arguments are passed safely (no shell interpolation); launch failures are visible and do not leave the terminal in raw mode.
- [ ] If session cwd disappeared, remain in TUI and report it rather than exiting into an invalid directory.
- [ ] Enter quits cleanly, restores terminal state, changes cwd, then execs the correct harness resume command.

## Editor, clipboard, and terminal lifecycle

- [ ] `o` opens the project directory detached and leaves the TUI usable. Resolution order is `KIOKU_EDITOR`, `$VISUAL`, first PATH match among `zed`, `cursor`, `code`, `subl`, first installed macOS app among Zed/Cursor/Visual Studio Code/Sublime Text in `/Applications` or `~/Applications`, then Finder via `open <dir>`.
- [ ] Does not choose TextEdit for a folder; status and footer hint identify the resolved editor (Finder on final fallback); failed launch reports failure without exiting.
- [ ] `y` copies the exact resume command via `pbcopy` and OSC 52 and reports a status line; unavailable clipboard does not corrupt terminal state.
- [ ] Ctrl-C, normal quit, resume, errors, and failed actions restore canonical/echo/cursor/alternate-screen state; no stuck raw mode or leaked child process.

## TUI contract from `mock/index.html`

- [ ] Layout order is query row, highlights row, hit list (about 38% height), transcript, optional status/footer; resizing and narrow terminals do not panic or scramble columns.
- [ ] Hit rows show one-cell `▌` agent band, fixed agent/project/age columns, selected `selbg`; transcript header and metadata use correct harness/project/cwd/date.
- [ ] Transcript uses `✱` only on hit messages, `HH:MM`, `you`/`asst`/`tool`, terminal-default foreground for message text, wraps long text to the available text column, and leaves a blank line before each `you` turn except the first.
- [ ] Search focus edits on every printable key, updates results after 30ms debounce without blocking input; Down/Esc moves focus to results. Results focus supports Up/Down and j/k; Up at first hit and `/` return to search.
- [ ] Focused zone renders normally and unfocused zones dim with SGR 2 or muted fallback; switching focus is visible on both light and dark terminal backgrounds.
- [ ] `n`/`N` navigate next/previous hit in the selected session transcript; `h` prompts and Enter adds a pen while Esc cancels; `H` clears pens; `v` toggles full mode; `?` toggles help.
- [ ] Active-query folding shows matching messages plus the `you` message that opened each matching turn. Each contiguous hidden run collapses to one `⋯` aligned at the time column; a 60+ minute gap between shown messages also emits `⋯`.
- [ ] Full mode shows every transcript message while preserving time-gap `⋯`; hit list is hidden. Current hit uses `selbg`; folding does not discard/mislabel hits.
- [ ] Highlight pen tags add/remove by mouse; pens rotate yellow/green/pink backgrounds; red hit ink remains legible on every pen and red ink is never used on non-hit text.
- [ ] Mouse query/hit/transcript clicks set correct focus/selection; wheel scrolls the zone under pointer; Tab/Shift-Tab cycle harness; Enter resumes; Esc in results closes active UI or quits; Ctrl-C quits.
- [ ] Colors match `.impeccable/palettes.json` `log` role and `pens.json`, with theme detected from terminal background and `KIOKU_THEME=light|dark` override. Contrast remains meaningful on both light and dark themes; active-state meaning is not conveyed by color alone.
- [ ] No decorative/redundant labels or “RESUMED” stamps; only useful metadata appears.

## Performance and robustness

- [ ] Large synthetic fixture indexes and opens within an explicit E2E timeout, keeps typing responsive during sync, and reports progress; query result cap and incremental skipping prevent UI stalls.
- [ ] Repeated query edits/debounce, navigation, resizing, empty index, one enormous message, Unicode text, and many malformed lines do not deadlock, panic, or leak terminal state.
- [ ] All tests use isolated temp HOME/cache/index paths and clean up only their own temporary directories; they never invoke destructive operations against the real stores.

## Fixture coverage

`./test/fixture.sh [HOME_DIR]` provides one synthetic session per harness with real-format envelopes, neutral invented text, targeted English/CJK cases, one malformed line per transcript, and repeated/recent messages for folding and navigation. It must never read or write the caller's real transcript directories.

> The fixture contains targeted cases, not literally every malformed structure above. Add a focused generated fixture only when an E2E case needs that input shape.

Source contracts: `SPEC.md`, `PRODUCT.md`, and `mock/index.html`.

## Date range, facets and per-session cap

Written before the code. Each item is asserted by `test/range_e2e.sh` (artifact: `test/range-e2e-result.json`).

- [x] Defaults do not move: with none of the new flags, JSON for search, `--sessions`, empty-query browse, short prefix and common word is byte-identical to v0.7.0; the index schema is identical.
- [x] Boundary: a message at `00:00:00`, one at `00:00:00.5`, and one at `23:59:59.999` the day before fall on the correct side of `--since DATE`; `--until DATE` covers the whole day and excludes the next midnight.
- [x] Timestamp forms: offsets (`+08:00`), 9-digit fractions and `Z` stamps compare as the same instant.
- [x] Empty and unparseable timestamps never match a range, and facets count them as `(unknown)` instead of failing.
- [x] Timezone: date-only bounds use the local zone (`TZ=Asia/Taipei` differs from `UTC`); week facet buckets use the same zone, so a bucket count equals the total for `--since W --until W+6`.
- [x] Relative forms: `Nd`, `Nw`, `today`, `yesterday` resolve to local midnight; `--until Nd` excludes that day.
- [x] Invalid input: garbage, impossible dates, `7x`, empty value, negative value and `--since` after `--until` exit nonzero with a message (MCP: tool error); a same-day range is valid and may be empty.
- [x] Short-prefix window: an explicit range replaces the 7-day window and drops `omitted_older`.
- [x] Common-word fast path honours the range; paging returns each hit once; sessions mode counts agree.
- [x] Cursors: valid for the same range, rejected for a different one; repeating a relative-range call gives the same cursor.
- [x] Browse (empty query) filters sessions by their last message.
- [x] Silent ignore: `--since`/`--facets`/`--max-per-session` on a terminal print a page instead of opening a TUI that ignores them; `show` and `index` reject them; `--since X`, `--since=X` and flags before the query parse the same.
- [x] Facets: off by default; counts sum to total on hit, sessions, browse and common-word paths; lists past 20 projects or 52 weeks end in `(other)`; weeks start on Monday.
- [x] Cap: opt-in; total, paging and the best-ranked hit stay correct in SQL; works on the common-word path and with a range; invalid values and `--sessions` are errors.
- [x] MCP: new arguments exist on `kioku_search` / `kioku_sessions` only (`max_per_session` on search only), match CLI output, and bad values are tool errors.
- [x] Cost: on 20,000 messages, facets, cap and a range each stay under max(1.5 s, 8x the plain search).
- [x] Read-only: source transcripts stay byte-identical.

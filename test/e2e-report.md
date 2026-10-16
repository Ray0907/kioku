# E2E report

Run: 2026-10-16 06:07:49 UTC

| Check | Result | Time | Details |
|---|---:|---:|---|
| make build (sqlite_fts5) | PASS | 0s | built ./kioku |
| Index Claude/Codex/Pi real-format fixtures | PASS | 0s | claude: 5 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 3 files, 3 changed, 0 skipped |
| No-op incremental scan; transcript bytes unchanged | PASS | 0s | claude: 5 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 3 files, 0 changed, 0 skipped |
| MCP stdio: tools, JSON parity, errors, recovery, plugin packaging | PASS | 1s | test/mcp-e2e-result.json (repeatable mock-only artifact) |
| Agy: protobuf sessions, WAL sync, resume, doctor | PASS | 1s | PASS: Agy search/CJK, roles/order, MCP, resume command and cwd, wire recovery, bad workspace rows and cached data, no-op, WAL signals, read-only stores, doctor, failed scan retention, overrides, and deletion. |
| Grok: discovery, metadata, search/show/sessions, append, read-only | PASS | 0s | test/screens/grok.txt; resume is cd only (grok unavailable locally) |
| Cursor: JSON graph, rowid order, CJK, metadata, change detection, read-only | PASS | 0s | test/screens/cursor.txt and cursor.jsonl |
| Cursor resume flag verification | SKIP | 0s | cursor-agent unavailable; cursor-agent --resume <id> is unverified locally |
| OpenCode: v1/v2, WAL, search/show/filter, reindex, read-only | PASS | 1s | test/screens/opencode.txt |
| Exclude self: filtering, mentions, paging, migration, append | PASS | 0s | test/screens/exclude-self.txt |
| Environment source-dir precedence and expansion | PASS | 1s | 36 cases: native/KIOKU precedence, empty vars, tilde (including parent traversal), missing roots, independent harnesses, root switching, read-only stores |
| Incremental sync adds new file | PASS | 0s | claude: 11 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 4 files, 1 changed, 0 skipped |
| Changed source is replaced, not duplicated | PASS | 0s | claude: 11 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 4 files, 1 changed, 0 skipped |
| Deleted source is removed from index | PASS | 0s | claude: 5 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 3 files, 0 changed, 0 skipped |
| Short-prefix omitted footer and --all-time | PASS | 0s | text/JSON hits and sessions, inclusive 7-day boundary, --all-time, filters, and cursors checked |
| Tool-order fixture indexed | PASS | 0s | claude: 9 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 4 files, 1 changed, 0 skipped |
| Conversation hits precede matching tool row in JSON order | PASS | 0s | roles=['asst', 'user', 'tool'] |
| Tool-only match remains searchable | PASS | 0s |  |
| Pagination/show fixture indexed | PASS | 0s | claude: 32 messages codex: 5 messages pi: 5 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 5 files, 1 changed, 0 skipped |
| Cursor pages cover every hit exactly once | PASS | 5s | 23 unique refs across cursor pages; union equals --limit 500 |
| Default page size and cursor footer/JSON parity | PASS | 0s | 10 + 10 + 3 rows; text and JSON cursors appear only while more remain |
| Cursor rejects changed query and flags clearly | PASS | 0s |  |
| --sessions ranking/counts agree with hit list | PASS | 0s | 6 hits across 3 sessions; counts sum and best-hit rank agrees |
| show ref context and text hit marker | PASS | 0s | 5-message context, selected hit, resume command, project/cwd, and 400-cell truncation verified |
| show --all paginates the whole session | PASS | 3s | all 23 messages returned once across show --all pages |
| kioku --help exits with usage before search/index | PASS | 0s |  |
| query: snapshot (contains) | PASS | 0s |  |
| query: recoverytoken (contains) | PASS | 0s |  |
| query: resum (contains) | PASS | 0s |  |
| query: sume (empty) | PASS | 0s |  |
| query: "tea black" (empty) | PASS | 0s |  |
| query: naive (contains) | PASS | 0s |  |
| query: json_extract (contains) | PASS | 0s |  |
| query: low-water (contains) | PASS | 0s |  |
| query: S3 (contains) | PASS | 0s |  |
| query: cjk.c (contains) | PASS | 0s |  |
| query: 魚池 (contains) | PASS | 0s |  |
| query: 南投魚池 (empty) | PASS | 0s |  |
| query: 茶 (contains) | PASS | 0s |  |
| query: 日本語 (contains) | PASS | 0s |  |
| query: 한국어 (contains) | PASS | 0s |  |
| query: --flag (empty) | PASS | 0s |  |
| CLI --version | PASS | 0s | v0.6.0-dirty |
| Empty query lists recent messages as compact JSON | PASS | 0s | 5 rows |
| Malformed quote query does not crash | PASS | 0s |  |
| Compact JSON search-page schema | PASS | 0s | 6 hits |
| JSON harness filter claude | PASS | 0s |  |
| JSON harness filter codex | PASS | 0s |  |
| JSON harness filter pi | PASS | 0s |  |
| TUI include-self survives harness change | PASS | 4s | test/screens/tui-self.txt |
| Grok TUI: harness cycling and cd-only Enter | PASS | 4s | test/screens/grok-tui.txt |
| TUI starts and displays matching hit | PASS | 0s |  |
| Mono emphasis: bold without dimming | PASS | 0s |  |
| Hit color present in ANSI capture | PASS | 0s |  |
| Transcript folding ellipsis ⋯ | PASS | 0s |  |
| Full mode reveals non-hit transcript content | PASS | 0s |  |
| Highlight pen background color | PASS | 0s |  |
| Ctrl-C exits and restores terminal (process + alternate screen) | PASS | 0s |  |
| OpenCode TUI: harness cycling and resume argv + cwd | PASS | 4s | test/screens/opencode-tui.txt |
| Cursor TUI: harness cycling and resume argv + cwd | PASS | 4s | test/screens/cursor-tui.txt; real cursor-agent flag unverified if unavailable |
| Resume Claude stub argv + cwd | PASS | 0s | claude\|<fixture>/home/work/demo\|--resume 11111111-1111-4111-8111-111111111111 |
| Resume codex stub argv + cwd | PASS | 0s | codex\|<fixture>/home/work/demo\|resume 22222222-2222-4222-8222-222222222222 |
| Resume pi stub argv + cwd | PASS | 0s | pi\|<fixture>/home/work/demo\|--session <fixture>/home/.pi/agent/sessions/-work-demo/33333333-3333-4333-8333-333333333333.jsonl |
| Editor stub launch + project directory | PASS | 0s | zed\|<repo>\|<fixture>/home/work/demo |
| Editor launch leaves TUI alive | PASS | 0s |  |
| TUI n/N visits matching tool hit | PASS | 0s | both directions retain the tool hit in transcript |
| Hit-list truncation: ellipsis + whole Latin words | PASS | 0s | 3 hit rows end their clipped snippet in ellipsis; Latin token intact or omitted |
| Performance: 2k sessions / 200k messages (20 varied queries) | PASS | 0s | index=5878.05ms queries=20 median=22.3ms p95=108.19ms slowest=[{"query":"-perfneedle","ms":109.66,"timing":"timing startup=0.82ms\ntiming sync_walk=0.84ms\ntiming sync_stat=2.52ms\ntiming sync_sources=1.32ms\ntiming sync_changes=0.18ms\ntiming sync_counts=0.25ms\ntiming sync=5.13ms"},{"query":"-neutral","ms":108.19,"timing":"timing startup=0.85ms\ntiming sync_walk=0.82ms\ntiming sync_stat=2.45ms\ntiming sync_sources=1.41ms\ntiming sync_changes=0.19ms\ntiming sync_counts=0.26ms\ntiming sync=5.14ms"},{"query":"\"synthetic message\"","ms":55.32,"timing":"timing startup=0.89ms\ntiming sync_walk=0.87ms\ntiming sync_stat=2.42ms\ntiming sync_sources=1.37ms\ntiming sync_changes=0.18ms\ntiming sync_counts=0.26ms\ntiming sync=5.12ms"}] |
| Search semantics: common term is ranked by message time, not insertion order | PASS | 0s | 10 rows; harnesses=['claude', 'codex']; newest=synthetic NEWEST_MATCH_MARKER |
| Append sync: next CLI query finds new line; only tail parsed | PASS | 0s | sync_changes=0.40ms; claude: 2 messages codex: 200001 messages pi: 1 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 2002 files, 0 changed, 0 skipped |
| Partial final line is withheld until newline | PASS | 0s | timing startup=0.87ms timing sync_walk=0.84ms timing sync_stat=2.48ms timing sync_sources=1.45ms timing sync_changes=0.19ms timing sync_counts=0.28ms timing sync=5.27ms |
| Partial line is re-read and indexed when completed | PASS | 0s | claude: 2 messages codex: 200002 messages pi: 1 messages grok: 0 messages opencode: 0 messages cursor: 0 messages agy: 0 messages 2002 files, 0 changed, 0 skipped |
| Real stores: read-only independent lower-bound sanity | SKIP | 0s | Set KIOKU_E2E_REAL=1 to opt in to reading this machine’s ~/.claude, ~/.codex, and ~/.pi stores. |

**Summary:** 71 PASS, 0 FAIL.

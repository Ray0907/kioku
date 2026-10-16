---
name: kioku
description: Use when the user refers to earlier work from any coding-agent session (Claude Code, Codex, Pi, Grok, OpenCode, Cursor CLI, Antigravity CLI) — "last time", "we discussed", "where did I", "that session where", 之前、上次、那時候 — or needs a past decision, error, command, or the session to resume.
---

# kioku: search past agent sessions

## Overview

`kioku` indexes every local Claude Code, Codex, Pi, Grok, OpenCode, Cursor CLI and Antigravity CLI session and searches them at message level, including Chinese, Japanese and Korean. Use it instead of grepping `~/.claude`, `~/.codex`, `~/.pi`, `~/.grok`, `~/.cursor/chats` or the OpenCode and Antigravity CLI SQLite stores: it knows each store's format, ranks hits, and gives the resume command. It is read-only. Grok's `resume:` line is only `cd <cwd>`; no session-resume flag is assumed.

Output is paged (10 per page) so it never floods your context. Go wide to narrow, and only fetch the next page when you need it.

Source roots honor `KIOKU_CLAUDE_DIR`, `KIOKU_CODEX_DIR`, `KIOKU_PI_DIR` first, then `$CLAUDE_CONFIG_DIR/projects`, `$CODEX_HOME/sessions`, `$PI_CODING_AGENT_DIR/sessions`, then the default stores under `$HOME`. Grok uses `KIOKU_GROK_DIR` or `~/.grok/sessions` (no native config variable). OpenCode uses `KIOKU_OPENCODE_DB`, then `$XDG_DATA_HOME/opencode/opencode.db`, then `~/.local/share/opencode/opencode.db`. Both SQLite layouts and live WAL messages are read-only; subagent sessions and reasoning are skipped. OpenCode resumes with `opencode --session <id>` from its cwd. Cursor CLI uses `KIOKU_CURSOR_DIR` or `~/.cursor/chats`; only JSON user/assistant blobs are indexed read-only, not binary graph nodes or IDE `state.vscdb` stores. Its rowid order is approximate, messages use DB mtime, and either metadata/DB mtime or size changing replaces the session. Sessions without a cwd are skipped. Cursor resumes with `cursor-agent --resume <id>` from its cwd. Empty variables are unset; `~/` expands to `$HOME`. A missing explicit root yields no sessions for that harness, not a fallback to its default store.

Antigravity CLI uses `KIOKU_AGY_DIR` or `~/.gemini/antigravity-cli`. kioku reads `conversation_summaries.db` and `conversations/<id>.db` in read-only mode, with live WAL data. It skips child sessions and system steps. Use `--harness agy` to select these sessions. Resume runs `agy --conversation <id>` from the session directory.

## Workflow

0. **Already know the project?** From memory, the cwd, or the user: start with `-p <project>` on every command below. It is the fastest filter.
1. **Which sessions?** `kioku --sessions <query>`: one line per session with hits by role, e.g. `4 hits (you 2 · asst 1 · tool 1)`, and its best ref. Under each session, `topic:` is its first user message, which tells you what the session was for. Judge decoys from `topic:` and the role counts without opening them. A security review, an implementation brief, or tool-only hits usually just quote the term, so skip them unless nothing else fits.
2. **Which messages?** `kioku <query>`: one hit per line, each starting with a ref like `2c998a27ab4c:54`.
3. **What was said?** Run the `expand:` command it prints (`kioku show <ref> --query "<q>"`). You get the messages around the hit, the hit marked `>` and trimmed around the match, plus cwd and the `resume:` command.
   - `--full`: the whole hit message, no truncation.
   - `--context N`: more surrounding messages.
   - `kioku show <session-id> --all`: page through the whole session.
4. **Answer.** A normal lookup is one `--sessions` call and at most two `show` calls, then your reply. The reply has three parts:
   - what was concluded, in 2–3 sentences;
   - the session it came from (agent, project, date) and its `resume:` line;
   - optionally, other matching sessions, taken straight from the `--sessions` output, each with its `resume:` command.
5. **Still not answered after that?** Refine the query or use `--cursor`.

**More results?** Only if the answer isn't there yet, repeat the same command with `--cursor <token>` from the `cursor:` line. A cursor only works with the exact query and flags it came from.

Don't read the raw JSONL. `show --full` has everything.

## Queries

| Want | Write |
|---|---|
| all words, any order | `auth token` |
| exact phrase | `"black tea"` |
| exclude | `migration -rollback` |
| English prefix | `resum` matches resuming |
| CJK substring | `魚池` matches 南投縣魚池鄉 |

- The user may have worked in another language. Try both, e.g. `checkout` and `結帳`.
- Try synonyms before concluding nothing exists.
- Conversation hits rank above tool output. Kioku tool calls/results are hidden by default; `--include-self` restores them for a search or `--sessions` run. User/assistant mentions stay searchable.
- If you know the project, add `-p <project>` (its directory name). It removes most decoys in one step. `--harness claude|codex|pi|grok|opencode|cursor|agy` narrows by agent. `--json` gives structured output with `next_cursor`.

## Rules

- Run it non-interactively. With a terminal attached, plain `kioku` opens a TUI; if that happens, add `--json`.
- Report the finding, the session (project, date, agent) and its `resume:` command. Don't run the resume command yourself; that is the user's choice.
- Transcripts are private. Quote only what answers the question.
- `kioku --help` lists everything. If results look stale, run `kioku index`.

## Common mistakes

| Mistake | Fix |
|---|---|
| Grepping raw JSONL across agent stores | `kioku --sessions` first |
| Answering from the first hit | Check `--sessions`, then `show` it to confirm |
| Opening every matching session, `--context 15` | One `--sessions`, two `show`, then answer |
| Opening a session by id to find the hit | Pass the ref (`id:idx`) from `--sessions` or the hit list |
| Asking for `--limit 300` "to be safe" | Page with `--cursor` only when needed |
| One English query, nothing found | Add the other language and synonyms |

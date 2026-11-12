# kioku for agents

How coding agents and scripts use kioku. See the [README](../README.md) for install and the TUI.

## Command line

```sh
kioku --limit 5 snapshot                  # short refs + snippets, at most 5 hits
kioku -p kioku --limit 5 snapshot        # exact project basename, case-insensitive; repeat -p for more
kioku --here --limit 5 snapshot          # scope to this project and its descendants
kioku --limit 5 --cursor TOKEN snapshot   # next page, same query and flags
kioku --sessions --limit 5 snapshot       # conversation-bearing sessions first; per-role hit counts
kioku --since 7d snapshot                 # date range, local time: 2026-10-20, 7d, 2w, today, yesterday
kioku --since 2026-10-01 --until 2026-10-07 --facets snapshot  # plus hit counts per project, harness, week
kioku --max-per-session 2 snapshot        # at most 2 hits from any one session
kioku --role user,asst snapshot           # only these roles: user, asst, tool
kioku --model opus snapshot              # session model contains opus, case-insensitive
kioku --include-automated snapshot        # also search automated review sessions
kioku show a1b2c3d4e5f6:12 --query "snapshot" --context 3  # center the hit, with surrounding messages
kioku show a1b2c3d4e5f6:12 --full         # entire marked message; context remains compact
kioku show a1b2c3d4e5f6 --context 0 --all    # walk the entire session, 10 messages/page
kioku show a1b2c3d4e5f6 --outline         # each user message and the final reply of its turn, no tool rows
kioku show a1b2c3d4e5f6:12 --format md        # Markdown to paste into an issue or PR
kioku --json --limit 5 snapshot          # one JSON object with hits and next_cursor
kioku stats --since 30d -p kioku          # sessions, messages and last active per harness and project
kioku stats --json --harness claude       # {total,harness,project}; rows are {value,sessions,messages,last_active}
```

Sessions whose first user message starts with an automated review prompt (`Review this change`, `# Review Guidelines`) are left out of search, `--sessions`, totals and facets; `--include-automated` brings them back and `show` always opens them. `--role` takes `user`, `asst`, `tool` or a comma list and also applies to counts, facets and paging.

`--here` scopes search, `--sessions`, counts, facets and `stats` to the nearest ancestor containing a `.git` entry (otherwise the working directory), excludes home and `/`, combines with other filters, matches both the given root and its resolved symlink path, and binds cursors to that root; MCP `kioku_search` and `kioku_sessions` use the optional absolute-directory string `cwd` for the same scope (omitting it leaves search unscoped).

`--model opus` filters search, `--sessions` and `stats` by a case-insensitive literal substring of the session model (empty models do not match); MCP `kioku_search` and `kioku_sessions` accept the same filter as the optional string `model`.

`kioku stats` counts the messages inside the optional `--since`/`--until` range (same values as search) and the sessions that have one, per harness and per project, with the newest message time in local time. Projects past 20 are summed into `(other)`. Kioku tool calls are not counted. To search for the word itself, use `kioku -- stats`.

Replace the example ref and token with values from your output. Refs are stable short hashes of the source-qualified session ID plus the message index; `show` also accepts an unambiguous native session ID. Search snippets use at most 160 display cells; `show` text uses at most 400 unless `--full` is used for the marked hit (JSON then includes its original full text). The hit header and JSON `total_sessions` count distinct sessions across **all** matches. The copyable `expand:` command passes the search query to `show`, which centers the marked message around its first visible match; without `--query`, `show` starts at the beginning of the text. Session results include a directly showable `best_ref`, per-role counts (`roles.you`, `roles.asst`, `roles.tool` in JSON), and a 70-cell topic from the first real user message. `show` repeats that topic in its header and JSON. Sessions with conversation hits precede tool-only sessions; BM25 order remains within each tier. Show headers and JSON `start`, `end`, and `hit_index` use the same zero-based message indices as refs; `session_total` is the message count. A `cursor:` footer (or JSON `next_cursor`) appears only when more results exist; reuse it with the same mode, query, and flags. `show --all` starts at the first message and pages through the full session. JSON mirrors the text fields without full transcripts; use `show` to expand a result. Cursors use offsets against the current index, so a simultaneous reindex can shift later pages.

## MCP, plugin and skill

Install the `kioku` binary first and keep it on the agent's `PATH`. In Claude Code:

```text
/plugin marketplace add Ray0907/kioku
/plugin install kioku@kioku
```

Restart Claude Code after installation. The plugin loads the existing `skills/kioku` skill and starts the local MCP server. It exposes `kioku_search`, `kioku_sessions`, `kioku_show` and `kioku_context`. The first three return the same compact JSON pages as `--json` in a text content block; `kioku_context` returns the same text as `kioku context`. `kioku_show` takes `limit` (default 10, at most 100) and `outline`; the search tools take `include_automated`, and `kioku_search` takes `role`. All tools are read-only; they update kioku's cache but never change source transcripts or expose `index --rebuild`. Snippets are capped unless you explicitly use `kioku_show` with `full: true` for a message ref.

For any stdio MCP client, add this to its MCP configuration (adapt the outer key if your client uses a different format):

```json
{
  "mcpServers": {
    "kioku": { "command": "kioku", "args": ["mcp"] }
  }
}
```

The plugin also adds a `SessionStart` hook that runs `kioku context`: every new Claude Code session starts with the project's recent sessions (agent, age, topic, last question and reply, and a ref for `kioku show`). It prints nothing, and never blocks a session, when there is no history or no index. Run `kioku context` in a project folder to preview it.

`kioku mcp` uses newline-delimited JSON-RPC 2.0 over stdin/stdout, with protocol version `2025-06-18`. Logs go only to stderr. Normal `KIOKU_*` source and index environment variables still apply. The server can read your local transcripts: enable it only in trusted clients and quote only the history needed for the task.

For harnesses without MCP or skills, put this in `AGENTS.md`:

```markdown
When asked about earlier coding-agent work, search local history with:
- `kioku --json --sessions -p <project> "<query>"`
- `kioku show <best_ref> --json --query "<query>"`
Use `kioku --json "<query>"` for message-level hits; reuse the exact query
and flags with `--cursor <next_cursor>` if needed. Quote only relevant
findings, include the resume command, and never run it without permission.
```

## Install the skill alone

`skills/kioku/SKILL.md` teaches coding agents to reach for kioku whenever you mention earlier work ("last time we…", "where did I…", 之前、上次). With it, they search wide to narrow instead of grepping raw transcripts: `--sessions`, then hits, then `show`, paging with `--cursor`. Install it once:

```sh
git clone https://github.com/Ray0907/kioku && cd kioku
mkdir -p ~/.claude/skills/kioku ~/.agents/skills/kioku
cp skills/kioku/SKILL.md ~/.claude/skills/kioku/   # Claude Code
cp skills/kioku/SKILL.md ~/.agents/skills/kioku/   # Codex, Pi and other agents that read ~/.agents/skills
```

The `kioku` binary must be on your `PATH`.

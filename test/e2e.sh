#!/bin/bash
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd); TEST="$ROOT/test"; BIN="$ROOT/kioku"; REALHOME_DEFAULT=$HOME
REPORT="$TEST/e2e-report.md"; SCREENS="$TEST/screens"; mkdir -p "$SCREENS"
printf '# E2E report\n\nRun: %s\n\n| Check | Result | Time | Details |\n|---|---:|---:|---|\n' "$(date -u '+%Y-%m-%d %H:%M:%S UTC')" > "$REPORT"
PASS=0; FAIL=0
: > "$TEST/BUGS.md"
record(){ local name=$1 res=$2 sec=$3 detail=${4:-} expected='check passes per test/FAILURE_MODES.md' repro='test/e2e.sh'; detail=${detail//|/\\|}; detail=${detail//$'\n'/ }; printf '| %s | %s | %ss | %s |\n' "$name" "$res" "$sec" "$detail" >> "$REPORT"; if [[ $res == PASS ]]; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); if [[ $name == Performance:* ]]; then expected='p95 over 20 varied query latencies is <= 200ms'; repro='test/e2e.sh (200k-message fixture)'; fi; if [[ $name == Hit-list* ]]; then expected='each clipped hit snippet ends with ellipsis and no Latin word is cut'; fi; if [[ $name == Search\ semantics:* ]]; then expected='common-word results include the newest Claude message and results outside the last-indexed Pi file'; fi; failbug "$name" "$repro" "$expected" "$detail"; fi; }
skip(){ printf '| %s | SKIP | 0s | %s |\n' "$1" "$2" >> "$REPORT"; }
TMP=$(mktemp -d "${TMPDIR:-/tmp}/kioku-e2e.XXXXXX")
cleanup(){ tmux kill-session -t "htool-$$" 2>/dev/null || :; tmux kill-session -t "htrunc-$$" 2>/dev/null || :; tmux kill-session -t "he2e-$$" 2>/dev/null || :; tmux kill-session -t "hrsm-$$" 2>/dev/null || :; tmux kill-session -t "hrsm-codex-$$" 2>/dev/null || :; tmux kill-session -t "hrsm-pi-$$" 2>/dev/null || :; tmux kill-session -t "hedt-$$" 2>/dev/null || :; rm -rf "$TMP"; }
trap cleanup EXIT
failbug(){ printf '\n- **%s**\n  - Repro: `%s`\n  - Expected: %s\n  - Actual: %s\n' "$1" "$2" "$3" "$4" >> "$TEST/BUGS.md"; }

# Build phase
start=$SECONDS; (cd "$ROOT" && make build) >"$SCREENS/build.txt" 2>&1; rc=$?; if ((rc==0)); then record 'make build (sqlite_fts5)' PASS $((SECONDS-start)) 'built ./kioku'; else record 'make build (sqlite_fts5)' FAIL $((SECONDS-start)) "$(<"$SCREENS/build.txt")"; fi

H="$TMP/home"; "$TEST/fixture.sh" "$H" >/dev/null || exit 1
mkdir -p "$H/work/demo" "$H/index"
python3 - "$H" <<'PY'
import pathlib,sys
h=pathlib.Path(sys.argv[1])
for p in h.rglob('*.jsonl'):
 p.write_text(p.read_text().replace('/work/demo',str(h/'work/demo')))
PY
unset KIOKU_CLAUDE_DIR KIOKU_CODEX_DIR KIOKU_PI_DIR KIOKU_GROK_DIR KIOKU_OPENCODE_DB KIOKU_CURSOR_DIR KIOKU_AGY_DIR XDG_DATA_HOME CLAUDE_CONFIG_DIR CODEX_HOME PI_CODING_AGENT_DIR
export HOME="$H" KIOKU_INDEX="$H/index/index.db" KIOKU_THEME=light TERM=xterm-256color COLORTERM=truecolor

start=$SECONDS; out=$("$BIN" index --rebuild 2>&1); rc=$?
if ((rc==0)) && grep -q 'claude: 5 messages' <<<"$out" && grep -q 'codex: 5 messages' <<<"$out" && grep -q 'pi: 5 messages' <<<"$out" && grep -q '3 files, 3 changed, 0 skipped' <<<"$out"; then record 'Index Claude/Codex/Pi real-format fixtures' PASS $((SECONDS-start)) "$out"; else record 'Index Claude/Codex/Pi real-format fixtures' FAIL $((SECONDS-start)) "$out"; fi
start=$SECONDS; before=$(shasum "$H"/.claude/projects/*/*.jsonl "$H"/.codex/sessions/*/*/*/*.jsonl "$H"/.pi/agent/sessions/*/*.jsonl); out=$("$BIN" index 2>&1); rc=$?; after=$(shasum "$H"/.claude/projects/*/*.jsonl "$H"/.codex/sessions/*/*/*/*.jsonl "$H"/.pi/agent/sessions/*/*.jsonl)
if ((rc==0)) && grep -q '0 changed' <<<"$out" && [[ $before == "$after" ]]; then record 'No-op incremental scan; transcript bytes unchanged' PASS $((SECONDS-start)) "$out"; else record 'No-op incremental scan; transcript bytes unchanged' FAIL $((SECONDS-start)) "$out"; fi
source "$TEST/mcp_e2e.sh"
source "$TEST/e2e/agy.sh"
source "$TEST/e2e/grok.sh"
source "$TEST/e2e/cursor.sh"
source "$TEST/e2e/opencode.sh"
source "$TEST/e2e/self.sh"
source "$TEST/e2e/index.sh"
source "$TEST/e2e/search.sh"
source "$TEST/e2e/tui.sh"

start=$SECONDS; long_ok=1; LONGHOME="$TMP/long-home"
python3 - "$LONGHOME" <<'PY'
import json,pathlib,sys
h=pathlib.Path(sys.argv[1]); p=h/'.claude/projects/demo/long.jsonl'; p.parent.mkdir(parents=True)
text='\n'.join(f'B6LONG line {i:03d}' for i in range(1,201))
p.write_text(json.dumps({'type':'assistant','timestamp':'2026-11-11T12:00:00Z','cwd':str(h),'sessionId':'b6-long','message':{'role':'assistant','content':[{'type':'text','text':text}]}})+'\n')
PY
tmux new-session -d -x 100 -y 30 -s "htool-$$" "cd '$TMP' && HOME='$LONGHOME' KIOKU_INDEX='$LONGHOME/index.db' KIOKU_THEME=light TERM=xterm-256color exec '$BIN' B6LONG" 2>/dev/null
sleep 1
capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q 'B6LONG line 001' <<<"$capture" || long_ok=0
if grep -q 'B6LONG line 200' <<<"$capture"; then long_ok=0; fi
tmux send-keys -t "htool-$$" Down
for ((i=0;i<30;i++)); do tmux send-keys -t "htool-$$" NPage; done
sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q 'B6LONG line 200' <<<"$capture" || long_ok=0
for ((i=0;i<30;i++)); do tmux send-keys -t "htool-$$" PPage; done
sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q 'B6LONG line 001' <<<"$capture" || long_ok=0
tmux kill-session -t "htool-$$" 2>/dev/null || :
if ((long_ok)); then record 'TUI: page through a 200-line message and back' PASS $((SECONDS-start)) 'PgDn reaches line 200; PgUp returns to line 001'; else record 'TUI: page through a 200-line message and back' FAIL $((SECONDS-start)) "$capture"; fi

source "$TEST/e2e/perf.sh"
source "$TEST/e2e/real.sh"
printf '\n**Summary:** %d PASS, %d FAIL.\n' "$PASS" "$FAIL" >> "$REPORT"
printf 'E2E suite (including make build): %d PASS / %d FAIL.\n' "$PASS" "$FAIL" | tee -a "$SCREENS/grok.txt" >> "$SCREENS/opencode.txt"
# Committed artifacts must not carry machine-specific paths (e.g. macOS /var/folders temp dirs).
redact_root=$(cd "$TMP" && pwd -P)
for f in "$REPORT" "$TEST/BUGS.md" "$SCREENS"/*; do
  [[ -f $f ]] || continue
  sed -i '' -e "s#${redact_root}#<fixture>#g" -e "s#${TMP}#<fixture>#g" -e "s#/private/var/folders/[^ |\"']*/T/kioku-e2e\.[A-Za-z0-9]*#<fixture>#g" -e "s#/var/folders/[^ |\"']*/T/kioku-e2e\.[A-Za-z0-9]*#<fixture>#g" -e "s#${ROOT}#<repo>#g" -E -e "s#(/private)?/var/folders/[^ |\"']*#<fixture>#g" "$f"
done
printf '%d PASS / %d FAIL — report: test/e2e-report.md\n' "$PASS" "$FAIL"
((FAIL==0))

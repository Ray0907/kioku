# Long Latin hits exercise snippet clipping in each agent's hit-list row.
python3 - "$H" <<'PY'
import glob,json,pathlib,sys
h=pathlib.Path(sys.argv[1]); text='cutprobe alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu supercalifragilisticexpialidocious tail'
claude=glob.glob(str(h/'.claude/projects/*/*.jsonl'))[0]
with open(claude,'a') as f: f.write(json.dumps({'type':'user','uuid':'11111111-1111-4111-8111-111111111106','timestamp':'2026-09-27T10:30:00Z','cwd':str(h/'work/demo'),'sessionId':'11111111-1111-4111-8111-111111111111','message':{'role':'user','content':text}},ensure_ascii=False)+'\n')
codex=glob.glob(str(h/'.codex/sessions/**/*.jsonl'),recursive=True)[0]
with open(codex,'a') as f: f.write(json.dumps({'timestamp':'2026-09-27T10:30:00Z','type':'response_item','payload':{'type':'message','role':'user','content':[{'type':'input_text','text':text}]}},ensure_ascii=False)+'\n')
pi=glob.glob(str(h/'.pi/agent/sessions/*/*.jsonl'))[0]
with open(pi,'a') as f: f.write(json.dumps({'type':'message','id':'33333333-3333-4333-8333-333333333306','parentId':'33333333-3333-4333-8333-333333333305','timestamp':'2026-09-27T10:30:00Z','message':{'role':'user','content':[{'type':'text','text':text}]}},ensure_ascii=False)+'\n')
PY
# Include-self persists through a TUI harness change and subsequent queries.
start=$SECONDS
SELFHOME="$H/self-lookups"; self_ok=1; : > "$SCREENS/tui-self.txt"
for flag in '' --include-self; do
  tmux new-session -d -x 120 -y 40 -s "htool-$$" "cd '$ROOT' && HOME='$SELFHOME' KIOKU_INDEX='$SELFHOME/index.db' KIOKU_THEME=light TERM=xterm-256color exec ./kioku --harness pi $flag secretword" 2>/dev/null
  sleep 1
  capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
  printf '%s\n%s\n' "pi ${flag:-default}:" "$capture" >> "$SCREENS/tui-self.txt"
  if [[ -n $flag ]]; then
    grep -q '6 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'grok --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '0 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'opencode --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '0 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'cursor --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '0 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'hermes --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '0 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'agy --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '0 messages' <<<"$capture" || self_ok=0
    tmux send-keys -t "htool-$$" Tab; sleep .3
    capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
    printf 'all --include-self:\n%s\n' "$capture" >> "$SCREENS/tui-self.txt"
    grep -q '20 messages' <<<"$capture" || self_ok=0
  else
    grep -q '0 messages' <<<"$capture" || self_ok=0
  fi
  tmux kill-session -t "htool-$$" 2>/dev/null || :
done
if ((self_ok)); then record 'TUI include-self survives harness change' PASS $((SECONDS-start)) 'test/screens/tui-self.txt'; else record 'TUI include-self survives harness change' FAIL $((SECONDS-start)) "$(<"$SCREENS/tui-self.txt")"; fi

# Grok is reachable in both TUI cycle directions; Enter prints only cd and exits cleanly.
start=$SECONDS; grok_ok=1; GROKHOME=$(cd "$H/grok-home" && pwd -P)
python3 - "$GROKHOME" <<'PY'
import json,pathlib,sys
home=pathlib.Path(sys.argv[1]); cwd=home/'work/grok demo'; cwd.mkdir(parents=True)
summary=next((home/'.grok/sessions').glob('*/555*/summary.json')); data=json.loads(summary.read_text()); data['info']['cwd']=str(cwd); summary.write_text(json.dumps(data))
PY
tmux new-session -d -x 120 -y 40 -s "htool-$$" "cd '$ROOT' && HOME='$GROKHOME' KIOKU_INDEX='$GROKHOME/tui.db' KIOKU_THEME=light TERM=xterm-256color exec ./kioku --harness grok grokteatoken" 2>/dev/null
tmux set-option -t "htool-$$" remain-on-exit on 2>/dev/null
sleep 1; tmux capture-pane -t "htool-$$" -p > "$SCREENS/grok-tui.txt" 2>&1
grep -q 'grok' "$SCREENS/grok-tui.txt" && grep -q '1 messages' "$SCREENS/grok-tui.txt" || grok_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '1 messages' <<<"$capture" || grok_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" Enter; sleep .3
tmux capture-pane -t "htool-$$" -p -S -100 >> "$SCREENS/grok-tui.txt" 2>&1
[[ $(tmux display-message -p -t "htool-$$" '#{pane_dead} #{pane_dead_status}') == '1 0' ]] || grok_ok=0
grep -Fq "cd \"$GROKHOME/work/grok demo\"" "$SCREENS/grok-tui.txt" || grok_ok=0
tmux kill-session -t "htool-$$" 2>/dev/null || :
if ((grok_ok)); then record 'Grok TUI: harness cycling and cd-only Enter' PASS $((SECONDS-start)) 'test/screens/grok-tui.txt'; else record 'Grok TUI: harness cycling and cd-only Enter' FAIL $((SECONDS-start)) "$(<"$SCREENS/grok-tui.txt")"; fi

# Real TUI via tmux. Plain and escape-preserving captures are retained for inspection.
tmux set-option -g remain-on-exit on 2>/dev/null || :
export KIOKU_INDEX="$H/index/tui.db"
tmux new-session -d -x 120 -y 40 -s "he2e-$$" "cd '$ROOT' && HOME='$HOME' KIOKU_INDEX='$KIOKU_INDEX' KIOKU_THEME=light TERM=xterm-256color exec ./kioku snapshot" 2>/dev/null
tmux set-option -t "he2e-$$" remain-on-exit on 2>/dev/null
sleep 1
tmux capture-pane -t "he2e-$$" -p > "$SCREENS/tui-start.txt" 2>&1; tmux capture-pane -t "he2e-$$" -ep > "$SCREENS/tui-start-ansi.txt" 2>&1
start=$SECONDS; if grep -q 'SNAPSHOT' "$SCREENS/tui-start.txt"; then record 'TUI starts and displays matching hit' PASS $((SECONDS-start)) ''; else record 'TUI starts and displays matching hit' FAIL $((SECONDS-start)) "$(<"$SCREENS/tui-start.txt")"; fi
if grep -q $'\033\[1m' "$SCREENS/tui-start-ansi.txt" && ! grep -q $'\033\[2m' "$SCREENS/tui-start-ansi.txt"; then record 'Mono emphasis: bold without dimming' PASS 0 ''; else record 'Mono emphasis: bold without dimming' FAIL 0 'expected SGR 1 and no SGR 2 in capture'; fi
if grep -Fq '38;2;182;50;44' "$SCREENS/tui-start-ansi.txt"; then record 'Hit color present in ANSI capture' PASS 0 ''; else record 'Hit color present in ANSI capture' FAIL 0 'red hit pen not found in escape capture'; fi
# Escape to results, enable full transcript, check folded/full render, then add a pen.
tmux send-keys -t "he2e-$$" Escape; sleep .3; tmux capture-pane -t "he2e-$$" -p > "$SCREENS/tui-folded.txt"
tmux send-keys -t "he2e-$$" v; sleep .2; tmux capture-pane -t "he2e-$$" -p > "$SCREENS/tui-full.txt"
if grep -q '⋯' "$SCREENS/tui-folded.txt"; then record 'Transcript folding ellipsis ⋯' PASS 0 ''; else record 'Transcript folding ellipsis ⋯' FAIL 0 'not visible'; fi
if grep -q 'Fish pond\|魚池\|tea' "$SCREENS/tui-full.txt"; then record 'Full mode reveals non-hit transcript content' PASS 0 ''; else record 'Full mode reveals non-hit transcript content' FAIL 0 'expected transcript text missing'; fi
tmux send-keys -t "he2e-$$" v; sleep .15; tmux send-keys -t "he2e-$$" h; sleep .2; tmux send-keys -l -t "he2e-$$" snapshot; tmux send-keys -t "he2e-$$" Enter; sleep .3; tmux capture-pane -t "he2e-$$" -ep > "$SCREENS/tui-pen-ansi.txt"
if grep -Fq '48;2;253;232;154' "$SCREENS/tui-pen-ansi.txt"; then record 'Highlight pen background color' PASS 0 ''; else record 'Highlight pen background color' FAIL 0 'pen color not found'; fi
# Ctrl-C exits UI; tmux remains-on-exit lets us assert the process is dead.
tmux send-keys -t "he2e-$$" C-c; sleep .5; tmux capture-pane -t "he2e-$$" -p > "$SCREENS/tui-after-ctrl-c.txt" 2>&1; state=$(tmux display-message -p -t "he2e-$$" '#{pane_dead}' 2>/dev/null || echo yes)
if [[ $state == 1 ]] && ! grep -q 'kioku ▸' "$SCREENS/tui-after-ctrl-c.txt"; then record 'Ctrl-C exits and restores terminal (process + alternate screen)' PASS 0 ''; else record 'Ctrl-C exits and restores terminal (process exits)' FAIL 0 "pane_dead=$state"; fi

# Stub agents and editor: assert exec argv/cwd and detached editor arguments.
STUB="$TMP/stub"; mkdir -p "$STUB"; export STUBLOG="$TMP/stub.log"
for tool in claude codex pi opencode cursor-agent zed; do cat > "$STUB/$tool" <<'SH'
#!/bin/sh
printf '%s|%s|%s\n' "$(basename "$0")" "$PWD" "$*" >> "$STUBLOG"
SH
chmod +x "$STUB/$tool"; done
export PATH="$STUB:$PATH"
# OpenCode cycles both directions and execs its resume stub from the stored cwd.
start=$SECONDS; opencode_ok=1; OPHOME=$(cd "$H/opencode-home" && pwd -P)
python3 - "$OPHOME" <<'PY'
import pathlib,sqlite3,sys
home=pathlib.Path(sys.argv[1]); cwd=home/'work/opencode demo'; cwd.mkdir(parents=True)
with sqlite3.connect(home/'.local/share/opencode/opencode.db') as db:
 db.execute('UPDATE session SET directory=? WHERE id="oc-v1"',(str(cwd),))
PY
tmux new-session -d -x 120 -y 40 -s "htool-$$" "cd '$ROOT' && HOME='$OPHOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_INDEX='$OPHOME/tui.db' KIOKU_THEME=light TERM=xterm-256color exec ./kioku --harness opencode oceditedtoken" 2>/dev/null
tmux set-option -t "htool-$$" remain-on-exit on 2>/dev/null
sleep 1; tmux capture-pane -t "htool-$$" -p > "$SCREENS/opencode-tui.txt" 2>&1
grep -q '▢ opencode demo  opencode' "$SCREENS/opencode-tui.txt" && grep -q '1 messages' "$SCREENS/opencode-tui.txt" || opencode_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || opencode_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || opencode_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '1 messages' <<<"$capture" || opencode_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" Escape; sleep .2; tmux send-keys -t "htool-$$" Enter; sleep .6
expected="opencode|$OPHOME/work/opencode demo|--session oc-v1"
grep -Fq "$expected" "$STUBLOG" 2>/dev/null || opencode_ok=0
[[ $(tmux display-message -p -t "htool-$$" '#{pane_dead} #{pane_dead_status}') == '1 0' ]] || opencode_ok=0
printf '\nResume stub: %s\n' "$expected" >> "$SCREENS/opencode-tui.txt"
tmux kill-session -t "htool-$$" 2>/dev/null || :
if ((opencode_ok)); then record 'OpenCode TUI: harness cycling and resume argv + cwd' PASS $((SECONDS-start)) 'test/screens/opencode-tui.txt'; else record 'OpenCode TUI: harness cycling and resume argv + cwd' FAIL $((SECONDS-start)) "$(<"$SCREENS/opencode-tui.txt")"; fi

# Cursor cycles through agy to all; Enter uses cwd and native ID.
start=$SECONDS; cursor_ok=1; CURSORHOME=$(cd "$H/cursor-home" && pwd -P)
python3 - "$CURSORHOME" <<'PY'
import json,pathlib,sys
home=pathlib.Path(sys.argv[1]); cwd=home/'work/cursor demo'; cwd.mkdir(parents=True)
meta=next((home/'.cursor/chats').glob('*/666*/meta.json'))
m=json.loads(meta.read_text()); m['cwd']=str(cwd); meta.write_text(json.dumps(m))
PY
tmux new-session -d -x 120 -y 40 -s "htool-$$" "cd '$ROOT' && HOME='$CURSORHOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_INDEX='$CURSORHOME/tui.db' KIOKU_THEME=light TERM=xterm-256color exec ./kioku --harness cursor cursorfinaltoken" 2>/dev/null
tmux set-option -t "htool-$$" remain-on-exit on 2>/dev/null
sleep 1; tmux capture-pane -t "htool-$$" -p > "$SCREENS/cursor-tui.txt" 2>&1
grep -q 'cursor.*cursor demo' "$SCREENS/cursor-tui.txt" && grep -q '1 messages' "$SCREENS/cursor-tui.txt" || cursor_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '1 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Escape; sleep .2; tmux send-keys -t "htool-$$" Enter; sleep .6
expected="cursor-agent|$CURSORHOME/work/cursor demo|--resume 66666666-6666-4666-8666-666666666666"
grep -Fq "$expected" "$STUBLOG" 2>/dev/null || cursor_ok=0
[[ $(tmux display-message -p -t "htool-$$" '#{pane_dead} #{pane_dead_status}') == '1 0' ]] || cursor_ok=0
printf '\nResume stub: %s\n' "$expected" >> "$SCREENS/cursor-tui.txt"
tmux kill-session -t "htool-$$" 2>/dev/null || :
if ((cursor_ok)); then record 'Cursor TUI: harness cycling and resume argv + cwd' PASS $((SECONDS-start)) 'test/screens/cursor-tui.txt; real cursor-agent flag unverified if unavailable'; else record 'Cursor TUI: harness cycling and resume argv + cwd' FAIL $((SECONDS-start)) "$(<"$SCREENS/cursor-tui.txt")"; fi

# Resume Claude: execute stub in real synthetic cwd after selecting the first hit.
export KIOKU_INDEX="$H/index/resume.db"
tmux new-session -d -x 100 -y 32 -s "hrsm-$$" "cd '$ROOT' && HOME='$HOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_INDEX='$KIOKU_INDEX' TERM=xterm-256color exec ./kioku --harness claude snapshot" 2>/dev/null
sleep 1; tmux send-keys -t "hrsm-$$" Escape; sleep .2; tmux send-keys -t "hrsm-$$" Enter; for _ in {1..50}; do grep -q "^claude|" "$STUBLOG" 2>/dev/null && break; sleep .1; done
CWD_PHYS=$(cd "$H/work/demo" && pwd -P)
if grep -Fq "claude|$CWD_PHYS|--resume 11111111-1111-4111-8111-111111111111" "$STUBLOG" 2>/dev/null; then record 'Resume Claude stub argv + cwd' PASS 0 "$(tail -1 "$STUBLOG")"; else record 'Resume Claude stub argv + cwd' FAIL 0 "$(cat "$STUBLOG" 2>/dev/null || echo no stub call)"; fi
# Codex and Pi use their harness-specific resume forms too.
for harness in codex pi; do
  session="hrsm-$harness-$$"; export KIOKU_INDEX="$H/index/resume-$harness.db"
  tmux new-session -d -x 100 -y 32 -s "$session" "cd '$ROOT' && HOME='$HOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_INDEX='$KIOKU_INDEX' TERM=xterm-256color exec ./kioku --harness $harness snapshot" 2>/dev/null
  sleep 1; tmux send-keys -t "$session" Escape; sleep .2; tmux send-keys -t "$session" Enter; for _ in {1..50}; do grep -q "^$harness|" "$STUBLOG" 2>/dev/null && break; sleep .1; done
  if [[ $harness == codex ]]; then expected="codex|$CWD_PHYS|resume 22222222-2222-4222-8222-222222222222"; else pi_path=$(python3 -c 'import os,sys; print(os.path.normpath(sys.argv[1]))' "$H/.pi/agent/sessions/-work-demo/33333333-3333-4333-8333-333333333333.jsonl"); expected="pi|$CWD_PHYS|--session $pi_path"; fi
  if grep -Fq "$expected" "$STUBLOG" 2>/dev/null; then record "Resume $harness stub argv + cwd" PASS 0 "$expected"; else record "Resume $harness stub argv + cwd" FAIL 0 "expected $expected; log: $(cat "$STUBLOG" 2>/dev/null)"; fi
done
# Editor configuration takes precedence and receives project cwd while TUI survives.
export KIOKU_INDEX="$H/index/editor.db" KIOKU_EDITOR="$STUB/zed"
tmux new-session -d -x 100 -y 32 -s "hedt-$$" "cd '$ROOT' && HOME='$HOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_EDITOR='$KIOKU_EDITOR' KIOKU_INDEX='$KIOKU_INDEX' TERM=xterm-256color exec ./kioku snapshot" 2>/dev/null
sleep 1; tmux send-keys -t "hedt-$$" Escape; sleep .2; tmux send-keys -t "hedt-$$" o; sleep .5
if grep -Fq 'zed|' "$STUBLOG" 2>/dev/null && grep -Fq 'work/demo' "$STUBLOG" 2>/dev/null; then record 'Editor stub launch + project directory' PASS 0 "$(tail -1 "$STUBLOG")"; else record 'Editor stub launch + project directory' FAIL 0 "zed not launched with project directory; log=$(cat "$STUBLOG" 2>/dev/null)"; fi
tmux capture-pane -t "hedt-$$" -p > "$SCREENS/tui-editor.txt" 2>&1
if tmux has-session -t "hedt-$$" 2>/dev/null; then record 'Editor launch leaves TUI alive' PASS 0 ''; else record 'Editor launch leaves TUI alive' FAIL 0 'TUI exited'; fi

tmux kill-session -t "hedt-$$" 2>/dev/null || :
# n/N must still navigate to matching tool-output messages in the transcript.
export KIOKU_INDEX="$H/index/tool-nav.db"
tmux new-session -d -x 120 -y 40 -s "htool-$$" "cd '$ROOT' && HOME='$HOME' KIOKU_INDEX='$KIOKU_INDEX' KIOKU_THEME=light TERM=xterm-256color exec ./kioku toolprobeonly" 2>/dev/null
sleep 1; tmux send-keys -t "htool-$$" Escape; sleep .2; tmux send-keys -t "htool-$$" n; sleep .2; tmux capture-pane -t "htool-$$" -p > "$SCREENS/tui-tool-nav-next.txt"
tmux send-keys -t "htool-$$" N; sleep .2; tmux capture-pane -t "htool-$$" -p > "$SCREENS/tui-tool-nav-prev.txt"
if grep -Eq '[✱›].*tool.*toolprobeonly' "$SCREENS/tui-tool-nav-next.txt" && grep -Eq '[✱›].*tool.*toolprobeonly' "$SCREENS/tui-tool-nav-prev.txt"; then record 'TUI n/N visits matching tool hit' PASS 0 'both directions retain the tool hit in transcript'; else record 'TUI n/N visits matching tool hit' FAIL 0 "n=$(grep -E 'toolprobeonly' "$SCREENS/tui-tool-nav-next.txt") N=$(grep -E 'toolprobeonly' "$SCREENS/tui-tool-nav-prev.txt")"; fi
tmux kill-session -t "htool-$$" 2>/dev/null || :
# Truncation test has a hit crossing a Latin word at the snippet's right boundary.
export KIOKU_INDEX="$H/index/trunc.db"
tmux new-session -d -x 120 -y 40 -s "htrunc-$$" "cd '$ROOT' && HOME='$HOME' KIOKU_INDEX='$KIOKU_INDEX' KIOKU_THEME=light TERM=xterm-256color exec ./kioku cutprobe" 2>/dev/null
sleep 1; tmux capture-pane -t "htrunc-$$" -p > "$SCREENS/tui-truncation.txt" 2>&1
python3 - "$SCREENS/tui-truncation.txt" <<'PY' > "$TMP/truncation-check.txt"
import sys
rows=[x.rstrip('\\n') for x in open(sys.argv[1],encoding='utf8') if x.startswith((' › ✻ ', '   ✻ ', ' › ◇ ', '   ◇ ', ' › π ', '   π '))]
errors=[]
if len(rows)!=3: errors.append(f'expected 3 hit rows, found {len(rows)}')
for row in rows:
 agent=row[3]
 snippet=row[5:120-19].rstrip()
 if not snippet.endswith('…'): errors.append(f'{agent} hit row has no trailing ellipsis: {row}')
 token='supercalifragilisticexpialidocious'
 if token[:12] in snippet and token not in snippet: errors.append(f'{agent} row cuts Latin token: {row}')
print('\\n'.join(errors) if errors else '3 hit rows end their clipped snippet in ellipsis; Latin token intact or omitted')
sys.exit(bool(errors))
PY
if [[ ! -s "$TMP/truncation-check.txt" ]] || ! grep -q 'expected\|no trailing\|cuts Latin' "$TMP/truncation-check.txt"; then record 'Hit-list truncation: ellipsis + whole Latin words' PASS 0 "$(<"$TMP/truncation-check.txt")"; else record 'Hit-list truncation: ellipsis + whole Latin words' FAIL 0 "$(<"$TMP/truncation-check.txt")"; fi
tmux kill-session -t "htrunc-$$" 2>/dev/null || :


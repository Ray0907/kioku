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
unset KIOKU_CLAUDE_DIR KIOKU_CODEX_DIR KIOKU_PI_DIR KIOKU_GROK_DIR KIOKU_OPENCODE_DB KIOKU_CURSOR_DIR XDG_DATA_HOME CLAUDE_CONFIG_DIR CODEX_HOME PI_CODING_AGENT_DIR
export HOME="$H" KIOKU_INDEX="$H/index/index.db" KIOKU_THEME=light TERM=xterm-256color COLORTERM=truecolor

start=$SECONDS; out=$("$BIN" index --rebuild 2>&1); rc=$?
if ((rc==0)) && grep -q 'claude: 5 messages' <<<"$out" && grep -q 'codex: 5 messages' <<<"$out" && grep -q 'pi: 5 messages' <<<"$out" && grep -q '3 files, 3 changed, 0 skipped' <<<"$out"; then record 'Index Claude/Codex/Pi real-format fixtures' PASS $((SECONDS-start)) "$out"; else record 'Index Claude/Codex/Pi real-format fixtures' FAIL $((SECONDS-start)) "$out"; fi
start=$SECONDS; before=$(shasum "$H"/.claude/projects/*/*.jsonl "$H"/.codex/sessions/*/*/*/*.jsonl "$H"/.pi/agent/sessions/*/*.jsonl); out=$("$BIN" index 2>&1); rc=$?; after=$(shasum "$H"/.claude/projects/*/*.jsonl "$H"/.codex/sessions/*/*/*/*.jsonl "$H"/.pi/agent/sessions/*/*.jsonl)
if ((rc==0)) && grep -q '0 changed' <<<"$out" && [[ $before == "$after" ]]; then record 'No-op incremental scan; transcript bytes unchanged' PASS $((SECONDS-start)) "$out"; else record 'No-op incremental scan; transcript bytes unchanged' FAIL $((SECONDS-start)) "$out"; fi
# Grok directory sessions track only chat_history.jsonl, including append-only tool matching.
start=$SECONDS
if python3 - "$BIN" "$H/grok-home" <<'PY' > "$SCREENS/grok.txt" 2>&1
import hashlib,json,os,pathlib,shlex,sqlite3,subprocess,sys
binary,home=sys.argv[1:]; home=pathlib.Path(home)
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'))
def run(*args): return subprocess.check_output([binary,*args],env=env,text=True)
def page(*args): return json.loads(run(args[0],'--json',*args[1:]) if args[0]=='show' else run('--json',*args))
def fingerprints(): return {str(p.relative_to(home)):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*') if p.name in ['chat_history.jsonl','summary.json']}
before=fingerprints()
assert 'grok: 6 messages' in run('index','--rebuild')
assert '0 changed' in run('index')
for query in ['grokteatoken','魚池','grokanswer','groktearesult','grokrecovery']:
 hit=page('--harness','grok',query)
 assert hit['total']==1 and hit['hits'][0]['harness']=='grok', hit
 assert page(query)['total']==1
 assert page('--harness','claude',query)['total']==0
sessions=page('--sessions','--harness','grok','grokteatoken')
assert sessions['total']==1 and sessions['sessions'][0]['harness']=='grok', sessions
assert page('--sessions','--harness','grok')['total']==1
shown=page('show','grok-native-id','--all','--limit','20')
assert shown['cwd']=='/work/grok demo' and shown['project']=='grok demo', shown
assert shlex.split(shown['resume_cmd'])==shown['resume_argv']==['cd',shown['cwd']], shown
assert [m['role'] for m in shown['messages']]==['user','asst','tool','tool','user','tool'], shown
assert shown['messages'][2]['text']=='read_file · {"path":"groktea.txt"}', shown
assert shown['messages'][3]['text']=='read_file · groktearesult: notes found.', shown
assert 'grokrecovery' in run('show',sessions['sessions'][0]['best_ref'],'--context','5')
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert db.execute('SELECT native_id,model,started FROM sessions').fetchone()==('grok-native-id','grok-message-model','2026-09-27T10:15:00Z')
 source=db.execute('SELECT path,offset,size,tool_calls FROM sources').fetchone()
 assert source[0].endswith('/chat_history.jsonl') and source[1]==source[2]
 assert json.loads(source[3])[0]['Name']=='bash'
assert fingerprints()==before
print('PASS: search/CJK, --harness grok, --sessions, show, summary metadata, malformed-line recovery, compact tools; sources unchanged.')
print('Resume: grok --help unavailable locally (command not found); only cd <cwd>, no invented resume flag.')
# Appended result resolves its name from the tool call persisted by the prior scan.
source=pathlib.Path(source[0])
with source.open('a') as f:
 f.write(json.dumps({'type':'tool_result','id':'pending','content':'grokappendresult: completed'})+'\n')
appended=fingerprints()
hit=page('--harness','grok','grokappendresult')
assert hit['total']==1 and hit['hits'][0]['snippet'].startswith('bash · '), hit
assert 'grok: 7 messages' in run('index') and '0 changed' in run('index')
assert fingerprints()==appended
print('PASS: incremental append ingests only the tail and resolves tool_result id across sync boundaries.')
# Missing/malformed summaries fall back to the URL-decoded cwd and session directory ID.
root=home/'.grok/sessions'; fallback=root/'%2Fwork%2Fgrok%20fallback'/'fallback-id'; fallback.mkdir()
(fallback/'summary.json').write_text('{broken')
(fallback/'chat_history.jsonl').write_text(json.dumps({'type':'assistant','content':'grokfallback','model_id':'fallback-model'})+'\n')
empty=root/'%2Fwork%2Fgrok%20fallback'/'empty-id'; empty.mkdir()
(empty/'summary.json').write_text('{}'); (empty/'chat_history.jsonl').write_text('not-json\n')
(root/'unrelated.jsonl').write_text(json.dumps({'type':'user','content':'grokignore'})+'\n')
fallback_show=page('show','fallback-id')
assert fallback_show['cwd']=='/work/grok fallback'
assert page('--sessions','--harness','grok')['total']==2
assert page('grokignore')['total']==0
(fallback/'summary.json').unlink()
assert page('show','fallback-id')['cwd']=='/work/grok fallback'
# Explicit override wins; ~/ expands; a missing override never falls back.
for override,total in [('~/.grok/sessions',2),(str(home/'missing'),0),(str(root),2),('',2)]:
 env['KIOKU_GROK_DIR']=override
 assert page('--sessions','--harness','grok')['total']==total
print('PASS: summary fallback, native ID fallback, empty sessions, exact history discovery, override/tilde/missing-root behavior.')
PY
then record 'Grok: discovery, metadata, search/show/sessions, append, read-only' PASS $((SECONDS-start)) 'test/screens/grok.txt; resume is cd only (grok unavailable locally)'; else record 'Grok: discovery, metadata, search/show/sessions, append, read-only' FAIL $((SECONDS-start)) "$(<"$SCREENS/grok.txt")"; fi

# Cursor CLI ignores binary graph nodes and IDE stores; changes replace the whole session.
start=$SECONDS
if python3 - "$BIN" "$H/cursor-home" "$SCREENS/cursor.jsonl" <<'PY' > "$SCREENS/cursor.txt" 2>&1
import hashlib,json,os,pathlib,shlex,shutil,sqlite3,subprocess,sys
binary,home,artifact=sys.argv[1:]; home=pathlib.Path(home); root=home/'.cursor/chats'
meta=next(root.glob('*/666*/meta.json')); source=meta.with_name('store.db'); sid=meta.parent.name
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'))
def run(*args): return subprocess.check_output([binary,*args],env=env,text=True)
def page(*args): return json.loads(run(args[0],'--json',*args[1:]) if args[0]=='show' else run('--json',*args))
def fingerprints(): return {str(p.relative_to(root)): (hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mtime_ns,p.stat().st_size) for p in root.rglob('*') if p.is_file()}
before=fingerprints()
assert 'cursor: 3 messages' in run('index','--rebuild')
assert '0 changed' in run('index')
for query in ['cursorteatoken','魚池','日本語','한국어','cursoranswer','cursorlasttoken']:
 hit=page('--harness','cursor',query)
 assert hit['total']==1 and hit['hits'][0]['harness']=='cursor', hit
 assert page(query)['total']==1 and page('--harness','claude',query)['total']==0
for query in ['cursorbinaryhidden','cursorstatehidden','cursorsystemhidden','cursormalformedhidden','cursorimagehidden']:
 assert page(query)['total']==0, query
sessions=page('--sessions','--harness','cursor')
assert sessions['total']==1 and sessions['sessions'][0]['harness']=='cursor', sessions
shown=page('show',sid,'--all','--limit','20')
assert shown['cwd']=='/work/cursor demo' and shown['project']=='cursor demo', shown
assert shlex.split(shown['resume_cmd'])==shown['resume_argv']==['cursor-agent','--resume',sid], shown
assert [m['role'] for m in shown['messages']]==['user','asst','user'], shown
assert [m['text'] for m in shown['messages']]==['cursorteatoken 魚池 日本語 한국어','cursoranswer  second line','cursorlasttoken final line'], shown
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert [r[0] for r in db.execute('SELECT text FROM messages ORDER BY idx')]==['cursorteatoken 魚池 日本語 한국어','cursoranswer\n\nsecond line','cursorlasttoken\nfinal line']
 assert db.execute('SELECT started FROM sessions').fetchone()[0]=='2026-09-27T10:00:00Z'
 timestamps=db.execute('SELECT ts FROM messages ORDER BY idx').fetchall()
 from datetime import datetime
 assert all(abs(datetime.fromisoformat(t[0].replace('Z','+00:00')).timestamp()-source.stat().st_mtime)<0.00001 for t in timestamps)
assert fingerprints()==before
pathlib.Path(artifact).write_text(json.dumps(shown,ensure_ascii=False)+'\n')
print('PASS: rowid order, string/mixed text blocks, JSON-only messages, CJK, filters/sessions/show, metadata and DB-mtime timestamps; source bytes/mtime/size unchanged.')
# A same-size message edit (mtime only) must replace, not append, the session.
oldtime=source.stat().st_mtime_ns; oldsize=source.stat().st_size
with sqlite3.connect(source) as db:
 db.execute("UPDATE blobs SET data=? WHERE id='a'",(json.dumps({'role':'assistant','content':'cursoreditetoken'}),))
assert source.stat().st_size==oldsize
os.utime(source,ns=(oldtime+1000000000,oldtime+1000000000))
assert page('cursoreditetoken')['total']==1 and page('cursoranswer')['total']==0
assert page('show',sid,'--all')['session_total']==3
# Growth with the old mtime also invalidates the cache (size-only change).
oldtime=source.stat().st_mtime_ns; oldsize=source.stat().st_size
with sqlite3.connect(source) as db:
 db.execute('INSERT INTO blobs VALUES(?,?)',('growth',json.dumps({'role':'assistant','content':'cursorgrowthtoken '+('x'*20000)})))
assert source.stat().st_size>oldsize
os.utime(source,ns=(oldtime,oldtime))
assert page('cursorgrowthtoken')['total']==1
assert page('show',sid,'--all')['session_total']==4
# Metadata mtime-only and size-only changes also reparse cwd/created time.
oldsize=meta.stat().st_size; oldtime=meta.stat().st_mtime_ns
meta.write_text(meta.read_text().replace('cursor demo','cursor edit'))
assert meta.stat().st_size==oldsize
os.utime(meta,ns=(oldtime+1000000000,oldtime+1000000000))
assert page('show',sid)['cwd']=='/work/cursor edit'
oldtime=meta.stat().st_mtime_ns
meta.write_text(json.dumps({'cwd':'/work/cursor relocated project','createdAtMs':1790503260000}))
os.utime(meta,ns=(oldtime,oldtime))
assert page('show',sid)['cwd']=='/work/cursor relocated project'
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert db.execute('SELECT started FROM sessions').fetchone()[0]=='2026-09-27T10:01:00Z'
meta.write_text('{}')
assert page('--sessions','--harness','cursor')['total']==0
meta.write_text(json.dumps({'cwd':'/work/cursor demo','createdAtMs':1790503200000}))
assert page('--sessions','--harness','cursor')['total']==1
# Emptying the graph removes stale hits, and subsequent messages restore the session.
with sqlite3.connect(source) as db: db.execute('DELETE FROM blobs')
assert page('cursorteatoken')['total']==0
with sqlite3.connect(source) as db:
 db.execute('INSERT INTO blobs VALUES(?,?)',('final',json.dumps({'role':'user','content':'cursorfinaltoken'})))
assert page('cursorfinaltoken')['total']==1 and '0 changed' in run('index')
print('PASS: DB mtime/size and metadata changes trigger whole-session replacement; missing cwd and empty graphs remove stale hits.')
# Explicit override, tilde, spaces, absent override with no default fallback.
relocated=home/'relocated cursor'; shutil.copytree(root,relocated)
for override,total in [('~/.cursor/chats',1),(str(relocated),1),(str(home/'missing'),0),('',1)]:
 env['KIOKU_CURSOR_DIR']=override
 assert page('--sessions','--harness','cursor')['total']==total
 assert not (home/'missing').exists()
before=fingerprints(); assert '0 changed' in run('index'); assert fingerprints()==before
print('PASS: KIOKU_CURSOR_DIR override/tilde/spaces/missing-root behavior; repeat no-op scan leaves stores unchanged.')
PY
then record 'Cursor: JSON graph, rowid order, CJK, metadata, change detection, read-only' PASS $((SECONDS-start)) 'test/screens/cursor.txt and cursor.jsonl'; else record 'Cursor: JSON graph, rowid order, CJK, metadata, change detection, read-only' FAIL $((SECONDS-start)) "$(<"$SCREENS/cursor.txt")"; fi
if command -v cursor-agent >/dev/null 2>&1; then
  cursor_help=$(cursor-agent --help 2>&1)
  if grep -q -- '--resume' <<<"$cursor_help"; then record 'Cursor resume flag verification' PASS 0 'cursor-agent --help lists --resume'; else record 'Cursor resume flag verification' FAIL 0 "$cursor_help"; fi
else
  skip 'Cursor resume flag verification' 'cursor-agent unavailable; cursor-agent --resume <id> is unverified locally'
fi

# OpenCode is DB-backed: both layouts, WAL visibility, whole-session replacement, no store writes.
start=$SECONDS
if python3 - "$BIN" "$H/opencode-home" <<'PY' > "$SCREENS/opencode.txt" 2>&1
import hashlib,json,os,pathlib,shlex,shutil,sqlite3,subprocess,sys
binary,home=sys.argv[1:]; home=pathlib.Path(home); source=home/'.local/share/opencode/opencode.db'
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'))
def run(*args): return subprocess.check_output([binary,*args],env=env,text=True)
def page(*args): return json.loads(run(args[0],'--json',*args[1:]) if args[0]=='show' else run('--json',*args))
def fingerprints(): return {p.name:p.read_bytes() if p.name.endswith('-shm') else hashlib.sha256(p.read_bytes()).digest() for p in source.parent.iterdir() if p.suffix=='.db' or p.name.endswith(('-wal','-shm'))}
def unchanged(before):
 after=fingerprints()
 assert after.keys()==before.keys(), (before.keys(),after.keys())
 for name,old in before.items():
  new=after[name]
  if name.endswith('-shm'):
   # mode=ro can rebuild the WAL-index header and update reader coordination.
   # https://www.sqlite.org/walformat.html#the_wal_index_header
   # iChange/checksums/read-marks/nBackfillAttempted are ephemeral; page/frame
   # maps, WAL identity, mxFrame, nPage and checkpoint nBackfill must not change.
   assert len(old)>=136 and len(old)==len(new)
   stable=[(0,8),(12,40),(48,56),(60,88),(96,100),(120,128),(132,len(old))]
   assert all(old[a:b]==new[a:b] for a,b in stable), name
   if old!=new: print('PASS: SHM fingerprint changed only in SQLite WAL-index recovery/reader coordination fields; frame/page maps and checkpoint position unchanged.')
  else:
   assert old==new, name
before=fingerprints()
assert 'opencode: 9 messages' in run('index','--rebuild')
assert '0 changed' in run('index')
for query in ['opencodevone','魚池','ocanswer','octooltoken','ocoutputtoken','opencodevtwo','ocv2answer','ocv2tool']:
 hit=page('--harness','opencode',query)
 assert hit['total']==1 and hit['hits'][0]['harness']=='opencode', hit
 assert page(query)['total']==1
 assert page('--harness','claude',query)['total']==0
for query in ['ocsubagenthidden','ocreasoninghidden','ocv1duplicatehidden','ocv2childhidden','ocv2reasonhidden']:
 assert page(query)['total']==0, query
sessions=page('--sessions','--harness','opencode')
assert sessions['total']==2, sessions
shown=page('show','oc-v1','--all','--limit','20')
assert shown['cwd']=='/work/opencode demo' and shown['project']=='opencode demo', shown
assert shlex.split(shown['resume_cmd'])==shown['resume_argv']==['opencode','--session','oc-v1'], shown
assert [m['text'] for m in shown['messages']]==['opencodevone 魚池','ocanswer','read · ocnotes.txt','bash · printf octooltoken','read · ocoutputtoken','octietoken'], shown
assert [m['role'] for m in shown['messages']]==['user','asst','tool','tool','tool','asst'], shown
v2=page('show','oc-both','--all','--limit','20')
assert [m['text'] for m in v2['messages']]==['opencodevtwo','ocv2answer','tool · ocv2tool'], v2
assert [m['role'] for m in v2['messages']]==['user','asst','tool'], v2
assert v2['cwd']=='/work/opencode v2'
assert shown['resume_cmd'] in run('show','oc-v1')
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert db.execute('SELECT model FROM sessions WHERE native_id="oc-v1"').fetchone()[0]=='oc-model'
 assert db.execute('SELECT path,updated,offset FROM sources ORDER BY path').fetchall()==[(str(source)+'#oc-both',1790503202000,0),(str(source)+'#oc-v1',1790503202000,0)]
unchanged(before)
print('PASS: v1/v2 search, CJK, harness filter, sessions/show, timestamps/model/cwd, message/part ordering, tools, reasoning/subagent exclusion, v2 precedence; source bytes unchanged.')
# An open WAL writer leaves committed rows in the WAL, not the main database.
writer=sqlite3.connect(source)
assert writer.execute('PRAGMA journal_mode=WAL').fetchone()[0]=='wal'
writer.execute('PRAGMA wal_autocheckpoint=0')
writer.execute('INSERT INTO message VALUES(?,?,?,?)',('m4','oc-v1',1790503205000,json.dumps({'role':'assistant','time':{'created':1790503205000}})))
writer.execute('INSERT INTO part VALUES(?,?,?,?)',('p8','m4','oc-v1',json.dumps({'type':'text','text':'ocwalappend'})))
writer.commit(); before=fingerprints()
assert '1 changed' in run('index'), run('index')
assert page('ocwalappend')['total']==1
assert '0 changed' in run('index')
unchanged(before)
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert db.execute('SELECT updated FROM sources WHERE path=?',(str(source)+'#oc-v1',)).fetchone()[0]==1790503205000
# Changing old content reparses/replaces the session, not an append or duplicate.
writer.execute('UPDATE part SET data=? WHERE id="p1"',(json.dumps({'type':'text','text':'oceditedtoken'}),))
writer.execute('UPDATE session SET time_updated=1790503206000 WHERE id="oc-v1"'); writer.commit()
assert page('oceditedtoken')['total']==1 and page('opencodevone')['total']==0
assert page('ocwalappend')['total']==1
# Metadata-only no-op discovery must not decode unchanged parts.
writer.execute('UPDATE part SET data="not-json" WHERE id="p1"'); writer.commit()
assert '0 changed' in run('index') and page('oceditedtoken')['total']==1
writer.execute('UPDATE part SET data=? WHERE id="p1"',(json.dumps({'type':'text','text':'oceditedtoken'}),))
writer.execute('INSERT INTO session_message VALUES(?,?,?,?,?,?,?)',('next','oc-both','assistant',5,1790503207000,1790503207000,'{"text":"ocv2append"}')); writer.commit()
before=fingerprints()
assert page('ocv2append')['total']==1 and page('ocv2answer')['total']==1
assert '0 changed' in run('index')
unchanged(before)
# A v2 child still masks v1; making it empty removes previously indexed rows.
writer.execute('UPDATE session_v2 SET parent_id="oc-v1" WHERE id="oc-both"'); writer.commit()
assert page('opencodevtwo')['total']==0 and page('ocv1duplicatehidden')['total']==0
writer.execute('UPDATE session_v2 SET parent_id=NULL,time_updated=1790503208000 WHERE id="oc-both"')
writer.execute('DELETE FROM session_message WHERE session_id="oc-both"'); writer.commit()
assert page('ocv2append')['total']==0
assert '0 changed' in run('index')
writer.execute('DELETE FROM session WHERE id="oc-v1"'); writer.commit()
assert page('oceditedtoken')['total']==0
writer.close()
# Restore v1 for the override and TUI checks below.
with sqlite3.connect(source) as db:
 db.execute('INSERT INTO session VALUES(?,?,?,?,?,?,?)',('oc-v1','p',None,'/work/opencode demo','v1',1790503200000,1790503206000))
assert page('oceditedtoken')['total']==1
print('PASS: live WAL messages visible without checkpoint/write; latest message timestamp triggers sync, edits/empty/deleted sessions replace or prune rows, unchanged sessions are not reparsed, v2 subagents mask v1.')
# Independent schemas, XDG location, overrides/tilde/missing paths.
for filename,query in [('v2.db','opencodevtwo'),('minimal-v1.db','opencodevone')]:
 env['KIOKU_OPENCODE_DB']=str(source.parent/filename)
 assert page('--harness','opencode',query)['total']==1
 assert '0 changed' in run('index')
xdg=home/'xdg data'; (xdg/'opencode').mkdir(parents=True)
shutil.copyfile(source.parent/'v2.db',xdg/'opencode/opencode.db')
env.pop('KIOKU_OPENCODE_DB'); env['XDG_DATA_HOME']=str(xdg)
assert page('opencodevtwo')['total']==1 and page('oceditedtoken')['total']==0
env['KIOKU_OPENCODE_DB']='~/.local/share/opencode/opencode.db'
assert page('oceditedtoken')['total']==1
for override in [str(home/'missing.db'),str(home/'missing/opencode.db')]:
 env['KIOKU_OPENCODE_DB']=override
 assert page('--sessions','--harness','opencode')['total']==0
 assert not pathlib.Path(override).exists()
env['KIOKU_OPENCODE_DB']=''; env['XDG_DATA_HOME']=''
assert page('oceditedtoken')['total']==1
# Unsupported schema is diagnosed, other harnesses keep working.
bad=home/'unsupported.db'; sqlite3.connect(bad).close()
shutil.copytree(home.parent/'.claude',home/'.claude')
def indexed_opencode():
 with sqlite3.connect(env['KIOKU_INDEX']) as db:
  return [db.execute(q).fetchall() for q in [
   "SELECT * FROM sessions WHERE harness='opencode' ORDER BY uid",
   "SELECT * FROM messages WHERE session_uid IN (SELECT uid FROM sessions WHERE harness='opencode') ORDER BY id",
   "SELECT * FROM sources WHERE path IN (SELECT path FROM sessions WHERE harness='opencode') ORDER BY path"]]
preserved=indexed_opencode()
assert preserved[0] and preserved[1] and preserved[2]
env['KIOKU_OPENCODE_DB']=str(bad)
for flags in [[],['--rebuild']]:
 result=subprocess.run([binary,'index',*flags],env=env,text=True,capture_output=True)
 assert result.returncode==0 and 'opencode' in result.stderr.lower() and 'session' in result.stderr.lower(), result
 assert 'claude: 5 messages' in result.stdout, result
 assert indexed_opencode()==preserved, (flags,indexed_opencode())
 assert page('oceditedtoken')['total']==1
# Rebuild still clears empty transcripts in healthy harnesses, not just changed ones.
claude_file=next((home/'.claude').rglob('*.jsonl')); claude_original=claude_file.read_bytes()
claude_file.write_bytes(b'')
result=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True)
assert result.returncode==0 and 'claude: 0 messages' in result.stdout, result
assert indexed_opencode()==preserved
claude_file.write_bytes(claude_original)
# A transient invalid database must also preserve the last good index, then recover.
env['KIOKU_OPENCODE_DB']=''
original=source.read_bytes(); source.write_bytes(b'not a sqlite database')
result=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True)
assert result.returncode==0 and 'opencode' in result.stderr.lower(), result
assert indexed_opencode()==preserved and 'claude: 5 messages' in result.stdout
source.write_bytes(original)
assert page('oceditedtoken')['total']==1 and '0 changed' in run('index')
print('PASS: v2-only and minimal v1 layouts; XDG/override/tilde/empty/missing paths; invalid schemas and transient load failures preserve OpenCode rows (including rebuild), other harnesses sync, recovery succeeds.')
PY
then record 'OpenCode: v1/v2, WAL, search/show/filter, reindex, read-only' PASS $((SECONDS-start)) 'test/screens/opencode.txt'; else record 'OpenCode: v1/v2, WAL, search/show/filter, reindex, read-only' FAIL $((SECONDS-start)) "$(<"$SCREENS/opencode.txt")"; fi

# Self lookups are hidden only in search; retained rows keep refs and paging stable.
start=$SECONDS
if python3 - "$BIN" "$H/self-lookups" <<'PY' > "$SCREENS/exclude-self.txt" 2>&1
import hashlib,json,os,pathlib,sqlite3,subprocess,sys
binary,home=sys.argv[1:]; home=pathlib.Path(home)
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'))
def run(*args): return subprocess.check_output([binary,*args],env=env,text=True)
def page(*args): return json.loads(run('--json',*args))
def fingerprints(): return {str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*.jsonl')}
before=fingerprints()
for mode in [[],['--sessions']]:
 assert page(*mode,'-p','self-lookups','secretword')['total']==0
 assert page(*mode,'--include-self','-p','self-lookups','secretword')['total']==(3 if mode else 18)
 assert page(*mode,'secretword')['total']==(1 if mode else 2)
 assert page(*mode,'--include-self','secretword')['total']==(4 if mode else 20)
 for flags in [[],['--include-self']]:
  args=[*mode,*flags,'--limit','1','secretword']; first=page(*args); current=first; refs=[]
  while True:
   rows=current['sessions' if mode else 'hits']; refs += [r['ref'] for r in rows]
   assert current['total']==first['total'] and current['shown']==len(rows)
   if not current.get('next_cursor'): break
   current=page(*args,'--cursor',current['next_cursor'])
  assert len(refs)==len(set(refs))==first['total']
  if first.get('next_cursor'):
   changed=[*mode,*(['--include-self'] if not flags else []),'--limit','1','secretword','--cursor',first['next_cursor']]
   bad=subprocess.run([binary,'--json',*changed],env=env,text=True,capture_output=True)
   assert bad.returncode and 'cursor' in bad.stderr, bad
 assert page(*mode,'mentionprobe')['total']==(1 if mode else 6)
 assert page(*mode,'ordinaryprobe')['total']==(3 if mode else 6)
 assert page(*mode,'-p','self-lookups')['total']==3  # newest non-self message per session
 assert page(*mode,'--include-self','-p','self-lookups')['total']==3
for harness in ['claude','codex','pi']:
 assert page('--harness',harness,'-p','self-lookups','secretword')['total']==0
 assert page('--include-self','--harness',harness,'-p','self-lookups','secretword')['total']==6
assert fingerprints()==before
print('Default hides 18 lookup rows; include-self restores them; mentions and ordinary tools survive; both paging modes are consistent.')
# Simulate a pre-feature index; a failed read must roll back and leave migration pending.
meta_only=home/'.codex/sessions/meta-only.jsonl'
meta_only.write_text(json.dumps({'type':'session_meta','payload':{'id':'meta-only'}})+'\n')
before=fingerprints()
with sqlite3.connect(home/'index.db') as db:
 # Stock SQLite lacks cjk; detach FTS triggers while ALTER TABLE validates the schema.
 triggers=db.execute("SELECT name,sql FROM sqlite_master WHERE type='trigger' AND name GLOB 'messages_*'").fetchall()
 for name,_ in triggers: db.execute(f'DROP TRIGGER "{name}"')
 db.execute('ALTER TABLE messages DROP COLUMN self')
 for _,sql in triggers: db.execute(sql)
 db.execute('UPDATE meta SET schema_version=2')
 old_rows=db.execute('SELECT id,session_uid,idx,ts,role,text FROM messages ORDER BY id').fetchall()
 old_sources=db.execute('SELECT * FROM sources ORDER BY path').fetchall()
# A symlink to a nonempty directory reliably fails reading, even when tests run as root.
source=home/'.pi/agent/sessions/self/self.jsonl'; saved=source.with_suffix('.saved')
unreadable=home/'unreadable'; unreadable.mkdir(); (unreadable/'entry').write_text('synthetic')
source.rename(saved); source.symlink_to(unreadable,target_is_directory=True)
try:
 failed=subprocess.run([binary,'index'],env=env,text=True,capture_output=True)
 assert failed.returncode and 'migration' in failed.stderr, failed
 with sqlite3.connect(home/'index.db') as db:
  assert db.execute('SELECT schema_version FROM meta').fetchone()[0]==2
  assert db.execute('SELECT id,session_uid,idx,ts,role,text FROM messages ORDER BY id').fetchall()==old_rows
  assert db.execute('SELECT * FROM sources ORDER BY path').fetchall()==old_sources
finally:
 source.unlink(); saved.rename(source)
assert fingerprints()==before
assert page('-p','self-lookups','secretword')['total']==0
with sqlite3.connect(home/'index.db') as db:
 assert db.execute('SELECT schema_version FROM meta').fetchone()[0]>2
 assert db.execute('SELECT count(*) FROM messages WHERE self=1').fetchone()[0]==21
assert fingerprints()==before
print('Failed migration preserves old rows and version; retry reparses unchanged files, restores self markers, and accepts metadata-only sources.')
# A result appended after a prior sync still inherits its matching call marker.
for harness,store in [('claude','.claude/projects/self'),('codex','.codex/sessions'),('pi','.pi/agent/sessions/self')]:
 if harness=='claude': row={'type':'user','message':{'content':[{'type':'tool_result','tool_use_id':'pending','content':'pendingsecret result'}]}}
 elif harness=='codex': row={'type':'response_item','payload':{'type':'function_call_output','call_id':'pending','output':'pendingsecret result'}}
 else: row={'type':'message','message':{'role':'toolResult','toolCallId':'pending','toolName':'bash','content':[{'type':'text','text':'pendingsecret result'}]}}
 with (home/store/'self.jsonl').open('a') as f: f.write(json.dumps(row)+'\n')
appended=fingerprints()
assert page('pendingsecret')['total']==0
assert page('--include-self','pendingsecret')['total']==6
assert fingerprints()==appended
print('Append-only matching works across sync boundaries for all three harnesses; transcript bytes unchanged by searches.')
# Force the common-query/newest-matches path with both visible and hidden tools.
common=home/'.codex/sessions/common.jsonl'
rows=[{'type':'session_meta','payload':{'id':'common','cwd':'/work/common'}}]
for i in range(400):
 rows.append({'type':'response_item','timestamp':'2026-09-27T11:00:00Z','payload':{'type':'function_call','call_id':str(i),'name':'shell','arguments':json.dumps({'command':('kioku ' if i%2 else 'echo ')+'commonselfprobe'})}})
common.write_text(''.join(json.dumps(r)+'\n' for r in rows))
for flags,total in [([],200),(['--include-self'],400)]:
 first=page(*flags,'--limit','2','commonselfprobe')
 second=page(*flags,'--limit','2','commonselfprobe','--cursor',first['next_cursor'])
 assert first['total']==second['total']==total
 assert len({r['ref'] for r in first['hits']+second['hits']})==4
 grouped=page('--sessions',*flags,'commonselfprobe')
 assert grouped['total']==1 and grouped['sessions'][0]['roles']['tool']==total
print('Common-query fast path filters counts, session roles and consecutive pages consistently.')
# Hidden future lookups must not move the default short-prefix date window.
short=home/'.claude/projects/normal/short.jsonl'
rows=[{'type':'user','timestamp':ts,'cwd':'/work/short','sessionId':'short','message':{'content':'xy conversation'}} for ts in ['2026-09-01T00:00:00Z','2026-09-27T00:00:00Z']]
rows += [{'type':'assistant','timestamp':'2026-11-01T00:00:00Z','message':{'content':[{'type':'tool_use','id':'xy','name':'bash','input':{'command':'kioku xy'}}]}}, {'type':'user','timestamp':'2026-11-01T00:00:00Z','message':{'content':[{'type':'tool_result','tool_use_id':'xy','content':'xy lookup result'}]}}]
short.write_text(''.join(json.dumps(r)+'\n' for r in rows))
for mode in [[],['--sessions']]:
 default=page(*mode,'xy'); included=page(*mode,'--include-self','xy')
 assert default['total']==1 and default['omitted_older']==1
 assert included['total']==(1 if mode else 2) and included['omitted_older']==2
 assert page(*mode,'--all-time','xy')['total']==(1 if mode else 2)
 assert page(*mode,'--include-self','--all-time','xy')['total']==(1 if mode else 4)
print('Short-prefix date windows and omitted-older counts exclude self by default.')
# Shell assignment escapes must not expose lookup calls or their results.
escaped=home/'.claude/projects/normal/escaped.jsonl'
rows=[]
for i,cmd in enumerate([r'FOO=two\ words kioku --sessions escapedsecret', r'command FOO=two\ words kioku escapedsecret', r'FOO="two\" words" kioku escapedsecret']):
 rows += [{'type':'assistant','cwd':'/work/escaped','sessionId':'escaped','message':{'content':[{'type':'tool_use','id':str(i),'name':'bash','input':{'command':cmd}}]}}, {'type':'user','message':{'content':[{'type':'tool_result','tool_use_id':str(i),'content':'escapedsecret result'}]}}]
escaped.write_text(''.join(json.dumps(r)+'\n' for r in rows))
escaped_before=fingerprints()
for mode in [[],['--sessions']]:
 assert page(*mode,'escapedsecret')['total']==0
 assert page(*mode,'--include-self','escapedsecret')['total']==(1 if mode else 6)
assert fingerprints()==escaped_before
print('Escaped whitespace and quotes in shell assignments hide matching calls/results by default and restore them with include-self.')
PY
then record 'Exclude self: filtering, mentions, paging, migration, append' PASS $((SECONDS-start)) 'test/screens/exclude-self.txt'; else record 'Exclude self: filtering, mentions, paging, migration, append' FAIL $((SECONDS-start)) "$(<"$SCREENS/exclude-self.txt")"; fi

# Native config dirs are below KIOKU overrides; missing explicit roots never fall back.
start=$SECONDS
if python3 - "$BIN" "$H" <<'PY' > "$SCREENS/env-dirs.txt" 2>&1
import hashlib,json,os,pathlib,sqlite3,subprocess,sys
binary,home=sys.argv[1:]; home=pathlib.Path(home)
stores=[('claude','KIOKU_CLAUDE_DIR','CLAUDE_CONFIG_DIR','.claude/projects','projects'),('codex','KIOKU_CODEX_DIR','CODEX_HOME','.codex/sessions','sessions'),('pi','KIOKU_PI_DIR','PI_CODING_AGENT_DIR','.pi/agent/sessions','sessions')]
def fingerprints():
 return {str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*.jsonl')}
before=fingerprints()
for harness,override,native,default,child in stores:
 config=f'relocated/{harness} config'; relocated=home/config/child
 cases=[
  ('unset',{},home/default),
  ('empty',{override:'',native:''},home/default),
  ('native',{native:str(home/config)},relocated),
  ('native tilde',{native:f'~/{config}'},relocated),
  ('native tilde parent',{native:f'~/../{home.name}/{config}'},relocated),
  ('empty override',{override:'',native:str(home/config)},relocated),
  ('KIOKU wins',{override:str(home/default),native:str(home/config)},home/default),
  ('KIOKU tilde wins',{override:f'~/{default}',native:str(home/config)},home/default),
  ('KIOKU tilde parent',{override:f'~/../{home.name}/{default}',native:str(home/config)},home/default),
  ('missing native',{native:str(home/'missing')},None),
  ('missing KIOKU wins',{override:str(home/'missing'),native:str(home/config)},None),
  ('empty native',{native:''},home/default),
 ]
 env=os.environ.copy(); env['KIOKU_INDEX']=str(home/'index'/f'env-{harness}.db')
 for name,values,expected in cases:
  current=env|values
  index=subprocess.run([binary,'index'],env=current,text=True,capture_output=True)
  assert index.returncode==0, index.stderr
  search=subprocess.run([binary,'--json','--harness',harness,'recoverytoken'],env=current,text=True,capture_output=True)
  assert search.returncode==0, search.stderr
  page=json.loads(search.stdout)
  print(json.dumps({'harness':harness,'case':name,'index':index.stdout.strip(),'search':page}),flush=True)
  assert page['total']==(1 if expected else 0), (harness,name,page)
  assert all(hit['harness']==harness for hit in page['hits']), page
  with sqlite3.connect(current['KIOKU_INDEX']) as db:
   paths=db.execute('SELECT harness,path FROM sessions').fetchall()
  for other,_,_,other_default,_ in stores:
   root=expected if other==harness else home/other_default
   actual=[path for agent,path in paths if agent==other]
   wanted=sorted(str(p) for p in root.rglob('*.jsonl')) if root else []
   assert sorted(actual)==wanted, (harness,name,other,actual,wanted)
   assert f'{other}: {5 if root else 0} messages' in index.stdout, index.stdout
assert fingerprints()==before, 'transcript stores changed'
print('36 cases: native/KIOKU precedence, empty vars, tilde (including parent traversal), missing roots, independent harnesses, root switching, read-only stores')
PY
then record 'Environment source-dir precedence and expansion' PASS $((SECONDS-start)) "$(tail -1 "$SCREENS/env-dirs.txt")"; else record 'Environment source-dir precedence and expansion' FAIL $((SECONDS-start)) "$(tail -5 "$SCREENS/env-dirs.txt")"; fi

# New, modified, and deleted sources must sync without duplicate rows.
orig=$(echo "$H"/.claude/projects/*/*.jsonl); clone="$H/.claude/projects/-work-demo/44444444-4444-4444-8444-444444444444.jsonl"
python3 - "$orig" "$clone" <<'PY'
import pathlib,sys
s=pathlib.Path(sys.argv[1]).read_text().replace('11111111-1111-4111-8111-111111111111','44444444-4444-4444-8444-444444444444')
s += '{"type":"user","message":{"role":"user","content":"Added syncmarker."},"timestamp":"2026-09-27T10:20:00Z","cwd":"/work/demo","sessionId":"44444444-4444-4444-8444-444444444444"}\n'
pathlib.Path(sys.argv[2]).write_text(s)
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; if ((rc==0)) && grep -q '4 files, 1 changed' <<<"$out" && grep -q 'claude: 11 messages' <<<"$out"; then record 'Incremental sync adds new file' PASS $((SECONDS-start)) "$out"; else record 'Incremental sync adds new file' FAIL $((SECONDS-start)) "$out"; fi
python3 - "$clone" <<'PY'
import pathlib,sys
p=pathlib.Path(sys.argv[1]); p.write_text(p.read_text().replace('syncmarker','syncchange'))
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; changed=$("$BIN" --json syncchange 2>&1); if ((rc==0)) && grep -q '4 files, 1 changed' <<<"$out" && jq -e '.total==1 and (.hits|length)==1 and (.hits[0].snippet|contains("syncchange"))' <<<"$changed" >/dev/null; then record 'Changed source is replaced, not duplicated' PASS $((SECONDS-start)) "$out"; else record 'Changed source is replaced, not duplicated' FAIL $((SECONDS-start)) "$out $changed"; fi
rm "$clone"; start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; removed=$("$BIN" --json syncchange 2>&1); if ((rc==0)) && grep -q 'claude: 5 messages' <<<"$out" && jq -e '.total==0 and .shown==0 and (.hits|length)==0' <<<"$removed" >/dev/null; then record 'Deleted source is removed from index' PASS $((SECONDS-start)) "$out"; else record 'Deleted source is removed from index' FAIL $((SECONDS-start)) "$out $removed"; fi
# Short prefixes report omitted older messages; --all-time restores them.
if python3 - "$BIN" "$TMP/short-prefix" <<'PY' > "$TMP/short-prefix.txt" 2> "$TMP/short-prefix.err"
import json,os,pathlib,subprocess,sys
binary,root=sys.argv[1:]; root=pathlib.Path(root)
sessions=root/'.pi/agent/sessions/demo'; sessions.mkdir(parents=True)
for sid,dates in [('old',['2026-01-01T00:00:00Z','2026-01-02T00:00:00Z']),('recent',['2026-01-25T00:00:00Z','2026-02-01T00:00:00Z'])]:
 rows=[{'type':'session','version':3,'id':sid,'timestamp':dates[0],'cwd':'/work/demo'}]
 rows += [{'type':'message','id':f'{sid}-{i}','timestamp':date,'message':{'role':'user','content':[{'type':'text','text':f'qw qword {sid} match {i}'}]}} for i,date in enumerate(dates)]
 (sessions/f'{sid}.jsonl').write_text('\n'.join(json.dumps(r) for r in rows)+'\n')
env=os.environ.copy(); env.update(HOME=str(root),KIOKU_INDEX=str(root/'index.db'))
def run(*args):
 p=subprocess.run([binary,*args],env=env,text=True,capture_output=True)
 assert p.returncode==0, p.stderr
 return p.stdout
for query in ['q','qw']:
 for mode in [[],['--sessions']]:
  page=json.loads(run('--json',*mode,query))
  assert page['total']==(1 if mode else 2) and page['omitted_older']==2, page
  text=run(*mode,query)
  assert text.rstrip().endswith('2 older matches omitted (--all-time)'), text
  page=json.loads(run('--json',*mode,query,'--all-time'))
  assert page['total']==(2 if mode else 4) and page.get('omitted_older',0)==0, page
  text=run('--all-time',*mode,query)
  assert 'older matches omitted' not in text and 'old match' in text, text
for query in ['"qw"','qword']:
 page=json.loads(run('--json',query))
 assert page['total']==4 and page.get('omitted_older',0)==0, page
page=json.loads(run('--json','qw','-recent'))
assert page['total']==0 and page['omitted_older']==2, page
assert run('qw','-recent').rstrip().endswith('2 older matches omitted (--all-time)')
for flags in [['--harness','claude'],['--project','absent']]:
 page=json.loads(run('--json',*flags,'qw'))
 assert page['total']==0 and page.get('omitted_older',0)==0, page
page=json.loads(run('--json','--limit','1','qw'))
assert page['shown']==1 and page['omitted_older']==2, page
next_page=json.loads(run('--json','--limit','1','--cursor',page['next_cursor'],'qw'))
assert next_page['shown']==1 and next_page['omitted_older']==2, next_page
mismatch=subprocess.run([binary,'--json','--limit','1','--cursor',page['next_cursor'],'--all-time','qw'],env=env,text=True,capture_output=True)
assert mismatch.returncode!=0 and 'cursor' in mismatch.stderr, mismatch
for mode in [[],['--sessions']]:
 for flags,toggled in [([],['--all-time']),(['--all-time'],[])]:
  page=json.loads(run('--json','--limit','1',*mode,*flags,'qword'))
  expected=json.loads(run('--json','--limit','1',*mode,*flags,'--cursor',page['next_cursor'],'qword'))
  next_page=json.loads(run('--json','--limit','1',*mode,*toggled,'--cursor',page['next_cursor'],'qword'))
  assert next_page==expected, (next_page,expected)
assert '--all-time' in run('--help') and 'omitted_older' in run('--help')
print('text/JSON hits and sessions, inclusive 7-day boundary, --all-time, filters, and cursors checked')
PY
then record 'Short-prefix omitted footer and --all-time' PASS 0 "$(<"$TMP/short-prefix.txt")"; else record 'Short-prefix omitted footer and --all-time' FAIL 0 "$(<"$TMP/short-prefix.err")"; fi

# One transcript deliberately puts matching user/asst messages before a short tool row.
python3 - "$H" <<'PY'
import json,pathlib,sys
h=pathlib.Path(sys.argv[1]); p=h/'.claude/projects/-work-demo/55555555-5555-4555-8555-555555555555.jsonl'
sid='55555555-5555-4555-8555-555555555555'; filler=' '.join(['conversation']*70)
rows=[
 {'type':'user','uuid':'55555555-5555-4555-8555-555555555551','timestamp':'2026-09-27T11:00:00Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'user','content':'rankprobe '+filler}},
 {'type':'assistant','uuid':'55555555-5555-4555-8555-555555555552','timestamp':'2026-09-27T11:00:03Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'assistant','content':[{'type':'text','text':'rankprobe '+filler},{'type':'tool_use','id':'tool-rank-1','name':'Bash','input':{'command':'rankprobe'}}]}},
 {'type':'user','uuid':'55555555-5555-4555-8555-555555555553','timestamp':'2026-09-27T11:00:04Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'user','content':[{'type':'tool_result','tool_use_id':'tool-rank-1','content':'toolprobeonly output from helper'}]}}
]
p.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; if ((rc==0)) && grep -q '4 files, 1 changed' <<<"$out"; then record 'Tool-order fixture indexed' PASS $((SECONDS-start)) "$out"; else record 'Tool-order fixture indexed' FAIL $((SECONDS-start)) "$out"; fi
ranked=$("$BIN" --json rankprobe 2>&1); printf '%s\n' "$ranked" > "$SCREENS/tool-order.jsonl"
if python3 - "$SCREENS/tool-order.jsonl" <<'PY' > "$TMP/tool-order.txt" 2> "$TMP/tool-order.err"
import json,sys
rows=json.load(open(sys.argv[1]))['hits']; roles=[r['role'] for r in rows]
conversation=[i for i,r in enumerate(roles) if r in ('user','asst')]; tools=[i for i,r in enumerate(roles) if r=='tool']
assert conversation and tools, f'expected user/asst and tool hits, got {roles}'
assert max(conversation)<min(tools), f'tool row ranked before conversation hits: {roles}'
for row in rows:
 text=row['snippet'].encode('utf-16-le'); assert row['highlights'], row
 for r in row['highlights']:
  assert text[2*r['location']:2*(r['location']+r['length'])].decode('utf-16-le').lower()=='rankprobe', row
print(f'roles={roles}')
PY
then record 'Conversation hits precede matching tool row in JSON order' PASS 0 "$(<"$TMP/tool-order.txt")"; else record 'Conversation hits precede matching tool row in JSON order' FAIL 0 "$(<"$TMP/tool-order.err")"; fi
tool_only=$("$BIN" --json toolprobeonly 2>&1); if jq -e 'any(.hits[]; .role=="tool" and (.snippet|contains("toolprobeonly")))' <<<"$tool_only" >/dev/null; then record 'Tool-only match remains searchable' PASS 0 ''; else record 'Tool-only match remains searchable' FAIL 0 "$tool_only"; fi

# Dedicated 23-hit session for stateless pages and show/context behavior.
python3 - "$H" <<'PY'
import json,pathlib,sys
h=pathlib.Path(sys.argv[1]); p=h/'.claude/projects/-work-demo/66666666-6666-4666-8666-666666666666.jsonl'; sid='66666666-6666-4666-8666-666666666666'; rows=[]
for i in range(23):
 text=f'pageprobe PAGE_HIT_{i:02d} '+('LongText '+('boundary '*120) if i==10 else 'short context')
 role='user' if i%2==0 else 'assistant'
 rows.append({'type':role,'uuid':f'66666666-6666-4666-8666-{i:012d}','timestamp':f'2026-09-28T12:{i:02d}:00Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':role,'content':text if role=='user' else [{'type':'text','text':text}]}})
p.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; if ((rc==0)) && grep -q '5 files, 1 changed' <<<"$out"; then record 'Pagination/show fixture indexed' PASS $((SECONDS-start)) "$out"; else record 'Pagination/show fixture indexed' FAIL $((SECONDS-start)) "$out"; fi
full_page=$("$BIN" --json --limit 500 pageprobe 2>&1); printf '%s\n' "$full_page" > "$TMP/page-full.json"
: > "$TMP/page-refs.txt"; page=$("$BIN" --json --limit 5 pageprobe 2>&1); page_count=0; cursor=''
while [[ -n $page ]]; do
  printf '%s\n' "$page" | jq -r '.hits[].ref' >> "$TMP/page-refs.txt" || break
  cursor=$(printf '%s\n' "$page" | jq -r '.next_cursor // empty'); page_count=$((page_count+1))
  [[ -z $cursor || $page_count -ge 20 ]] && break
  page=$("$BIN" --json --limit 5 --cursor "$cursor" pageprobe 2>&1)
done
python3 - "$TMP/page-full.json" "$TMP/page-refs.txt" <<'PY' > "$TMP/page-check.txt" 2> "$TMP/page-error.txt"
import json,sys
full=json.load(open(sys.argv[1])); expected=[x['ref'] for x in full['hits']]; actual=[x.strip() for x in open(sys.argv[2]) if x.strip()]
assert full['total']==23 and len(expected)==23, f'full page expected 23 hits, got total={full["total"]} shown={len(expected)}'
assert len(actual)==len(set(actual)), 'cursor pages contain duplicate refs'
assert actual==expected, f'cursor union skipped/reordered refs: {len(actual)} vs {len(expected)}'
print(f'{len(actual)} unique refs across cursor pages; union equals --limit 500')
PY
if [[ -s "$TMP/page-check.txt" ]]; then record 'Cursor pages cover every hit exactly once' PASS "$page_count" "$(<"$TMP/page-check.txt")"; else record 'Cursor pages cover every hit exactly once' FAIL "$page_count" "$(<"$TMP/page-error.txt")"; fi

# Default compact page is ten; the cursor footer/JSON field exist iff more remain.
text_first=$("$BIN" pageprobe 2>&1); json_first=$("$BIN" --json pageprobe 2>&1)
text_cursor=$(sed -n 's/^cursor: //p' <<<"$text_first"); json_cursor=$(jq -r '.next_cursor // empty' <<<"$json_first")
start=$SECONDS; text_second=$("$BIN" --cursor "$text_cursor" pageprobe 2>&1); text_cursor2=$(sed -n 's/^cursor: //p' <<<"$text_second"); text_last=$("$BIN" --cursor "$text_cursor2" pageprobe 2>&1)
json_second=$("$BIN" --json --cursor "$json_cursor" pageprobe 2>&1); json_cursor2=$(jq -r '.next_cursor // empty' <<<"$json_second"); json_last=$("$BIN" --json --cursor "$json_cursor2" pageprobe 2>&1)
if grep -q '^10/23 hits' <<<"$text_first" && [[ $(grep -Ec '^[[:xdigit:]]{12}:[0-9]+  ' <<<"$text_first") == 10 ]] && [[ -n $text_cursor && -n $json_cursor ]] && jq -e '.shown==10 and .total==23' <<<"$json_first" >/dev/null && grep -q '^10/23 hits' <<<"$text_second" && [[ -n $text_cursor2 && -n $json_cursor2 ]] && grep -q '^3/23 hits' <<<"$text_last" && ! grep -q '^cursor:' <<<"$text_last" && jq -e '.shown==3 and .total==23 and (has("next_cursor")|not)' <<<"$json_last" >/dev/null; then record 'Default page size and cursor footer/JSON parity' PASS $((SECONDS-start)) '10 + 10 + 3 rows; text and JSON cursors appear only while more remain'; else record 'Default page size and cursor footer/JSON parity' FAIL $((SECONDS-start)) "first=$text_first last=$text_last json_last=$json_last"; fi
bad_query=$("$BIN" --json --cursor "$json_cursor" snapshot 2>&1); bad_query_rc=$?
bad_flags=$("$BIN" --json --limit 5 --cursor "$json_cursor" pageprobe 2>&1); bad_flags_rc=$?
if ((bad_query_rc!=0 && bad_flags_rc!=0)) && grep -q 'cursor does not match this query or flags' <<<"$bad_query" && grep -q 'cursor does not match this query or flags' <<<"$bad_flags"; then record 'Cursor rejects changed query and flags clearly' PASS 0 ''; else record 'Cursor rejects changed query and flags clearly' FAIL 0 "query=$bad_query flags=$bad_flags"; fi

# Grouped session totals/hit counts agree with message-level results and top session.
hits_snapshot=$("$BIN" --json snapshot 2>&1); sessions_snapshot=$("$BIN" --json --sessions snapshot 2>&1)
printf '%s\n' "$hits_snapshot" > "$TMP/snapshot-hits.json"; printf '%s\n' "$sessions_snapshot" > "$TMP/snapshot-sessions.json"
python3 - "$TMP/snapshot-hits.json" "$TMP/snapshot-sessions.json" <<'PY' > "$TMP/sessions-check.txt" 2> "$TMP/sessions-error.txt"
import json,sys
h=json.load(open(sys.argv[1])); s=json.load(open(sys.argv[2]))
assert s['total']==h['total_sessions'] and sum(x['hits'] for x in s['sessions'])==h['total'], f'hit/session totals disagree: {h["total"]}/{h["total_sessions"]} vs {s["total"]}/{sum(x["hits"] for x in s["sessions"])}'
assert s['sessions'] and h['hits'][0]['ref'].split(':',1)[0]==s['sessions'][0]['ref'], 'sessions are not ranked by their best hit'
print(f'{h["total"]} hits across {h["total_sessions"]} sessions; counts sum and best-hit rank agrees')
PY
if [[ -s "$TMP/sessions-check.txt" ]]; then record '--sessions ranking/counts agree with hit list' PASS 0 "$(<"$TMP/sessions-check.txt")"; else record '--sessions ranking/counts agree with hit list' FAIL 0 "$(<"$TMP/sessions-error.txt")"; fi

# show ref exposes context, resume metadata, hit marker, truncation, and --all cursor pages.
show_ref=$(jq -r '.hits[] | select(.ref|endswith(":10")) | .ref' "$TMP/page-full.json")
show_json=$("$BIN" show "$show_ref" --context 2 --query PAGE_HIT_10 --json 2>&1); printf '%s\n' "$show_json" > "$TMP/show-context.json"
show_text=$("$BIN" show "$show_ref" --context 2 2>&1)
python3 - "$TMP/show-context.json" <<'PY' > "$TMP/show-check.txt" 2> "$TMP/show-error.txt"
import json,sys
p=json.load(open(sys.argv[1])); selected=[m for m in p['messages'] if m['hit']]
assert p['shown']==5 and p['total']==5 and len(selected)==1, f'expected selected hit and 2 context messages each side, got {p["shown"]}/{p["total"]}'
assert 'PAGE_HIT_08' in p['messages'][0]['text'] and 'PAGE_HIT_12' in p['messages'][-1]['text'], 'context bounds incorrect'
assert p['resume_cmd']=='claude --resume 66666666-6666-4666-8666-666666666666', p['resume_cmd']
assert p['resume_argv']==['claude','--resume','66666666-6666-4666-8666-666666666666'], p
assert selected[0]['matches'] and all(not m['matches'] for m in p['messages'] if not m['hit']), p
text=selected[0]['text'].encode('utf-16-le')
assert any(text[2*r['location']:2*(r['location']+r['length'])].decode('utf-16-le')=='PAGE_HIT_10' for r in selected[0]['highlights']), selected
assert p['project']=='demo' and p['cwd'].endswith('/work/demo'), f'missing project/cwd: {p["project"]} {p["cwd"]}'
assert len(selected[0]['text'])<=400 and selected[0]['text'].endswith('…'), 'long selected text was not truncated'
print('5-message context, selected hit, resume command, project/cwd, and 400-cell truncation verified')
PY
if [[ -s "$TMP/show-check.txt" ]] && grep -Eq '^> [0-9]{2}:[0-9]{2} user:' <<<"$show_text" && grep -q 'resume: claude --resume 66666666-6666-4666-8666-666666666666' <<<"$show_text"; then record 'show ref context and text hit marker' PASS 0 "$(<"$TMP/show-check.txt")"; else record 'show ref context and text hit marker' FAIL 0 "$(<"$TMP/show-error.txt") $show_text"; fi
show_all=$("$BIN" show "$show_ref" --all --json 2>&1); printf '%s\n' "$show_all" > "$TMP/show-all-first.json"; : > "$TMP/show-all-markers.txt"; show_pages=0
while [[ -n $show_all ]]; do
 printf '%s\n' "$show_all" | jq -r '.messages[].text' | grep -oE 'PAGE_HIT_[0-9]{2}' >> "$TMP/show-all-markers.txt" || :
 show_cursor=$(printf '%s\n' "$show_all" | jq -r '.next_cursor // empty'); show_pages=$((show_pages+1)); [[ -z $show_cursor || $show_pages -ge 10 ]] && break
 show_all=$("$BIN" show "$show_ref" --all --json --cursor "$show_cursor" 2>&1)
done
if python3 - "$TMP/show-all-markers.txt" <<'PY' > "$TMP/show-all-check.txt" 2> "$TMP/show-all-error.txt"
import sys
found=[x.strip() for x in open(sys.argv[1]) if x.strip()]; expected=[f'PAGE_HIT_{i:02}' for i in range(23)]
assert len(found)==23 and len(set(found))==23 and set(found)==set(expected), f'--all pages skipped/duplicated messages: {len(found)} unique={len(set(found))}'
print('all 23 messages returned once across show --all pages')
PY
then record 'show --all paginates the whole session' PASS "$show_pages" "$(<"$TMP/show-all-check.txt")"; else record 'show --all paginates the whole session' FAIL "$show_pages" "$(<"$TMP/show-all-error.txt")"; fi

# --help is immediate and must not create/open the index or run a search.
export KIOKU_INDEX="$H/index/help-must-not-exist.db"; rm -f "$KIOKU_INDEX"
help_out=$("$BIN" --help 2>&1); help_rc=$?
if ((help_rc==0)) && grep -q '^Usage:' <<<"$help_out" && grep -q 'kioku show <ref|session-id>' <<<"$help_out" && [[ ! -e $KIOKU_INDEX ]]; then record 'kioku --help exits with usage before search/index' PASS 0 ''; else record 'kioku --help exits with usage before search/index' FAIL 0 "$help_out index_exists=$([[ -e $KIOKU_INDEX ]] && echo yes || echo no)"; fi
export KIOKU_INDEX="$H/index/index.db"

# Query correctness cases from SPEC's Expected behavior plus languages/filters.
check_query(){ local q=$1 mode=$2 want=${3:-}; local t=$SECONDS out rc; out=$("$BIN" --json "$q" 2>&1); rc=$?; local ok=1
 if ((rc)); then ok=0; elif [[ $mode == empty ]]; then jq -e '.total==0 and .shown==0 and (.hits|length)==0' <<<"$out" >/dev/null 2>&1 || ok=0; elif ! grep -Fq "$want" <<<"$out"; then ok=0; fi
 if ((ok)); then record "query: $q ($mode)" PASS $((SECONDS-t)) ''; else record "query: $q ($mode)" FAIL $((SECONDS-t)) "expected $mode $want; got: $out"; failbug "Query $q" "HOME=$HOME KIOKU_INDEX=$KIOKU_INDEX ./kioku --json '$q'" "${mode} ${want}" "$out"; fi
}
check_query snapshot contains SNAPSHOT
check_query recoverytoken contains recoverytoken
check_query resum contains resuming
check_query sume empty
check_query '"tea black"' empty
check_query naive contains 'naïve'
for q in json_extract low-water S3 cjk.c; do check_query "$q" contains "$q"; done
check_query 魚池 contains 魚池
check_query 南投魚池 empty
check_query 茶 contains 茶
check_query 日本語 contains 日本語
check_query 한국어 contains 한국어
check_query --flag empty
start=$SECONDS; version=$("$BIN" --version 2>&1); rc=$?; if ((rc==0)) && [[ -n $version ]]; then record 'CLI --version' PASS $((SECONDS-start)) "$version"; else record 'CLI --version' FAIL $((SECONDS-start)) "$version"; fi
start=$SECONDS; recent=$("$BIN" --json 2>&1); rc=$?; if ((rc==0)) && jq -e '.shown>0 and (.hits|length)==.shown and all(.hits[]; has("ref") and has("harness") and has("snippet"))' <<<"$recent" >/dev/null 2>&1; then record 'Empty query lists recent messages as compact JSON' PASS $((SECONDS-start)) "$(jq -r '.shown' <<<"$recent") rows"; else record 'Empty query lists recent messages as compact JSON' FAIL $((SECONDS-start)) "$recent"; fi
start=$SECONDS; malformed=$("$BIN" --json '"' 2>&1); rc=$?; if ((rc==0)) && jq -e '.total==0 and (.hits|length)==0' <<<"$malformed" >/dev/null 2>&1; then record 'Malformed quote query does not crash' PASS $((SECONDS-start)) ''; else record 'Malformed quote query does not crash' FAIL $((SECONDS-start)) "$malformed"; fi

start=$SECONDS; out=$("$BIN" --json snapshot 2>&1); rc=$?; printf '%s\n' "$out" > "$SCREENS/json-snapshot.jsonl"
if ((rc==0)) && jq -e 'has("shown") and has("total") and has("total_sessions") and (.hits|length)==.shown and all(.hits[]; ([keys[]]|sort)==(["ref","harness","project","age","role","snippet","highlights"]|sort))' <<<"$out" >/dev/null; then record 'Compact JSON search-page schema' PASS $((SECONDS-start)) "$(jq -r '.shown' <<<"$out") hits"; else record 'Compact JSON search-page schema' FAIL $((SECONDS-start)) "$out"; fi
for h in claude codex pi; do start=$SECONDS; out=$("$BIN" --json --harness "$h" snapshot 2>&1); rc=$?; if ((rc==0)) && [[ -n $out ]] && ! grep -Ev '"harness":"'"$h"'"' <<<"$out" | grep -q .; then record "JSON harness filter $h" PASS $((SECONDS-start)) ''; else record "JSON harness filter $h" FAIL $((SECONDS-start)) "$out"; fi; done

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
tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '1 messages' <<<"$capture" || grok_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" Enter; sleep .3
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
grep -q '1 messages' <<<"$capture" || opencode_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" Escape; sleep .2; tmux send-keys -t "htool-$$" Enter; sleep .6
expected="opencode|$OPHOME/work/opencode demo|--session oc-v1"
grep -Fq "$expected" "$STUBLOG" 2>/dev/null || opencode_ok=0
[[ $(tmux display-message -p -t "htool-$$" '#{pane_dead} #{pane_dead_status}') == '1 0' ]] || opencode_ok=0
printf '\nResume stub: %s\n' "$expected" >> "$SCREENS/opencode-tui.txt"
tmux kill-session -t "htool-$$" 2>/dev/null || :
if ((opencode_ok)); then record 'OpenCode TUI: harness cycling and resume argv + cwd' PASS $((SECONDS-start)) 'test/screens/opencode-tui.txt'; else record 'OpenCode TUI: harness cycling and resume argv + cwd' FAIL $((SECONDS-start)) "$(<"$SCREENS/opencode-tui.txt")"; fi

# Cursor is the last harness: Tab reaches all; Shift-Tab returns; Enter uses cwd and native ID.
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
grep -q '1 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" Tab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
grep -q '0 messages' <<<"$capture" || cursor_ok=0
tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; tmux send-keys -t "htool-$$" BTab; sleep .3; capture=$(tmux capture-pane -t "htool-$$" -p 2>&1)
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
sleep 1; tmux send-keys -t "hrsm-$$" Escape; sleep .2; tmux send-keys -t "hrsm-$$" Enter; sleep .8
CWD_PHYS=$(cd "$H/work/demo" && pwd -P)
if grep -Fq "claude|$CWD_PHYS|--resume 11111111-1111-4111-8111-111111111111" "$STUBLOG" 2>/dev/null; then record 'Resume Claude stub argv + cwd' PASS 0 "$(tail -1 "$STUBLOG")"; else record 'Resume Claude stub argv + cwd' FAIL 0 "$(cat "$STUBLOG" 2>/dev/null || echo no stub call)"; fi
# Codex and Pi use their harness-specific resume forms too.
for harness in codex pi; do
  session="hrsm-$harness-$$"; export KIOKU_INDEX="$H/index/resume-$harness.db"
  tmux new-session -d -x 100 -y 32 -s "$session" "cd '$ROOT' && HOME='$HOME' PATH='$PATH' STUBLOG='$STUBLOG' KIOKU_INDEX='$KIOKU_INDEX' TERM=xterm-256color exec ./kioku --harness $harness snapshot" 2>/dev/null
  sleep 1; tmux send-keys -t "$session" Escape; sleep .2; tmux send-keys -t "$session" Enter; sleep .6
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
rows=[x.rstrip('\\n') for x in open(sys.argv[1],encoding='utf8') if x.startswith(('  ›✻ ', '   ✻ ', '  ›◇ ', '   ◇ ', '  ›π ', '   π '))]
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

# Large fixture: 2,000 Codex sessions x 100 user/assistant pairs = 200,000 messages.
PERF="$TMP/perfhome"; mkdir -p "$PERF"; export PERF
python3 - <<'PY'
import json,os,pathlib
root=pathlib.Path(os.environ['PERF'])/'.codex/sessions/2026/09/27'; root.mkdir(parents=True)
for i in range(2000):
 p=root/f'rollout-{i:04}.jsonl'; sid=f'{i:032x}'
 with p.open('w') as f:
  f.write(json.dumps({'timestamp':'2026-09-27T10:00:00Z','type':'session_meta','payload':{'id':sid,'cwd':'/work/perf'}})+'\n')
  for j in range(100):
   role='user' if j%2==0 else 'assistant'; typ='input_text' if role=='user' else 'output_text'
   text='perfneedle large fixture message 魚池 紅茶 日本語 한국어' if j==0 else f'neutral synthetic message {j}'
   f.write(json.dumps({'timestamp':'2026-09-27T10:00:01Z','type':'response_item','payload':{'type':'message','role':role,'content':[{'type':typ,'text':text}]}})+'\n')
PY
python3 - <<'PY'
import json,os,pathlib
root=pathlib.Path(os.environ['PERF'])
claude=root/'.claude/projects/newest-first/000-new.jsonl'; claude.parent.mkdir(parents=True)
rows=[{'type':'user','uuid':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1','timestamp':'2026-10-01T10:00:00Z','cwd':'/work/new','sessionId':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','message':{'role':'user','content':'synthetic newest session'}},{'type':'assistant','uuid':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2','timestamp':'2026-10-01T10:00:01Z','cwd':'/work/new','sessionId':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','message':{'role':'assistant','content':[{'type':'text','text':'synthetic NEWEST_MATCH_MARKER'}]}}]
claude.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
pi=root/'.pi/agent/sessions/zz-old/999-old.jsonl'; pi.parent.mkdir(parents=True)
rows=[{'type':'session','version':3,'id':'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','timestamp':'2025-01-01T00:00:00Z','cwd':'/work/old'},{'type':'message','id':'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1','timestamp':'2025-01-01T00:00:01Z','message':{'role':'user','content':[{'type':'text','text':'synthetic OLDEST_MATCH_MARKER'}]}}]
pi.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
export HOME="$PERF" KIOKU_INDEX="$TMP/perf.db"
python3 - "$BIN" "$TMP/perf-metrics.json" <<'PY'
import json,math,os,statistics,subprocess,sys,time
binary,outfile=sys.argv[1:]; env=os.environ.copy(); env['KIOKU_DEBUG_TIMING']='1'
t=time.perf_counter_ns(); index=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True); index_ms=(time.perf_counter_ns()-t)/1e6
queries=['perfneedle','synthetic','neutral','"large fixture"','"synthetic message"','-perfneedle','large','fixture','message','neutral 42','neutral 17','魚池','紅茶','日本語','한국어','perfneed','"neutral synthetic"','-neutral','"fixture message"','absenttoken']
latencies=[]; failures=[]; results=[]; samples=[]
for q in queries:
 t=time.perf_counter_ns(); p=subprocess.run([binary,'--json',q],env=env,text=True,capture_output=True); elapsed=(time.perf_counter_ns()-t)/1e6; latencies.append(elapsed)
 if p.returncode: failures.append({'query':q,'error':p.stderr.strip()})
 results.append(sum(1 for x in p.stdout.splitlines() if x.strip())); samples.append({'query':q,'ms':round(elapsed,2),'timing':p.stderr.strip().replace('\\n','; ')})
sorted_ms=sorted(latencies); p95=sorted_ms[math.ceil(.95*len(sorted_ms))-1]
json.dump({'index_rc':index.returncode,'index_out':index.stdout+index.stderr,'index_ms':round(index_ms,2),'n':len(latencies),'median_ms':round(statistics.median(latencies),2),'p95_ms':round(p95,2),'failures':failures,'results':results,'slowest':sorted(samples,key=lambda x:x['ms'],reverse=True)[:3]},open(outfile,'w'))
PY
metrics=$(<"$TMP/perf-metrics.json"); index_ms=$(jq -r .index_ms <<<"$metrics"); median_ms=$(jq -r .median_ms <<<"$metrics"); p95_ms=$(jq -r .p95_ms <<<"$metrics"); failures=$(jq -r '.failures|length' <<<"$metrics"); index_rc=$(jq -r .index_rc <<<"$metrics"); details="index=${index_ms}ms queries=$(jq -r .n <<<"$metrics") median=${median_ms}ms p95=${p95_ms}ms slowest=$(jq -c .slowest <<<"$metrics")"
if ((index_rc==0 && failures==0)) && grep -q '200000 messages' <<<"$(jq -r .index_out <<<"$metrics")" && awk -v p="$p95_ms" 'BEGIN{exit !(p<=200)}'; then record 'Performance: 2k sessions / 200k messages (20 varied queries)' PASS 0 "$details"; else record 'Performance: 2k sessions / 200k messages (20 varied queries)' FAIL 0 "$details; index_rc=$index_rc query_failures=$failures $(jq -r .index_out <<<"$metrics")"; fi

# Very common term: newest session file is indexed first, oldest matching file last.
start=$SECONDS; semantic=$("$BIN" --json synthetic 2>&1); semantic_rc=$?; printf '%s\n' "$semantic" > "$SCREENS/common-word.jsonl"
if ((semantic_rc==0)) && python3 - "$SCREENS/common-word.jsonl" > "$TMP/semantic-summary.txt" 2> "$TMP/semantic-error.txt" <<'PY'
import json,sys
page=json.load(open(sys.argv[1])); rows=page['hits']; harnesses={r['harness'] for r in rows}
assert 'claude' in harnesses and 'codex' in harnesses, f'only harnesses in common-word results: {sorted(harnesses)}'
assert rows and rows[0]['harness']=='claude' and 'NEWEST_MATCH_MARKER' in rows[0]['snippet'], 'newest Claude match was absent or misranked at the top'
print(f'{len(rows)} rows; harnesses={sorted(harnesses)}; newest={rows[0]["snippet"]}')
PY
then record 'Search semantics: common term is ranked by message time, not insertion order' PASS $((SECONDS-start)) "$(<"$TMP/semantic-summary.txt")"; else record 'Search semantics: common term is ranked by message time, not insertion order' FAIL $((SECONDS-start)) "exit=$semantic_rc error=$(<"$TMP/semantic-error.txt") output=$(tail -3 "$SCREENS/common-word.jsonl")"; fi

# Appends (including an incomplete final record) must be ingested without rebuilding the corpus.
append_file="$PERF/.codex/sessions/2026/09/27/rollout-0000.jsonl"
python3 - "$append_file" "$TMP/partial-rest" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); full=json.dumps({'timestamp':'2026-09-27T12:00:00Z','type':'response_item','payload':{'type':'message','role':'user','content':[{'type':'input_text','text':'appendtoken now searchable'}]}}).encode()+b'\n'
with p.open('ab') as f: f.write(full)
partial=json.dumps({'timestamp':'2026-09-27T12:01:00Z','type':'response_item','payload':{'type':'message','role':'user','content':[{'type':'input_text','text':'partialtoken completed later'}]}}).encode()
with p.open('ab') as f: f.write(partial[:-1])
pathlib.Path(sys.argv[2]).write_bytes(partial[-1:]+b'\n')
PY
start=$SECONDS; appended=$(KIOKU_DEBUG_TIMING=1 "$BIN" --json appendtoken 2>"$TMP/append-timing.txt"); append_rc=$?; append_ms=$(sed -nE 's/^timing sync_changes=([0-9.]+)ms$/\1/p' "$TMP/append-timing.txt"); after_append=$("$BIN" index 2>&1)
if ((append_rc==0)) && grep -q appendtoken <<<"$appended" && grep -q 'codex: 200001 messages' <<<"$after_append" && grep -q '0 changed' <<<"$after_append" && [[ -n $append_ms ]] && awk -v x="$append_ms" 'BEGIN{exit !(x<200)}'; then record 'Append sync: next CLI query finds new line; only tail parsed' PASS $((SECONDS-start)) "sync_changes=${append_ms}ms; $after_append"; else record 'Append sync: next CLI query finds new line; only tail parsed' FAIL $((SECONDS-start)) "query=$appended timing=$(cat "$TMP/append-timing.txt") index=$after_append"; fi
partial=$(KIOKU_DEBUG_TIMING=1 "$BIN" --json partialtoken 2>"$TMP/partial-timing.txt"); partial_rc=$?; if ((partial_rc==0)) && jq -e '.total==0 and (.hits|length)==0' <<<"$partial" >/dev/null 2>&1; then record 'Partial final line is withheld until newline' PASS 0 "$(cat "$TMP/partial-timing.txt")"; else record 'Partial final line is withheld until newline' FAIL 0 "unexpected query output: $partial"; fi
cat "$TMP/partial-rest" >> "$append_file"; completed=$("$BIN" --json partialtoken 2>&1); complete_index=$("$BIN" index 2>&1)
if grep -q partialtoken <<<"$completed" && grep -q 'codex: 200002 messages' <<<"$complete_index" && grep -q '0 changed' <<<"$complete_index"; then record 'Partial line is re-read and indexed when completed' PASS 0 "$complete_index"; else record 'Partial line is re-read and indexed when completed' FAIL 0 "query=$completed index=$complete_index"; fi

# Independent read-only real-store count versus app indexing with an isolated DB.
if [[ ${KIOKU_E2E_REAL:-0} == 1 ]]; then
REALHOME="$REALHOME_DEFAULT"; REALTMP="$TMP/real"; mkdir -p "$REALTMP" "$TEST/out"; export REALHOME REALTMP
python3 - <<'PY' > "$REALTMP/independent.txt"
import glob,json,os
home=os.environ['REALHOME']
patterns={'claude':home+'/.claude/projects/**/*.jsonl','codex':home+'/.codex/sessions/**/*.jsonl','pi':home+'/.pi/agent/sessions/**/*.jsonl'}
for h,pat in patterns.items():
 files=glob.glob(pat,recursive=True); n=0
 for p in files:
  try:
   for line in open(p,encoding='utf8'):
    try:d=json.loads(line)
    except:continue
    m=d.get('message') or {}; payload=d.get('payload') or {}
    if h=='claude' and d.get('type') in ('user','assistant') and not d.get('isMeta') and not d.get('isSidechain'):
     c=m.get('content',''); n+=bool(c) if isinstance(c,str) else any(x.get('type')=='text' and x.get('text') for x in c if isinstance(x,dict))
    elif h=='codex' and d.get('type')=='response_item' and payload.get('type')=='message' and payload.get('role') in ('user','assistant'):
     n+=sum(bool(x.get('text')) for x in payload.get('content',[]) if isinstance(x,dict) and x.get('type') in ('input_text','output_text'))
    elif h=='pi' and d.get('type')=='message' and m.get('role') in ('user','assistant'):
     c=m.get('content',[]); n+=bool(c) if isinstance(c,str) else sum(bool(x.get('text')) for x in c if isinstance(x,dict) and x.get('type')=='text')
  except (OSError,UnicodeError): pass
 print(h,len(files),n)
PY
export HOME="$REALHOME" KIOKU_INDEX="$REALTMP/index.db"
start=$SECONDS; realout=$("$BIN" index --rebuild 2>&1); rc=$?; python3 - "$REALTMP/independent.txt" "$REALTMP/app.txt" <<'PY'
import sqlite3,sys
rows=sqlite3.connect(sys.argv[2].replace('app.txt','index.db')).execute('select harness,count(distinct uid),sum((select count(*) from messages m where m.session_uid=s.uid)) from sessions s group by harness').fetchall()
open(sys.argv[2],'w').write('\n'.join('%s %s %s'%r for r in rows)+'\n')
PY
{ printf 'independent: harness total_jsonl raw_user_assistant_text_blocks\n'; cat "$REALTMP/independent.txt"; printf 'indexed: harness sessions message_rows\n'; cat "$REALTMP/app.txt"; } > "$TEST/out/real-store-counts.txt"
# Raw user/assistant text blocks are a conservative lower bound: indexed rows also include tools.
if ((rc==0)) && python3 - "$REALTMP/independent.txt" "$REALTMP/app.txt" <<'PY'
import sys
ind={x.split()[0]:int(x.split()[2]) for x in open(sys.argv[1])}; app={x.split()[0]:int(x.split()[2]) for x in open(sys.argv[2])}
for h,n in ind.items():
 a=app.get(h,0)
 if n and a<n: print(f'{h}: indexed={a} below independent user/assistant lines={n}'); sys.exit(1)
 if n and not a: print(f'{h}: independent={n}, indexed=0'); sys.exit(1)
PY
then record 'Real stores: read-only independent lower-bound sanity' PASS $((SECONDS-start)) "$(tr '\n' ';' < "$TEST/out/real-store-counts.txt"); lower-bound only: indexed counts include tool rows and split content blocks"; else record 'Real stores: read-only independent lower-bound sanity' FAIL $((SECONDS-start)) "$realout; $(tr '\n' ';' < "$TEST/out/real-store-counts.txt")"; failbug 'Real store message undercount' 'KIOKU_INDEX=<temp>/index.db ./kioku index --rebuild; compare to test/out/real-store-counts.txt' 'indexed rows >= independent user/assistant text blocks' "$realout; see test/out/real-store-counts.txt"; fi
else
  skip 'Real stores: read-only independent lower-bound sanity' 'Set KIOKU_E2E_REAL=1 to opt in to reading this machine’s ~/.claude, ~/.codex, and ~/.pi stores.'
fi

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

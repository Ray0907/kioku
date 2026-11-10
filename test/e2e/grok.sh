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


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


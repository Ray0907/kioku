# Agy failure cases: bad wire data, wrong roles/order, child/empty sessions, stale
# metadata signals, live WAL data, failed scans, deleted sessions, and bad resume args.
start=$SECONDS
if python3 - "$BIN" "$H/agy-home" <<'PY' > "$SCREENS/agy.txt" 2>&1
import hashlib,json,os,pathlib,shlex,sqlite3,subprocess,sys
binary,home=sys.argv[1:]; home=pathlib.Path(home)
root=home/'.gemini/antigravity-cli'
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'))
def run(*args):
 p=subprocess.run([binary,*args],env=env,text=True,capture_output=True)
 assert p.returncode==0,p.stderr
 return p.stdout
def page(*args): return json.loads(run(args[0],'--json',*args[1:]) if args[0]=='show' else run('--json',*args))
def fingerprints(): return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob('*') if p.is_file() and not p.name.endswith('-shm') and (not p.name.endswith('-wal') or p.stat().st_size>0)}
before=fingerprints()
first=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True)
assert first.returncode==0 and 'agy: 5 messages' in first.stdout,first
assert '3 skipped' in first.stdout and 'agy-bad-workspace' in first.stderr and 'workspace_uris' in first.stderr,first
assert '0 changed' in run('index')
for query in ['agyteatoken','茶葉','agyanswer','agyresult','agytailtoken']:
 hit=page('--harness','agy',query)
 assert hit['total']==1 and hit['hits'][0]['harness']=='agy',hit
 assert page('--harness','cursor',query)['total']==0
for query in ['agyhiddeninjection','agybadtruncated','agyunknownstep','agychildtoken','agybadworkspacetoken']:
 assert page('--harness','agy',query)['total']==0,query
sessions=page('--sessions','--harness','agy')
assert sessions['total']==1 and 'subagent-demo' not in json.dumps(sessions),sessions
ref=sessions['sessions'][0]['best_ref']
shown=page('show',ref,'--all','--limit','20')
assert shown['cwd']=='/work/agy demo' and shown['project']=='agy demo',shown
assert shown['resume_cmd']=='agy --conversation agy-demo',shown
assert shown['resume_argv']==shlex.split(shown['resume_cmd']),shown
assert [m['role'] for m in shown['messages']]==['user','asst','tool','tool','user'],shown
assert shown['messages'][2]['text']=='bash · cat tea.txt',shown
assert shown['messages'][3]['text']=='cat tea.txt · agyresult: tea found',shown
requests=[{'jsonrpc':'2.0','id':1,'method':'tools/list'}]
for i,(name,args) in enumerate([('kioku_search',{'query':'agyteatoken','harness':'agy'}),('kioku_sessions',{'query':'agyteatoken','harness':'agy'}),('kioku_show',{'ref':ref,'all':True})],2):
 requests.append({'jsonrpc':'2.0','id':i,'method':'tools/call','params':{'name':name,'arguments':args}})
mcp=subprocess.run([binary,'mcp'],input=''.join(json.dumps(r)+'\n' for r in requests),env=env,text=True,capture_output=True,check=True)
replies=[json.loads(line)['result'] for line in mcp.stdout.splitlines()]
for tool in replies[0]['tools']:
 if tool['name'] in ['kioku_search','kioku_sessions']: assert 'agy' in tool['inputSchema']['properties']['harness']['enum']
for result in replies[1:]: assert not result.get('isError'),result
assert json.loads(replies[1]['content'][0]['text'])['total']==1
assert json.loads(replies[2]['content'][0]['text'])['total']==1
assert json.loads(replies[3]['content'][0]['text'])['resume_cmd']==shown['resume_cmd']
with sqlite3.connect(env['KIOKU_INDEX']) as db:
 assert db.execute('SELECT ts FROM messages ORDER BY idx LIMIT 1').fetchone()[0]=='2026-09-27T10:00:00.123Z'
assert fingerprints()==before
checks=json.loads(run('doctor','--json'))
assert next(c for c in checks if c['name']=='Antigravity CLI')['ok'],checks
assert fingerprints()==before
# The writer stays open; neither DB is checkpointed before the next sync.
def varint(n):
 out=bytearray()
 while n>127: out.append((n&127)|128); n>>=7
 return bytes(out)+bytes([n])
def blob(k,b):
 if isinstance(b,str): b=b.encode()
 return varint(k<<3|2)+varint(len(b))+b
with sqlite3.connect(root/'conversations/agy-demo.db') as conv, sqlite3.connect(root/'conversation_summaries.db') as meta:
 for db in [conv,meta]: db.execute('PRAGMA wal_autocheckpoint=0')
 before_db=(root/'conversations/agy-demo.db').read_bytes()
 before_meta=(root/'conversation_summaries.db').read_bytes()
 conv.execute('INSERT INTO steps VALUES(100,14,?)',(blob(19,blob(2,'agywaltoken')),)); conv.commit()
 meta.execute("UPDATE conversation_summaries SET step_count=step_count+1 WHERE conversation_id='agy-demo'"); meta.commit()
 assert (root/'conversations/agy-demo.db').read_bytes()==before_db
 assert (root/'conversation_summaries.db').read_bytes()==before_meta
 assert (root/'conversations/agy-demo.db-wal').stat().st_size>0
 wal_before=fingerprints()
 assert page('--harness','agy','agywaltoken')['total']==1
 assert '0 changed' in run('index')
 assert fingerprints()==wal_before
 conv.execute('UPDATE steps SET step_payload=? WHERE idx=100',(blob(19,blob(2,'agyedittoken')),)); conv.commit()
 meta.execute("UPDATE conversation_summaries SET last_modified_time='2026-09-27 10:01:00+00:00' WHERE conversation_id='agy-demo'"); meta.commit()
 assert page('--harness','agy','agyedittoken')['total']==1
 assert page('--harness','agy','agywaltoken')['total']==0
# Bad workspace data must report one session error and keep its cached rows.
meta=root/'conversation_summaries.db'
invalid=['bad JSON','[]','{}','[null]','["https://example.invalid/work"]','["file:relative"]','["file://remote/work"]','["file://"]','["file:///work/%00"]','["file:///work?query"]']
for workspaces in invalid:
 with sqlite3.connect(meta) as db:
  db.execute("UPDATE conversation_summaries SET workspace_uris=? WHERE conversation_id='agy-demo'",(workspaces,))
 failed=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True)
 assert failed.returncode==0 and 'agy: 6 messages' in failed.stdout,failed
 assert '4 skipped' in failed.stdout and 'agy-demo.db' in failed.stderr and 'workspace_uris' in failed.stderr,failed
 cached=page('show',ref,'--all','--limit','20')
 assert cached['cwd']=='/work/agy demo' and len(cached['messages'])==6,cached
with sqlite3.connect(meta) as db:
 db.execute("UPDATE conversation_summaries SET workspace_uris=? WHERE conversation_id='agy-demo'",(json.dumps(['file:///work/agy%20demo']),))
assert page('--harness','agy','agyedittoken')['total']==1
# A failed metadata read must not remove cached sessions, even on rebuild.
meta=root/'conversation_summaries.db'; saved=meta.read_bytes(); meta.write_bytes(b'bad database')
assert 'agy: 6 messages' in run('index','--rebuild')
meta.write_bytes(saved)
assert page('--harness','agy','agyedittoken')['total']==1
# Both explicit paths and ~/ overrides use the same store. Missing overrides do not fall back.
env['KIOKU_AGY_DIR']='~/.gemini/antigravity-cli'
assert page('--sessions','--harness','agy')['total']==1
env['KIOKU_AGY_DIR']=str(root)
assert page('--sessions','--harness','agy')['total']==1
env['KIOKU_AGY_DIR']=str(home/'absent')
assert page('--sessions','--harness','agy')['total']==0
env['KIOKU_AGY_DIR']=str(root)
assert page('--sessions','--harness','agy')['total']==1
# Enter must run the resume command in the session directory.
import time
cwd=home/'work/agy demo'; cwd.mkdir(parents=True)
with sqlite3.connect(meta) as db:
 db.execute("UPDATE conversation_summaries SET workspace_uris=?,last_modified_time='2026-09-27 10:02:00+00:00' WHERE conversation_id='agy-demo'",(json.dumps([cwd.as_uri()]),))
run('index')
stub=home/'bin'; stub.mkdir()
marker=home/'resume.json'
(stub/'agy').write_text('#!/usr/bin/env python3\nimport json,os,pathlib,sys\npathlib.Path('+repr(str(marker))+').write_text(json.dumps({"argv":sys.argv[1:],"cwd":os.getcwd()}))\n')
(stub/'agy').chmod(0o755)
name='kioku-agy-'+str(os.getpid())
command=shlex.join(['env','HOME='+str(home),'KIOKU_INDEX='+env['KIOKU_INDEX'],'KIOKU_AGY_DIR='+str(root),'PATH='+str(stub)+':'+env['PATH'],binary,'--harness','agy','agyteatoken'])
try:
 subprocess.run(['tmux','new-session','-d','-s',name,'-x','100','-y','32',command],check=True)
 for _ in range(100):
  screen=subprocess.check_output(['tmux','capture-pane','-p','-t',name],text=True)
  if 'agyteatoken' in screen: break
  time.sleep(.1)
 else: raise AssertionError('Agy TUI did not load')
 subprocess.run(['tmux','send-keys','-t',name,'Enter'],check=True)
 for _ in range(100):
  if marker.exists(): break
  time.sleep(.1)
 assert json.loads(marker.read_text())=={'argv':['--conversation','agy-demo'],'cwd':str(cwd.resolve())},marker.read_text()
finally:
 subprocess.run(['tmux','kill-session','-t',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
with sqlite3.connect(meta) as db: db.execute("DELETE FROM conversation_summaries WHERE conversation_id='agy-demo'")
assert page('--sessions','--harness','agy')['total']==0
checks=json.loads(run('doctor','--json'))
agy_check=next(c for c in checks if c['name']=='Antigravity CLI')
assert agy_check['ok'] and '3 skipped:' in agy_check['detail'] and 'agy-bad-workspace.db' in agy_check['detail'] and 'workspace_uris' in agy_check['detail'],agy_check
print('PASS: Agy search/CJK, roles/order, MCP, resume command and cwd, wire recovery, bad workspace rows and cached data, no-op, WAL signals, read-only stores, doctor, failed scan retention, overrides, and deletion.')
PY
then record 'Agy: protobuf sessions, WAL sync, resume, doctor' PASS $((SECONDS-start)) "$(<"$SCREENS/agy.txt")"; else record 'Agy: protobuf sessions, WAL sync, resume, doctor' FAIL $((SECONDS-start)) "$(<"$SCREENS/agy.txt")"; fi

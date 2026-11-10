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


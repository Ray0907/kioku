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


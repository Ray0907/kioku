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


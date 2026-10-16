#!/bin/sh
set -eu

if [ "$#" -gt 1 ]; then
  echo "usage: $0 [new-synthetic-HOME]" >&2
  exit 2
fi
if [ "$#" -eq 1 ]; then
  home=$1
  if [ -e "$home" ]; then
    echo "refusing to overwrite existing path: $home" >&2
    exit 1
  fi
  mkdir -p "$home"
else
  home=$(mktemp -d "${TMPDIR:-/tmp}/kioku-fixture.XXXXXX")
fi

# All paths and transcript text below are synthetic; this script never reads the real HOME.
claude="$home/.claude/projects/-work-demo"
codex="$home/.codex/sessions/2026/09/27"
pi="$home/.pi/agent/sessions/-work-demo"
mkdir -p "$claude" "$codex" "$pi"

cat > "$claude/11111111-1111-4111-8111-111111111111.jsonl" <<'JSONL'
{"parentUuid":null,"isSidechain":false,"promptId":"fixture-prompt-1","type":"user","message":{"role":"user","content":"Find the snapshot and resume path; resuming should preserve JSON output."},"uuid":"11111111-1111-4111-8111-111111111101","timestamp":"2026-09-27T10:00:00.000Z","cwd":"/work/demo","sessionId":"11111111-1111-4111-8111-111111111111","version":"1.0.0"}
{"parentUuid":"11111111-1111-4111-8111-111111111101","isSidechain":false,"type":"assistant","message":{"model":"fixture-model","role":"assistant","content":[{"type":"text","text":"SNAPSHOT: json_extract handles low-water marks, S3 uploads, cjk.c, and naïve input."}]},"uuid":"11111111-1111-4111-8111-111111111102","timestamp":"2026-09-27T10:00:03.000Z","session_id":"11111111-1111-4111-8111-111111111111","cwd":"/work/demo","sessionId":"11111111-1111-4111-8111-111111111111"}
{"parentUuid":"11111111-1111-4111-8111-111111111102","isSidechain":false,"type":"user","message":{"role":"user","content":"紅茶: 日月潭紅茶產於南投縣魚池鄉。魚池と紅茶を探す。한국어 검색도 확인합니다."},"uuid":"11111111-1111-4111-8111-111111111103","timestamp":"2026-09-27T10:01:00.000Z","cwd":"/work/demo","sessionId":"11111111-1111-4111-8111-111111111111"}
{"parentUuid":"11111111-1111-4111-8111-111111111103","isSidechain":false,"type":"assistant","message":{"model":"fixture-model","role":"assistant","content":[{"type":"text","text":"茶 is searchable; 日本語の議事録と 한국어 세션도 이 fixture에 있습니다."}]},"uuid":"11111111-1111-4111-8111-111111111104","timestamp":"2026-09-27T10:01:05.000Z","cwd":"/work/demo","sessionId":"11111111-1111-4111-8111-111111111111"}
{"broken-json":
{"parentUuid":"11111111-1111-4111-8111-111111111104","isSidechain":false,"type":"user","message":{"role":"user","content":"Parser continues after malformed line: recoverytoken."},"uuid":"11111111-1111-4111-8111-111111111105","timestamp":"2026-09-27T10:02:00.000Z","cwd":"/work/demo","sessionId":"11111111-1111-4111-8111-111111111111"}
JSONL

cat > "$codex/rollout-2026-09-27T10-05-00-019d0000-0000-7000-8000-000000000002.jsonl" <<'JSONL'
{"timestamp":"2026-09-27T10:05:00.000Z","type":"session_meta","payload":{"id":"22222222-2222-4222-8222-222222222222","timestamp":"2026-09-27T10:05:00.000Z","cwd":"/work/demo","originator":"codex","cli_version":"fixture","source":"cli","model_provider":"fixture"}}
{"timestamp":"2026-09-27T10:05:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Search for snapshot, resum, json_extract, low-water, S3, and cjk.c."}]}}
{"timestamp":"2026-09-27T10:05:04.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SNAPSHOT is ready; resuming is safe, but a shorter prefix is not a match. The phrase is black tea."}]}}
{"timestamp":"2026-09-27T10:06:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"魚池 appears inside 日月潭紅茶產於南投縣魚池鄉. 日本語の検索と 한국어 검색."}]}}
{"timestamp":"2026-09-27T10:06:03.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"紅茶 is tea; single-character 茶 should match. naïve accents are searchable."}]}}
not-json
{"timestamp":"2026-09-27T10:07:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Parser continues after malformed line: recoverytoken."}]}}
JSONL

cat > "$pi/33333333-3333-4333-8333-333333333333.jsonl" <<'JSONL'
{"type":"session","version":3,"id":"33333333-3333-4333-8333-333333333333","timestamp":"2026-09-27T10:10:00.000Z","cwd":"/work/demo"}
{"type":"message","id":"33333333-3333-4333-8333-333333333301","parentId":null,"timestamp":"2026-09-27T10:10:02.000Z","message":{"role":"user","content":[{"type":"text","text":"Find SNAPSHOT and resuming; test S3, json_extract, low-water, and cjk.c."}],"timestamp":1790503802000}}
{"type":"message","id":"33333333-3333-4333-8333-333333333302","parentId":"33333333-3333-4333-8333-333333333301","timestamp":"2026-09-27T10:10:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"A snapshot can resume safely; the phrase is black tea, and naïve input is searchable."}],"timestamp":1790503805000,"provider":"fixture","model":"fixture-model","stopReason":"stop"}}
{"type":"message","id":"33333333-3333-4333-8333-333333333303","parentId":"33333333-3333-4333-8333-333333333302","timestamp":"2026-09-27T10:11:00.000Z","message":{"role":"user","content":[{"type":"text","text":"魚池 in 日月潭紅茶產於南投縣魚池鄉. 日本語の会話です。한국어 대화입니다."}],"timestamp":1790503860000}}
{"type":"message","id":"33333333-3333-4333-8333-333333333304","parentId":"33333333-3333-4333-8333-333333333303","timestamp":"2026-09-27T10:11:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"茶 is one character; 红茶 and 紅茶 are both represented."}],"timestamp":1790503863000,"provider":"fixture","model":"fixture-model","stopReason":"stop"}}
{"broken-json":
{"type":"message","id":"33333333-3333-4333-8333-333333333305","parentId":"33333333-3333-4333-8333-333333333304","timestamp":"2026-09-27T10:12:00.000Z","message":{"role":"user","content":[{"type":"text","text":"Parser continues after malformed line: recoverytoken."}],"timestamp":1790503920000}}
JSONL

# Separate synthetic HOME keeps Grok coverage independent of baseline counts.
grok="$home/grok-home/.grok/sessions/%2Fwork%2Fgrok%20fallback/55555555-5555-4555-8555-555555555555"
mkdir -p "$grok"
cat > "$grok/summary.json" <<'JSON'
{"info":{"id":"grok-native-id","cwd":"/work/grok demo"},"created_at":"2026-09-27T10:15:00Z","current_model_id":"grok-summary-model"}
JSON
cat > "$grok/chat_history.jsonl" <<'JSONL'
{"type":"user","content":"grokteatoken: 日月潭紅茶產於南投縣魚池鄉。日本語と 한국어 대화."}
{"type":"assistant","model_id":"grok-message-model","content":[{"type":"text","text":"grokanswer: checking the tea notes."}],"tool_calls":[{"id":"tea-call","name":"read_file","arguments":"{\"path\":\"groktea.txt\"}"}]}
{"type":"tool_result","tool_call_id":"tea-call","content":[{"type":"text","text":"groktearesult: notes found.\nSecond line is compacted."}]}
{"broken-json":
{"type":"user","content":[{"type":"text","text":"grokrecovery: continued after malformed line."}]}
{"type":"assistant","tool_calls":[{"id":"pending","name":"bash","input":{"command":"echo grokappend"}}]}
JSONL

# Separate synthetic HOME keeps self-lookup coverage independent of baseline counts.
python3 - "$home/self-lookups" <<'PY'
import json,pathlib,sys
home=pathlib.Path(sys.argv[1])
commands=['kioku --sessions secretword', ' TAG="two words" command kioku --json secretword', 'command TAG=yes kioku secretword']
for harness,store in [('claude','.claude/projects/self'),('codex','.codex/sessions'),('pi','.pi/agent/sessions/self')]:
 rows=[]
 def emit(role,text='',calls=(),call_id='',name='bash'):
  ts='2026-09-27T11:00:00Z'
  if harness=='claude':
   content=[{'type':'text','text':text}] if text else []
   content += [{'type':'tool_use','id':i,'name':n,'input':{'command':cmd}} for i,n,cmd in calls]
   if role=='tool': content=[{'type':'tool_result','tool_use_id':call_id,'content':text}]
   rows.append({'type':'user' if role in ('user','tool') else 'assistant','timestamp':ts,'cwd':'/work/self-lookups','sessionId':'self-claude','message':{'role':role,'content':content}})
  elif harness=='codex':
   if text and role!='tool': rows.append({'type':'response_item','timestamp':ts,'payload':{'type':'message','role':role,'content':[{'type':'input_text' if role=='user' else 'output_text','text':text}]}})
   for i,n,cmd in calls:
    payload={'type':'custom_tool_call','input':cmd} if i=='self-2' else {'type':'function_call','arguments':json.dumps({'cmd':cmd})}
    rows.append({'type':'response_item','timestamp':ts,'payload':dict(payload,call_id=i,name=n)})
   if role=='tool': rows.append({'type':'response_item','timestamp':ts,'payload':{'type':'custom_tool_call_output' if call_id=='self-2' else 'function_call_output','call_id':call_id,'output':text}})
  else:
   content=[{'type':'text','text':text}] if text else []
   content += [{'type':'toolCall','id':i,'name':n,'arguments':{'command':cmd}} for i,n,cmd in calls]
   rows.append({'type':'message','timestamp':ts,'message':{'role':'toolResult' if role=='tool' else role,'content':content,'toolCallId':call_id,'toolName':name}})
 if harness=='codex': rows.append({'type':'session_meta','payload':{'id':'self-codex','cwd':'/work/self-lookups'}})
 if harness=='pi': rows.append({'type':'session','id':'self-pi','cwd':'/work/self-lookups'})
 emit('assistant','Searching archive.',calls=[('self-0','bash',commands[0]),('ordinary','bash','echo ordinaryprobe')])
 emit('tool','ordinaryprobe output',call_id='ordinary')
 emit('tool','secretword lookup result',call_id='self-0')
 for i,cmd in enumerate(commands[1:],1):
  emit('assistant',calls=[(f'self-{i}','bash',cmd)])
  emit('tool','secretword lookup result',call_id='' if harness=='pi' else f'self-{i}')
 # Leave an unresolved call for the E2E to append its result after indexing.
 emit('assistant',calls=[('pending','bash','kioku pendingsecret')])
 path=home/store/'self.jsonl'; path.parent.mkdir(parents=True,exist_ok=True)
 path.write_text(''.join(json.dumps(r)+'\n' for r in rows))
normal=home/'.claude/projects/normal/normal.jsonl'; normal.parent.mkdir(parents=True,exist_ok=True)
rows=[{'type':role,'timestamp':'2026-09-27T11:01:00Z','cwd':'/work/normal','sessionId':'normal','message':{'role':role,'content':text}} for role,text in [('user','Please remember secretword.'),('assistant','kioku merely mentions secretword.')]]
rows[-1]['message']['content']=[{'type':'text','text':rows[-1]['message']['content']}]
for i,cmd in enumerate(['echo kioku mentionprobe','kioku-other mentionprobe','FOO=kioku echo mentionprobe']):
 rows.append({'type':'assistant','message':{'content':[{'type':'tool_use','id':str(i),'name':'bash','input':{'command':cmd}}]}})
 rows.append({'type':'user','message':{'content':[{'type':'tool_result','tool_use_id':str(i),'content':'mentionprobe result'}]}})
normal.write_text(''.join(json.dumps(r)+'\n' for r in rows))
PY

# Separate synthetic HOME keeps OpenCode fixtures out of the other harness counts.
opencode="$home/opencode-home/.local/share/opencode"
mkdir -p "$opencode"
sqlite3 "$opencode/opencode.db" <<'SQL'
CREATE TABLE session(id TEXT PRIMARY KEY, project_id TEXT, parent_id TEXT, directory TEXT, title TEXT, time_created INTEGER, time_updated INTEGER);
CREATE TABLE message(id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);
CREATE INDEX message_session ON message(session_id, time_created);
CREATE TABLE part(id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, data TEXT);
CREATE INDEX part_message ON part(message_id, id);
INSERT INTO session VALUES('oc-v1','p',NULL,'/work/opencode demo','v1',1790503200000,1790503202000),('oc-child','p','oc-v1','/work/opencode demo','child',1790503200000,1790503200000),('oc-both','p',NULL,'/work/opencode demo','old',1790503200000,1790503200000);
INSERT INTO message VALUES('m2','oc-v1',1790503201000,'{"role":"assistant","modelID":"oc-model","time":{"created":1790503201000}}'),('m1','oc-v1',1790503200000,'{"role":"user","time":{"created":1790503200000}}'),('m3','oc-v1',1790503201000,'{"role":"assistant","time":{"created":1790503201000}}'),('child','oc-child',1790503200000,'{"role":"user"}'),('old','oc-both',1790503200000,'{"role":"user"}');
INSERT INTO part VALUES('p1','m1','oc-v1','{"type":"text","text":"opencodevone 魚池"}'),('p4','m2','oc-v1','{"type":"tool","tool":"read","state":{"input":{"path":"ocnotes.txt"},"output":"ignored"}}'),('p2','m2','oc-v1','{"type":"reasoning","text":"ocreasoninghidden"}'),('p3','m2','oc-v1','{"type":"text","text":"ocanswer"}'),('p5','m2','oc-v1','{"type":"tool","tool":"bash","state":{"input":{"command":"printf octooltoken\nsecond line"}}}'),('p6','m2','oc-v1','{"type":"tool","tool":"read","state":{"output":"ocoutputtoken\nsecond line"}}'),('p7','m3','oc-v1','{"type":"text","text":"octietoken"}'),('child','child','oc-child','{"type":"text","text":"ocsubagenthidden"}'),('old','old','oc-both','{"type":"text","text":"ocv1duplicatehidden"}');
CREATE TABLE session_v2(id TEXT PRIMARY KEY, project_id TEXT, parent_id TEXT, directory TEXT, title TEXT, time_created INTEGER, time_updated INTEGER);
CREATE TABLE session_message(id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT);
CREATE INDEX session_message_session ON session_message(session_id, time_created, time_updated);
INSERT INTO session_v2 VALUES('oc-both','p',NULL,'/work/opencode v2','new',1790503200000,1790503202000),('oc-v2child','p','oc-both','/work/opencode v2','child',1790503200000,1790503200000);
INSERT INTO session_message VALUES('z','oc-both','user',1,1790503200000,1790503200000,'{"text":"opencodevtwo"}'),('a','oc-both','assistant',2,1790503201000,1790503201000,'{"text":"ocv2answer"}'),('t','oc-both','tool-result',3,1790503202000,1790503202000,'{"text":"ocv2tool"}'),('r','oc-both','reasoning',4,1790503202000,1790503202000,'{"text":"ocv2reasonhidden"}'),('c','oc-v2child','user',1,1790503200000,1790503200000,'{"text":"ocv2childhidden"}');
SQL
# v2-only and minimal v1 schemas exercise independent layout detection.
cp "$opencode/opencode.db" "$opencode/v2.db"
sqlite3 "$opencode/v2.db" 'DROP TABLE session; DROP TABLE message; DROP TABLE part;'
cp "$opencode/opencode.db" "$opencode/minimal-v1.db"
sqlite3 "$opencode/minimal-v1.db" 'DROP TABLE session_v2; DROP TABLE session_message; ALTER TABLE message RENAME TO old_message; CREATE TABLE message AS SELECT id,session_id,data FROM old_message; DROP TABLE old_message;'

# Cursor CLI graph: only JSON message blobs are indexable; rowid, not id, is the order.
cursor="$home/cursor-home/.cursor/chats/d41d8cd98f00b204e9800998ecf8427e/66666666-6666-4666-8666-666666666666"
mkdir -p "$cursor" "${cursor%/*}/no-cwd"
cat > "$cursor/meta.json" <<'JSON'
{"cwd":"/work/cursor demo","title":"Cursor fixture","createdAtMs":1790503200000}
JSON
printf '%s\n' '{"title":"No cwd","createdAtMs":1790503200000}' > "${cursor%/*}/no-cwd/meta.json"
sqlite3 "$cursor/store.db" <<'SQL'
CREATE TABLE blobs(id TEXT PRIMARY KEY, data BLOB);
INSERT INTO blobs VALUES('z',CAST('  {"role":"user","content":"cursorteatoken 魚池 日本語 한국어"}  ' AS BLOB));
INSERT INTO blobs VALUES('binary',X'00017B22637572736F7262696E61727968696464656E22FF');
INSERT INTO blobs VALUES('a',CAST('{"role":"assistant","content":["cursoranswer","",{"text":"second line"},{"type":"image","url":"cursorimagehidden"}]}' AS BLOB));
INSERT INTO blobs VALUES('state',CAST('{"content":"cursorstatehidden","nodes":[]}' AS BLOB));
INSERT INTO blobs VALUES('system',CAST('{"role":"system","content":"cursorsystemhidden"}' AS BLOB));
INSERT INTO blobs VALUES('malformed',CAST('{"role":"user","content":"cursormalformedhidden"' AS BLOB));
INSERT INTO blobs VALUES('m',CAST('{"role":"user","content":[{"text":"cursorlasttoken"},"final line"]}' AS BLOB));
SQL
cp "$cursor/store.db" "${cursor%/*}/no-cwd/store.db"
printf '%s\n' 'IDE store is out of scope' > "$home/cursor-home/.cursor/state.vscdb"

# Antigravity CLI uses one protobuf step store per conversation.
mkdir -p "$home/agy-home/.gemini/antigravity-cli/conversations"
python3 - "$home/agy-home/.gemini/antigravity-cli" <<'PY'
import json,pathlib,sqlite3,sys
root=pathlib.Path(sys.argv[1])
def varint(n):
 out=bytearray()
 while n>127: out.append((n&127)|128); n>>=7
 return bytes(out)+bytes([n])
def num(k,n): return varint(k<<3)+varint(n)
def blob(k,b):
 if isinstance(b,str): b=b.encode()
 return varint(k<<3|2)+varint(len(b))+b
def stamp(): return blob(5,blob(1,num(1,1790503200)+num(2,123000000)))
def user(s): return stamp()+blob(19,blob(2,s))
steps=[
 (14,user('agyteatoken: 查詢茶葉資料')),
 (101,user('agyhiddeninjection')),
 (15,stamp()+blob(20,blob(1,'agyanswer: tea notes'))),
 (15,stamp()+blob(20,blob(1,'')+blob(7,blob(1,'call-demo')+blob(2,'bash')+blob(3,'{"command":"cat tea.txt"}')))),
 (132,stamp()+blob(140,blob(1,blob(2,'cat tea.txt'))+blob(2,blob(1,'agyresult: tea found')))),
 (14,user('agytailtoken')+num(90,3)+varint(91<<3|1)+bytes(8)+varint(92<<3|5)+bytes(4)),
 (14,user('agybadtruncated')+b'\x80'),
 (14,blob(19,b'\x12\xff')),
 (14,blob(19,blob(2,b'\xff'))),
 (14,b'\x00'),
 (14,b'\x80'*11),
 (14,blob(19,num(2,12))),
 (999,user('agyunknownstep')),
]
with sqlite3.connect(root/'conversation_summaries.db') as db:
 db.execute('PRAGMA journal_mode=WAL')
 db.execute('CREATE TABLE conversation_summaries(conversation_id TEXT PRIMARY KEY,title TEXT,preview TEXT,step_count INTEGER,last_modified_time TEXT,workspace_uris TEXT,parent_conversation_id TEXT)')
 for sid,parent,count in [('agy-demo','',len(steps)),('agy-child','agy-demo',1),('agy-empty','',0),('agy-missing','',1),('agy-bad-workspace','',1)]:
  db.execute('INSERT INTO conversation_summaries VALUES(?,?,?,?,?,?,?)',(sid,'subagent-demo' if parent else 'Tea notes','',count,'2026-09-27 10:00:00+00:00','bad JSON' if sid=='agy-bad-workspace' else json.dumps(['file:///work/agy%20demo','file:///work/other']),parent))
  if sid=='agy-missing': continue
  with sqlite3.connect(root/'conversations'/f'{sid}.db') as conv:
   conv.execute('PRAGMA journal_mode=WAL')
   conv.execute('CREATE TABLE steps(idx INTEGER PRIMARY KEY,step_type INTEGER,step_payload BLOB)')
   data=steps if sid=='agy-demo' else [(14,user('agychildtoken'))] if parent else [(14,user('agybadworkspacetoken'))] if sid=='agy-bad-workspace' else []
   conv.executemany('INSERT INTO steps VALUES(?,?,?)',[(i,t,p) for i,(t,p) in enumerate(data)])
PY

# Relocated copies exercise native config dirs, including spaces in paths.
mkdir -p "$home/relocated/claude config" "$home/relocated/codex config" "$home/relocated/pi config"
cp -R "$home/.claude/projects" "$home/relocated/claude config/projects"
cp -R "$home/.codex/sessions" "$home/relocated/codex config/sessions"
cp -R "$home/.pi/agent/sessions" "$home/relocated/pi config/sessions"

printf '%s\n' "$home"

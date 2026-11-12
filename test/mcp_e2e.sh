#!/bin/bash
# Sourced by e2e.sh after building; uses only a synthetic HOME.
start=$SECONDS
if python3 - "$BIN" "$H/mcp-home" "$TEST" <<'PY' > "$H/mcp-output.txt" 2>&1
import datetime,hashlib,json,os,pathlib,subprocess,sys
binary,home,test=sys.argv[1:]; home=pathlib.Path(home); test=pathlib.Path(test)
subprocess.run([str(test/'fixture.sh'),str(home)],check=True,stdout=subprocess.DEVNULL)
long='mcplongtoken '+('mock text <tag> 魚池\n'*150)
source=home/'.pi/agent/sessions/-work-demo/mcp-mock.jsonl'
source.write_text('\n'.join(json.dumps(row) for row in [
 {'type':'session','version':3,'id':'mcp-mock','timestamp':'2026-09-27T12:00:00Z','cwd':'/work/demo'},
 *[{'type':'message','id':f'mcp-message-{i}','timestamp':f'2026-09-27T12:00:{i+1:02d}Z','message':{'role':'user','content':[{'type':'text','text':long if i==0 else f'mock context {i}'}]}} for i in range(13)]
])+'\n')
now=datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z')
def session(name,messages):
 path=source.with_name(name+'.jsonl')
 rows=[{'type':'session','version':3,'id':name,'timestamp':now,'cwd':'/work/demo'}]
 for i,(role,text) in enumerate(messages):
  rows.append({'type':'message','id':f'{name}-{i}','timestamp':now,'message':{'role':role,'content':[{'type':'text','text':text}]}})
 path.write_text('\n'.join(json.dumps(row) for row in rows)+'\n')
session('mcp-bulk',[('user',f'bulk question {i}') for i in range(130)])
outline_messages=[('user','outline question one'),('assistant','intermediate one'),('toolResult','tool output one'),
 ('assistant','final reply one'),('toolResult','trailing tool output'),('user','outline question two'),
 ('assistant','intermediate two'),('toolResult','tool output two'),('assistant','final reply two'),('user','pending question')]
session('mcp-outline',outline_messages)
parts=lambda *texts: [{'type':'text','text':text} for text in texts]
source.with_name('mcp-multipart.jsonl').write_text('\n'.join(json.dumps(row) for row in [
 {'type':'session','version':3,'id':'mcp-multipart','timestamp':now,'cwd':'/work/demo'},
 *[{'type':'message','id':f'multipart-{i}','timestamp':now,'message':{'role':role,'content':content}}
   for i,(role,content) in enumerate([
    ('user',parts('multipart question')),
    ('assistant',parts('intermediate answer','intermediate footnote')),
    ('toolResult',parts('multipart tool output')),
    ('assistant',parts('complete final answer','final detail','final footnote')),
    ('user',parts('multipart pending question'))])]
])+'\n')
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'),KIOKU_DEBUG_TIMING='1',CLAUDE_PROJECT_DIR='/work/demo')
def cli_text(*args):
 p=subprocess.run([binary,*args],env=env,text=True,capture_output=True,timeout=30)
 assert p.returncode==0,p.stderr
 return p.stdout
def cli(*args):
 p=subprocess.run([binary,*args],env=env,text=True,capture_output=True,timeout=30)
 assert p.returncode==0,p.stderr
 return json.loads(p.stdout)
def request(i,method,params=None):
 value={'jsonrpc':'2.0','id':i,'method':method}
 if params is not None: value['params']=params
 return value
def call(i,name,args): return request(i,'tools/call',{'name':name,'arguments':args})
def pipe(lines,newline=True):
 text='\n'.join(line if isinstance(line,str) else json.dumps(line) for line in lines)+('\n' if newline else '')
 p=subprocess.run([binary,'mcp'],input=text,env=env,text=True,capture_output=True,timeout=30)
 assert p.returncode==0,p.stderr
 rows=[json.loads(line) for line in p.stdout.splitlines()]
 assert rows and all(isinstance(row,dict) and row['jsonrpc']=='2.0' and 'id' in row and (('result' in row) != ('error' in row)) for row in rows),p.stdout
 return rows,p.stderr
checks=[]
def passed(name): checks.append(name)
search=cli('--json','snapshot','--limit','1')
sessions=cli('--json','--sessions','snapshot','--limit','1')
ref=search['hits'][0]['ref']; long_ref=cli('--json','mcplongtoken')['hits'][0]['ref']
show_all=cli('show',long_ref.split(':')[0],'--json','--all')
before={str(p.relative_to(home)):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*') if p.is_file() and p.suffix=='.jsonl'}
lines=[request(1,'initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'mock-client','version':'1'}}),
 {'jsonrpc':'2.0','method':'notifications/initialized'},request(2,'tools/list'),
 call(3,'kioku_search',{'query':'snapshot','limit':1}),
 call(4,'kioku_sessions',{'query':'snapshot','limit':1}),
 call(5,'kioku_show',{'ref':ref,'context':1,'query':'snapshot'}),
 call(6,'kioku_search',{'query':'snapshot','limit':1,'cursor':search['next_cursor']}),
 call(7,'kioku_sessions',{'query':'snapshot','limit':1,'cursor':sessions['next_cursor']}),
 call(8,'kioku_show',{'ref':long_ref,'context':0}),
 call(9,'kioku_show',{'ref':long_ref,'context':0,'full':True}),
 call(10,'kioku_show',{'ref':ref.split(':')[0],'all':True}),
 call(11,'kioku_search',{'query':'snapshot','harness':'pi','project':'DEMO','all_time':True,'include_self':True}),
 call(12,'kioku_sessions',{'query':'snapshot','harness':'codex','project':'demo'}),
 call(13,'kioku_show',{'ref':long_ref.split(':')[0],'all':True,'cursor':show_all['next_cursor']}),
 call(14,'kioku_search',{'query':''}),call(15,'kioku_sessions',{'query':''}),
 request('mock-string-id','ping')]
rows,stderr=pipe(lines); by_id={row['id']:row for row in rows}
assert len(rows)==len(lines)-1 and len(by_id)==len(rows)
passed('stdout contains JSON-RPC lines only; initialized notification has no reply')
init=by_id[1]['result']
assert init['protocolVersion']=='2025-06-18' and init['serverInfo']['name']=='kioku' and init['serverInfo']['version'] and init['capabilities']['tools']=={},init
assert by_id['mock-string-id']['result']=={}
passed('initialize and ping preserve numeric/string request IDs')
tools={tool['name']:tool for tool in by_id[2]['result']['tools']}
expected={'kioku_search':({'query','harness','project','limit','cursor','all_time','include_self','since','until','facets','max_per_session','role','include_automated','model','cwd'},'query'),
 'kioku_sessions':({'query','harness','project','limit','cursor','since','until','facets','include_automated','model','cwd'},'query'),
 'kioku_show':({'ref','context','query','full','all','cursor','limit','outline'},'ref'),
 'kioku_context':(set(),None)}
assert tools.keys()==expected.keys()
for name,(properties,required) in expected.items():
 tool=tools[name]; schema=tool['inputSchema']
 assert tool['annotations']['readOnlyHint'] is True and schema['type']=='object'
 assert set(schema['properties'])==properties and schema['required']==([required] if required else []) and schema['additionalProperties'] is False,schema
 if required: assert schema['properties'][required]['type']=='string'
 for key in properties:
  assert schema['properties'][key]['type']==('boolean' if key in {'all_time','include_self','full','all','facets','outline','include_automated'} else 'integer' if key in {'context','limit','max_per_session'} else 'string')
assert tools['kioku_show']['inputSchema']['properties']['limit']=={'type':'integer','minimum':1,'maximum':100,'default':10}
passed('four read-only tools expose complete, strict input schemas; no rebuild')
def page(i):
 result=by_id[i]['result']; assert not result.get('isError',False),result
 assert len(result['content'])==1 and result['content'][0]['type']=='text',result
 return json.loads(result['content'][0]['text'])
assert page(3)==search and page(3)['total']>1
passed('search JSON matches --json')
assert page(4)==sessions and page(4)['total']>1
passed('sessions JSON matches --json')
assert page(5)==cli('show',ref,'--json','--context','1','--query','snapshot')
passed('show JSON matches --json')
assert page(6)==cli('--json','snapshot','--limit','1','--cursor',search['next_cursor'])
assert page(7)==cli('--json','--sessions','snapshot','--limit','1','--cursor',sessions['next_cursor'])
assert page(13)==cli('show',long_ref.split(':')[0],'--json','--all','--cursor',show_all['next_cursor'])
assert page(13)['shown']==3
passed('all three tool cursors interoperate with CLI')
assert len(page(8)['messages'][0]['text'])<len(long) and not page(8)['messages'][0].get('full',False)
assert page(9)==cli('show',long_ref,'--json','--context','0','--full')
assert page(9)['messages'][0]['text']==long.strip() and page(9)['messages'][0]['full'] is True
passed('show caps text by default; full=true returns only the full selected message')
assert page(10)==cli('show',ref.split(':')[0],'--json','--all')
assert page(11)==cli('--json','snapshot','--harness','pi','--project','DEMO','--all-time','--include-self')
assert page(12)==cli('--json','--sessions','snapshot','--harness','codex','--project','demo')
assert page(14)==cli('--json','') and page(15)==cli('--json','--sessions','')
passed('harness/project/all_time/include_self/all flags and empty-query browsing match CLI')
assert 'timing ' in stderr
passed('debug logs go to stderr')
rows,_=pipe([call(20,'kioku_show',{'ref':'mcp-bulk','all':True,'limit':12}),
 call(21,'kioku_show',{'ref':'mcp-bulk','all':True,'limit':10000}),
 call(22,'kioku_show',{'ref':'mcp-bulk','all':True}),
 call(23,'kioku_show',{'ref':'mcp-outline','outline':True}),
 call(24,'kioku_show',{'ref':'mcp-outline','outline':True,'limit':2}),
 call(25,'kioku_context',{})])
by_id={row['id']:row for row in rows}
assert page(20)['shown']==12 and page(20)==cli('show','mcp-bulk','--json','--all','--limit','12')
assert page(21)['shown']==100 and page(21)['next_cursor'] and page(22)['shown']==10
capped=page(21)
rows,_=pipe([call(26,'kioku_show',{'ref':'mcp-bulk','all':True,'limit':10000,'cursor':capped['next_cursor']})])
by_id[26]=rows[0]
assert page(26)['shown']==30 and not page(26).get('next_cursor')
passed('show limit defaults to 10, accepts 12, caps huge limits at 100 and pages the remainder')
outline=page(23)
assert [m['text'] for m in outline['messages']]==['outline question one','final reply one','outline question two','final reply two','pending question'],outline
assert [m['role'] for m in outline['messages']]==['user','asst','user','asst','user']
assert outline['total']==5 and outline['session_total']==10
assert outline==cli('show','mcp-outline','--json','--outline')
assert 'tool output' not in cli_text('show','mcp-outline','--outline')
assert 'final reply two' in cli_text('show','mcp-outline','--outline','--format','md')
passed('outline skips tools and intermediate replies, retains final replies and a trailing user; CLI text/JSON/Markdown agree')
p=page(24); collected=[]
while True:
 collected.extend(p['messages'])
 if not p.get('next_cursor'): break
 args={'ref':'mcp-outline','outline':True,'limit':2,'cursor':p['next_cursor']}
 rows,_=pipe([call(27,'kioku_show',args)]); by_id[27]=rows[0]
 p=page(27)
 assert p==cli('show','mcp-outline','--json','--outline','--limit','2','--cursor',args['cursor'])
assert collected==outline['messages']
passed('outline pagination covers each selected message once and cursors interoperate with CLI')
args={'ref':'mcp-multipart','outline':True,'limit':2}; collected=[]
while True:
 rows,_=pipe([call(30,'kioku_show',args)]); by_id[30]=rows[0]
 p=page(30)
 cli_args=['show','mcp-multipart','--json','--outline','--limit','2']
 if 'cursor' in args: cli_args+=['--cursor',args['cursor']]
 assert p==cli(*cli_args)
 collected.extend(m['text'] for m in p['messages'])
 if not p.get('next_cursor'): break
 args['cursor']=p['next_cursor']
assert collected==['multipart question','complete final answer','final detail','final footnote','multipart pending question'],collected
assert 'complete final answer' in cli_text('show','mcp-multipart','--outline')
assert 'final detail' in cli_text('show','mcp-multipart','--outline','--format','md')
passed('multipart outline preserves every final-reply text block across MCP/CLI pages and omits intermediate blocks')
context=by_id[25]['result']; assert not context.get('isError',False),context
assert context['content']==[{'type':'text','text':cli_text('context')}]
assert 'last asked:' in context['content'][0]['text']
env['CLAUDE_PROJECT_DIR']='/no-history'
rows,_=pipe([call(28,'kioku_context',{})])
assert rows[0]['result']['content']==[{'type':'text','text':cli_text('context')}]==[{'type':'text','text':''}]
env['CLAUDE_PROJECT_DIR']='/work/demo'
index=env['KIOKU_INDEX']; env['KIOKU_INDEX']=str(home/'missing-index.db')
rows,_=pipe([call(29,'kioku_context',{})])
assert rows[0]['result']['content']==[{'type':'text','text':cli_text('context')}]==[{'type':'text','text':''}]
assert not pathlib.Path(env['KIOKU_INDEX']).exists()
env['KIOKU_INDEX']=index
passed('context tool returns byte-identical CLI project context, including empty history and a missing index')
corrupt=home/'corrupt-index.db'; corrupt.write_bytes(b'not a SQLite database')
for bad_index in [corrupt,corrupt/'index.db']:
 env['KIOKU_INDEX']=str(bad_index)
 rows,_=pipe([call(31,'kioku_context',{})])
 result=rows[0]['result']
 assert result.get('isError') is True and result['content'][0]['text'],result
 assert cli_text('context')==''
env['KIOKU_INDEX']=index
passed('context reports corrupt-index and stat failures as MCP tool errors while CLI session starts stay silent and successful')
errors=[('kioku_show',{'ref':'mcp-bulk','limit':0}),('kioku_show',{'ref':'mcp-bulk','limit':-1}),
 ('kioku_show',{'ref':'mcp-bulk','limit':1.5}),('kioku_show',{'ref':'mcp-outline','outline':'yes'}),
 ('kioku_context',{'project':'demo'}),('unknown_tool',{}),('kioku_search',{}),('kioku_search',{'query':7}),
 ('kioku_search',{'query':'snapshot','limit':0}),('kioku_search',{'query':'snapshot','limit':1.5}),
 ('kioku_search',{'query':'snapshot','harness':'unknown'}),('kioku_search',{'query':'snapshot','project':''}),
 ('kioku_search',{'query':'snapshot','rebuild':True}),('kioku_search',{'query':'snapshot','full':True}),
 ('kioku_search',{'query':'snapshot','include_self':'yes'}),('kioku_sessions',{'query':'snapshot','all_time':True}),
 ('kioku_search',{'query':'snapshot','cursor':'bad'}),('kioku_show',{'ref':'missing-mock-session'}),
 ('kioku_show',{'ref':ref,'context':-1}),('kioku_show',{'ref':ref,'full':None}),
 ('kioku_show',{'ref':ref.split(':')[0],'full':True}),('kioku_show',{'ref':''}),
 ('kioku_search',[]),('kioku_search',None)]
bad_lines=[call(100+i,name,args) for i,(name,args) in enumerate(errors)]
bad_lines += [call(150,'kioku_search',{'query':'snapshot','limit':1,'cursor':sessions['next_cursor']}),
 request(151,'tools/call',{'name':'kioku_search'}),'not-json','{broken',
 '[]','null',request(200,'unknown/method'),request(201,'tools/call',[]),
 {'jsonrpc':'1.0','id':202,'method':'ping'},request(203,'initialize',{'protocolVersion':7}),
 {'jsonrpc':'2.0','method':'tools/call','params':{'name':'unknown_tool','arguments':{}}},request(204,'ping')]
rows,_=pipe(bad_lines); by_id={row['id']:row for row in rows if row['id'] is not None}
for i in [*(100+i for i in range(len(errors))),150,151]:
 result=by_id[i]['result']; assert result['isError'] is True and result['content'][0]['type']=='text',result
passed('unknown tools, missing/wrong/null/extra arguments, invalid cursors and query errors are tool errors')
assert [row['error']['code'] for row in rows if row['id'] is None]==[-32700,-32700,-32600,-32600]
for i,code in [(200,-32601),(201,-32602),(202,-32600),(203,-32602)]: assert by_id[i]['error']['code']==code,by_id[i]
assert by_id[204]['result']=={} and len(rows)==len(bad_lines)-1
passed('malformed lines and protocol errors recover; notifications never reply')
too_large={'jsonrpc':'2.0','id':None,'error':{'code':-32600,'message':'Request too large'}}
ping_reply={'jsonrpc':'2.0','id':301,'result':{}}
oversized=json.dumps(request(300,'ping'))+' '*(1<<20)
for line in [oversized,'x'*(3<<20)]:
 rows,_=pipe([line,request(301,'ping')])
 assert rows==[too_large,ping_reply],rows
passed('oversized requests are discarded with null-ID -32600; next ping succeeds')
exact=json.dumps(request(301,'ping')); exact+=' '*((1<<20)-len(exact))
assert pipe([exact])[0]==[ping_reply]
assert pipe([exact],newline=False)[0]==[ping_reply]
assert pipe([oversized],newline=False)[0]==[too_large]
passed('1 MiB boundary is accepted; normal and oversized final lines exit cleanly at EOF')
after={str(p.relative_to(home)):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*') if p.is_file() and p.suffix=='.jsonl'}
assert before==after
passed('source transcripts remain byte-identical')
repo=test.parent
plugin=json.loads((repo/'.claude-plugin/plugin.json').read_text())
assert plugin['name']=='kioku' and plugin['description'] and plugin['repository']=='https://github.com/Ray0907/kioku'
release=subprocess.check_output(['git','describe','--tags','--abbrev=0'],cwd=repo,text=True).strip().removeprefix('v')
assert plugin['version']==release
assert json.loads((repo/'.mcp.json').read_text())=={'mcpServers':{'kioku':{'command':'kioku','args':['mcp']}}}
assert (repo/'skills/kioku/SKILL.md').is_file()
marketplace=json.loads((repo/'.claude-plugin/marketplace.json').read_text())
assert marketplace['name']=='kioku' and marketplace['plugins'][0]['name']=='kioku' and marketplace['plugins'][0]['source']=='./'
passed('Claude Code plugin/marketplace metadata and root MCP config are valid')
artifact={'protocol_version':'2025-06-18','tools':list(expected),'pass':len(checks),'fail':0,'checks':checks}
(test/'mcp-e2e-result.json').write_text(json.dumps(artifact,indent=2)+'\n')
print(json.dumps(artifact,indent=2))
PY
then record 'MCP stdio: tools, JSON parity, errors, recovery, plugin packaging' PASS $((SECONDS-start)) 'test/mcp-e2e-result.json (repeatable mock-only artifact)'
else record 'MCP stdio: tools, JSON parity, errors, recovery, plugin packaging' FAIL $((SECONDS-start)) "$(<"$H/mcp-output.txt")"; fi

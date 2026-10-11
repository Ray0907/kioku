#!/bin/bash
# Sourced by e2e.sh after building; uses only a synthetic HOME.
start=$SECONDS
if python3 - "$BIN" "$H/mcp-home" "$TEST" <<'PY' > "$H/mcp-output.txt" 2>&1
import hashlib,json,os,pathlib,subprocess,sys
binary,home,test=sys.argv[1:]; home=pathlib.Path(home); test=pathlib.Path(test)
subprocess.run([str(test/'fixture.sh'),str(home)],check=True,stdout=subprocess.DEVNULL)
long='mcplongtoken '+('mock text <tag> 魚池\n'*150)
source=home/'.pi/agent/sessions/-work-demo/mcp-mock.jsonl'
source.write_text('\n'.join(json.dumps(row) for row in [
 {'type':'session','version':3,'id':'mcp-mock','timestamp':'2026-09-27T12:00:00Z','cwd':'/work/demo'},
 *[{'type':'message','id':f'mcp-message-{i}','timestamp':f'2026-09-27T12:00:{i+1:02d}Z','message':{'role':'user','content':[{'type':'text','text':long if i==0 else f'mock context {i}'}]}} for i in range(13)]
])+'\n')
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'),KIOKU_DEBUG_TIMING='1')
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
expected={'kioku_search':({'query','harness','project','limit','cursor','all_time','include_self'},'query'),
 'kioku_sessions':({'query','harness','project','limit','cursor'},'query'),
 'kioku_show':({'ref','context','query','full','all','cursor'},'ref')}
assert tools.keys()==expected.keys()
for name,(properties,required) in expected.items():
 tool=tools[name]; schema=tool['inputSchema']
 assert tool['annotations']['readOnlyHint'] is True and schema['type']=='object'
 assert set(schema['properties'])==properties and schema['required']==[required] and schema['additionalProperties'] is False,schema
 assert schema['properties'][required]['type']=='string'
 for key in properties:
  assert schema['properties'][key]['type']==('boolean' if key in {'all_time','include_self','full','all'} else 'integer' if key in {'context','limit'} else 'string')
passed('three read-only tools expose complete, strict input schemas; no rebuild')
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
errors=[('unknown_tool',{}),('kioku_search',{}),('kioku_search',{'query':7}),
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

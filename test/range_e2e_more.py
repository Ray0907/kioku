# Sourced by test/range_e2e.sh with exec(); shares its globals.
# ---- MCP -----------------------------------------------------------------------------------
def mcp(calls):
	lines=[{'jsonrpc':'2.0','id':i,'method':'tools/call','params':{'name':n,'arguments':a}} for i,(n,a) in enumerate(calls,1)]
	lines.insert(0,{'jsonrpc':'2.0','id':0,'method':'tools/list'})
	p=subprocess.run([binary,'mcp'],input=''.join(json.dumps(x)+'\n' for x in lines),env=env,text=True,capture_output=True,timeout=60)
	assert p.returncode==0,p.stderr
	return {r['id']:r['result'] for r in map(json.loads,p.stdout.splitlines())}
res=mcp([('kioku_search',{'query':'agetok','since':'6d','facets':True}),
	('kioku_sessions',{'query':'agetok','until':'6d'}),
	('kioku_search',{'query':'captok','max_per_session':2}),
	('kioku_search',{'query':'agetok','since':'nonsense'}),
	('kioku_search',{'query':'agetok','since':'2026-03-10','until':'2026-03-09'}),
	('kioku_sessions',{'query':'captok','max_per_session':2}),
	('kioku_show',{'ref':'x:0','since':'7d'}),
	('kioku_search',{'query':'agetok','max_per_session':0})])
body=lambda i: json.loads(res[i]['content'][0]['text'])
assert body(1)==js('agetok','--since','6d','--facets')
assert body(2)==js('--sessions','agetok','--until','6d')
assert body(3)==js('captok','--max-per-session','2')
for i in [4,5,6,7,8]: assert res[i]['isError'] is True,(i,res[i])
props={t['name']:set(t['inputSchema']['properties']) for t in res[0]['tools']}
assert {'since','until','facets','max_per_session'}<=props['kioku_search']
assert {'since','until','facets'}<=props['kioku_sessions'] and 'max_per_session' not in props['kioku_sessions']
assert not {'since','until','facets','max_per_session'}&props['kioku_show']
passed('MCP kioku_search and kioku_sessions accept the new arguments and match the CLI; bad values are tool errors')

# ---- odd timestamps: offsets, long fractions, garbage (from sources) and empty (in the index) ----
write_session('odd','odd-1',[('2026-03-10T08:00:00+08:00','user','emptytok offset equals 00:00:00Z'),
	('2026-03-10T07:59:59+08:00','user','emptytok offset equals 23:59:59Z the day before'),
	('2026-03-10T00:00:00.123456789Z','user','emptytok nine digit fraction'),
	('garbage','user','emptytok garbage stamp'),('2026-03-12T00:00:00Z','user','emptytok will lose its stamp')])
run(['index'])
with sqlite3.connect(home/'index.db') as db:  # no importable path writes ts='' (sync falls back), so edit the cache
	db.execute("UPDATE messages SET ts='' WHERE text LIKE '%will lose its stamp'")
assert js('emptytok')['total']==5
assert js('emptytok','--since','2026-03-10')['total']==2
assert js('emptytok','--until','2026-03-09')['total']==1
assert js('emptytok','--since','1970-01-01')['total']==3 and js('emptytok','--until','2100-01-01')['total']==3
passed('empty and unparseable timestamps never match a range; offsets and 9-digit fractions compare correctly')
odd=js('emptytok','--facets')['facets']
assert sum(x['count'] for x in odd['week'])==5 and any(x['value']=='(unknown)' for x in odd['week'])
passed('facets count undated rows as (unknown) instead of failing')

# ---- cost on a larger corpus (facets and cap must stay cheap) -----------------------------------
import time
big=tmp/'big'; (big/'.pi/agent/sessions/-work-perf').mkdir(parents=True)
for s_ in range(200):
	lines=[{'type':'session','version':3,'id':f'perf-{s_}','timestamp':'2026-01-01T00:00:00Z','cwd':f'/work/perf{s_%30}'}]
	for i in range(100):
		d=dt.datetime(2026,1,1,tzinfo=dt.timezone.utc)+dt.timedelta(hours=s_*7+i)
		lines.append({'type':'message','id':f'p{s_}-{i}','timestamp':d.strftime('%Y-%m-%dT%H:%M:%SZ'),
			'message':{'role':'user','content':[{'type':'text','text':f'perftok rare{s_} filler {i} '+'word '*20}]}})
	(big/'.pi/agent/sessions/-work-perf'/f'perf-{s_}.jsonl').write_text('\n'.join(json.dumps(x) for x in lines)+'\n')
benv=dict(env,HOME=str(big),KIOKU_INDEX=str(big/'index.db'))
subprocess.run([binary,'index'],env=benv,check=True,capture_output=True,timeout=300)
def timed(*args):
	t=time.perf_counter(); p=subprocess.run([binary,'--json',*args],env=benv,text=True,capture_output=True,timeout=60)
	assert p.returncode==0,p.stderr; return time.perf_counter()-t,json.loads(p.stdout)
timed('perftok')  # warm the page cache
plain,_=timed('perftok')
for extra in (['--facets'],['--max-per-session','2'],['--since','2026-02-01','--until','2026-03-01','--facets','--max-per-session','2']):
	took,page=timed('perftok',*extra)
	assert took<max(1.5,plain*8),(extra,took,plain)
	if '--facets' in extra: assert sum(x['count'] for x in page['facets']['project'])==page['total']
passed('on 20,000 messages, facets, cap and range each stay under max(1.5 s, 8x the plain search)')

# ---- read-only ----------------------------------------------------------------------------------
after={str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*.jsonl') if str(p) in sources}
assert after==sources
passed('source transcripts stay byte-identical')


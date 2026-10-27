#!/bin/bash
# E2E for --since/--until, --facets and --max-per-session (CLI and MCP).
# Uses only a synthetic HOME. Compares defaults against the v0.7.0 binary.
# Writes test/range-e2e-result.json; the file has no timestamps, so reruns match.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
# The artifact exists only if the latest run passed.
rm -f "$ROOT/test/range-e2e-result.json"
BIN=${BIN:-$ROOT/kioku}
BASE_REF=${BASE_REF:-v0.7.0}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/kioku-range.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

[ -x "$BIN" ] || (cd "$ROOT" && make build >/dev/null)
BASE_BIN=${BASE_BIN:-}
if [ -z "$BASE_BIN" ]; then
	mkdir -p "$TMP/base"
	git -C "$ROOT" archive "$BASE_REF" | tar -x -C "$TMP/base"
	(cd "$TMP/base" && make build >/dev/null 2>&1)
	BASE_BIN="$TMP/base/kioku"
fi

python3 - "$BIN" "$BASE_BIN" "$BASE_REF" "$TMP" "$ROOT/test" <<'PY'
import datetime as dt,hashlib,json,os,pathlib,pty,re,select,sqlite3,subprocess,sys
binary,base_bin,base_ref,tmp,test=sys.argv[1:]
tmp=pathlib.Path(tmp); test=pathlib.Path(test)
home=tmp/'home'
checks=[]
def passed(name): checks.append(name)

# ---- synthetic corpus -------------------------------------------------
subprocess.run([str(test/'fixture.sh'),str(home)],check=True,stdout=subprocess.DEVNULL)
pi_root=home/'.pi/agent/sessions'
def utc(s): return dt.datetime.fromisoformat(s.replace('Z','+00:00'))
def write_session(project,sid,rows):
	d=pi_root/f'-work-{project}'; d.mkdir(parents=True,exist_ok=True)
	lines=[{'type':'session','version':3,'id':sid,'timestamp':rows[0][0],'cwd':f'/work/{project}'}]
	for i,(ts,role,text) in enumerate(rows):
		lines.append({'type':'message','id':f'{sid}-{i}','timestamp':ts,
			'message':{'role':role,'content':[{'type':'text','text':text}]}})
	(d/f'{sid}.jsonl').write_text('\n'.join(json.dumps(x) for x in lines)+'\n')

now=dt.datetime.now(dt.timezone.utc)
def ago(days,hour=12): # a UTC instant at hour:00 UTC, `days` ago
	d=(now-dt.timedelta(days=days)).replace(hour=hour,minute=0,second=0,microsecond=0)
	return d.strftime('%Y-%m-%dT%H:%M:%SZ')

# Boundary session: fractional seconds around 2026-03-10T00:00:00Z.
write_session('bound','bound-1',[
	('2026-03-09T23:59:59.999Z','user','boundtok before midnight'),
	('2026-03-10T00:00:00Z','user','boundtok exactly midnight'),
	('2026-03-10T00:00:00.5Z','user','boundtok half past midnight'),
	('2026-03-10T16:30:00Z','user','boundtok taipei next morning'),
])
# Recent sessions at known ages, three projects.
write_session('alpha','alpha-1',[(ago(2),'user','agetok two days ago')])
write_session('alpha','alpha-2',[(ago(5),'user','agetok five days ago')])
write_session('beta','beta-1',[(ago(9),'user','agetok nine days ago')])
write_session('gamma','gamma-1',[(ago(20),'user','agetok twenty days ago')])
# Short Latin prefix: default window is 7 days before the newest message.
write_session('short','short-old',[('2026-03-01T10:00:00Z','user','xq old marker')])
write_session('short','short-new',[(ago(1),'user','xq new marker')])
# Cap session: one session with 5 hits of different relevance, plus two others.
write_session('capx','cap-big',[
	(ago(3,9),'user','captok'),
	(ago(3,10),'user','captok captok captok best'),
	(ago(3,11),'user','captok filler words here to dilute score a lot more than needed'),
	(ago(3,12),'user','captok another filler line to dilute the score of this message'),
	(ago(3,13),'user','captok final filler line to dilute the score of this message too'),
])
write_session('capy','cap-y',[(ago(4),'user','captok one hit')])
write_session('capz','cap-z',[(ago(6),'user','captok one hit')])
# Common-word corpus: 400 messages in 20 sessions, 20 days; word hits >90% of index.
for s in range(20):
	write_session('commonp'+str(s%4),f'common-{s}',
		[(ago(s,8+(i%8)),'user',f'commontok number {s}-{i}') for i in range(20)])
# Facet limits: 60 weekly sessions over 25 projects (limits are 20 projects, 52 weeks).
for i in range(60):
	day=dt.datetime(2025,1,6,12,tzinfo=dt.timezone.utc)+dt.timedelta(days=7*i)
	write_session(f'facet{i%25}',f'facet-{i}',[(day.strftime('%Y-%m-%dT%H:%M:%SZ'),'user',f'facettok commontok week {i}')])
# Sunday in UTC, Monday 01:00 in Asia/Taipei: separates UTC weeks from local weeks.
write_session('wk','wk-1',[('2026-03-15T10:00:00Z','user','weektok sunday both'),('2026-03-15T17:00:00Z','user','weektok monday in taipei')])
# Bare filler so commontok covers 90%+ but stays below 100%.
write_session('plain','plain-1',[(ago(30),'user','unrelated sentence '+str(i)) for i in range(5)])

env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'),TZ='UTC')
for k in ['KIOKU_CLAUDE_DIR','KIOKU_CODEX_DIR','KIOKU_PI_DIR','KIOKU_GROK_DIR','KIOKU_OPENCODE_DB','KIOKU_CURSOR_DIR','KIOKU_AGY_DIR','XDG_DATA_HOME','CLAUDE_CONFIG_DIR','CODEX_HOME','PI_CODING_AGENT_DIR']:
	env.pop(k,None)
def run(args,exe=None,tz=None,ok=True):
	e=dict(env);
	if tz: e['TZ']=tz
	p=subprocess.run([exe or binary,*args],env=e,text=True,capture_output=True,timeout=60)
	if ok: assert p.returncode==0,(args,p.stderr)
	return p
def js(*args,**kw): return json.loads(run(['--json',*args],**kw).stdout)
def fails(*args,**kw):
	p=run(['--json',*args],ok=False,**kw)
	assert p.returncode!=0 and p.stdout.strip()=='',(args,p.stdout,p.stderr)
	return p.stderr
run(['index','--rebuild'])
sources={str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in home.rglob('*.jsonl')}

def refs(page): return [h['ref'] for h in page['hits']]
def all_hits(*args,limit='7'):
	out,cursor=[],None
	while True:
		a=[*args,'--limit',limit]+(['--cursor',cursor] if cursor else [])
		page=js(*a); out+=refs(page); cursor=page.get('next_cursor')
		if not cursor: return out,page
def bodies(*args,**kw): # message bodies, via show of each hit
	return sorted(h['snippet'] for h in js(*args,'--limit','100',**kw)['hits'])

# ---- defaults must not move (compare with the baseline binary) ----------
base_env_index=tmp/'base-index.db'
def base(args):
	e=dict(env,KIOKU_INDEX=str(base_env_index))
	p=subprocess.run([base_bin,*args],env=e,text=True,capture_output=True,timeout=60)
	assert p.returncode==0,p.stderr
	return p.stdout
base(['index','--rebuild'])
for args in [['--json','agetok'],['--json','--sessions','agetok'],['--json',''],['--json','--sessions',''],
		['--json','commontok','--limit','13'],['--json','xq'],['--json','xq','--all-time'],
		['--json','boundtok','-p','bound'],['--json','--sessions','captok','--harness','pi']]:
	assert run(args).stdout==base(args),args
passed('default JSON output is byte-identical to v0.7.0 for search, sessions, browse, short prefix, common word')
def schema(path):
	with sqlite3.connect(path) as db: return db.execute("SELECT name,sql FROM sqlite_master ORDER BY name").fetchall()
assert schema(home/'index.db')==schema(base_env_index)
passed('index schema is identical to v0.7.0')

# ---- boundary: fractional seconds and half-open interval ---------------
def texts(page): return sorted(h['snippet'] for h in page['hits'])
t=js('boundtok','--since','2026-03-10','--until','2026-03-10')
assert t['total']==3 and not any('before midnight' in s for s in texts(t)),texts(t)
passed('--since DATE includes a message at 00:00:00 and one at 00:00:00.5; excludes 23:59:59.999 the day before')
t=js('boundtok','--until','2026-03-09')
assert t['total']==1 and 'before midnight' in texts(t)[0],texts(t)
passed('--until DATE is inclusive of that whole day (23:59:59.999 in, next midnight out)')
t=js('boundtok','--since','2026-03-10T00:00:00Z')
assert t['total']==3
t=js('boundtok','--since','2026-03-10T00:00:00.5Z')   # fraction is dropped: second precision
assert t['total']==3
passed('RFC3339 bounds work at second precision')

# ---- timezone ----------------------------------------------------------
# 2026-03-10T16:30:00Z is 2026-03-11 00:30 in Asia/Taipei.
utc_hits=js('boundtok','--since','2026-03-11','--until','2026-03-11',tz='UTC')
tpe_hits=js('boundtok','--since','2026-03-11','--until','2026-03-11',tz='Asia/Taipei')
assert utc_hits['total']==0 and tpe_hits['total']==1 and 'taipei' in texts(tpe_hits)[0],(utc_hits,tpe_hits)
tpe_prev=js('boundtok','--since','2026-03-10','--until','2026-03-10',tz='Asia/Taipei')
assert 'taipei' not in ' '.join(texts(tpe_prev)),texts(tpe_prev)
passed('date-only bounds use the local timezone (TZ=Asia/Taipei vs UTC)')

# ---- relative forms ----------------------------------------------------
def age_set(*args,**kw): return sorted(re.search(r'(\w+) days? ago',s).group(1) for s in texts(js('agetok',*args,**kw)))
assert age_set('--since','3d')==['two']
assert age_set('--since','6d')==['five','two']
assert age_set('--since','2w')==['five','nine','two']
assert age_set('--until','6d')==['nine','twenty']
assert age_set('--since','14d','--until','7d')==['nine']
passed('Nd and Nw resolve to local midnight; --until Nd excludes that day')
today=dt.date.today().isoformat()
write_session('today','today-1',[((now-dt.timedelta(minutes=1)).strftime('%Y-%m-%dT%H:%M:%SZ'),'user','todaytok fresh')])
run(['index'])
assert js('todaytok','--since','today')['total']==1 and js('todaytok','--until','yesterday')['total']==0
passed('today and yesterday keywords')
a=js('agetok','--since','3d'); b=js('agetok','--since=3d'); c=run(['--json','--since','3d','agetok']).stdout
assert a==b==json.loads(c)
passed('--since X, --since=X and flags before the query parse the same')

# ---- invalid input is an error, never an empty or unfiltered result -----
for bad in [['--since','garbage'],['--since','2026-13-45'],['--since','7x'],['--since',''],['--until','-1d'],
		['--since','2026-03-10','--until','2026-03-09'],['--since','2w','--until','3w']]:
	err=fails('agetok',*bad)
	assert 'since' in err or 'until' in err,(bad,err)
passed('invalid values, empty values and since after until exit nonzero with a message')
assert js('agetok','--since','2026-03-10','--until','2026-03-10')['total']==0
passed('same-day range is valid and may be empty')

# ---- implicit short-prefix window vs explicit range ----------------------
d=js('xq'); assert d['total']==1 and d.get('omitted_older')==1,d
r=js('xq','--since','2026-01-01'); assert r['total']==2 and 'omitted_older' not in r,r
passed('explicit --since replaces the 7-day short-prefix window and drops omitted_older')

# ---- common-word path ------------------------------------------------------
with sqlite3.connect(home/'index.db') as db:  # the common-word fast path needs >=300 messages and >=90% coverage
	n_all=db.execute('SELECT count(*) FROM messages').fetchone()[0]
	n_common=db.execute("SELECT count(*) FROM messages WHERE text LIKE '%commontok%'").fetchone()[0]
assert n_all>=300 and n_common*10>=n_all*9,(n_common,n_all)
total_all=js('commontok')['total']; assert total_all==460,total_all
# --since 5d keeps sessions ago(0..4) plus the 5th day only if hour >= midnight limit.
want=sum(1 for s in range(20) for i in range(20) if s<=5)  # local midnight 5d ago includes day s=5
page=js('commontok','--since','5d'); assert page['total']==want,(page['total'],want)
got,last=all_hits('commontok','--since','5d'); assert len(got)==len(set(got))==want,(len(got),want)
passed('common-word path honours the range; paging returns each hit once')
assert js('commontok','--sessions','--since','5d')['total']==6
passed('sessions mode counts sessions inside the range')

# ---- cursor stability -----------------------------------------------------------
p1=js('agetok','--since','30d','--limit','2'); assert p1['next_cursor']
p2=js('agetok','--since','30d','--limit','2','--cursor',p1['next_cursor'])
assert not set(refs(p1))&set(refs(p2))
err=fails('agetok','--since','20d','--limit','2','--cursor',p1['next_cursor'])
assert 'cursor' in err
passed('cursor works with the same range and is rejected for a different one')
assert js('agetok','--since','30d','--limit','2')['next_cursor']==p1['next_cursor']
passed('relative bounds resolve to local midnight, so repeating a call gives the same cursor')

# ---- browse (empty query) -----------------------------------------------------------
b=js('','--since','7d','--limit','100')
names={x['project'] for x in b['hits']}
assert 'gamma' not in names and 'alpha' in names,names
passed('empty-query browse filters sessions by their last message')

# ---- facets --------------------------------------------------------------------------
assert 'facets' not in js('agetok')
f=js('agetok','--facets')
assert sum(x['count'] for x in f['facets']['project'])==f['total']
assert sum(x['count'] for x in f['facets']['harness'])==f['total']
assert sum(x['count'] for x in f['facets']['week'])==f['total']
proj={x['value']:x['count'] for x in f['facets']['project']}
assert proj=={'alpha':2,'beta':1,'gamma':1},proj
assert all(re.fullmatch(r'\d{4}-\d\d-\d\d',x['value']) and dt.date.fromisoformat(x['value']).weekday()==0 for x in f['facets']['week'])
passed('facets are opt-in; counts sum to total; week buckets start on Monday')
fr=js('agetok','--facets','--since','7d'); assert {x['value'] for x in fr['facets']['project']}=={'alpha'}
passed('facets reflect the active range')
fs=js('--sessions','agetok','--facets'); assert sum(x['count'] for x in fs['facets']['project'])==fs['total']
fc=js('commontok','--facets'); assert sum(x['count'] for x in fc['facets']['project'])==fc['total']==460
fb=js('','--facets'); assert sum(x['count'] for x in fb['facets']['harness'])==fb['total']
passed('facets work for sessions, common-word and browse paths')
ff=js('facettok','--facets')['facets']
for name,limit in [('project',20),('week',52)]:
	assert ff[name][-1]['value']=='(other)' and len(ff[name])==limit+1,(name,len(ff[name]))
	assert sum(x['count'] for x in ff[name])==60
passed('lists past the limit are summed into (other); every facet list adds up to the total')
def week_end(w): return (dt.date.fromisoformat(w)+dt.timedelta(days=6)).isoformat()
for tz in ('UTC','Asia/Taipei'):
	wk=js('weektok','--facets',tz=tz)['facets']['week']
	assert {x['value'] for x in wk}=={'2026-03-09'} if tz=='UTC' else {x['value'] for x in wk}=={'2026-03-09','2026-03-16'},(tz,wk)
	for q in ('weektok','agetok','commontok'):
		for x in js(q,'--facets',tz=tz)['facets']['week']:
			if x['value']=='(other)': continue
			again=js(q,'--since',x['value'],'--until',week_end(x['value']),tz=tz)['total']
			assert again==x['count'],(tz,q,x,again)
passed('a week bucket count equals the total of --since W --until W+6, in UTC and Asia/Taipei')

# ---- per-session cap --------------------------------------------------------------------------
full=js('captok'); assert full['total']==7
capped=js('captok','--max-per-session','2'); assert capped['total']==4,capped['total']
by_session={}
for r in refs(capped): by_session[r.split(':')[0]]=by_session.get(r.split(':')[0],0)+1
assert max(by_session.values())==2 and len(by_session)==3,by_session
assert any('best' in h['snippet'] for h in capped['hits']),'cap must keep the best-ranked hit of a session'
got,_=all_hits('captok','--max-per-session','2',limit='2'); assert len(got)==len(set(got))==4
passed('cap limits hits per session in SQL: total, paging and best-ranked hit stay right; default unlimited')
cc=js('commontok','--max-per-session','1'); assert cc['total']==80,cc['total']
got,_=all_hits('commontok','--max-per-session','1',limit='6'); assert len(got)==len(set(got))==80
passed('cap works on the common-word path')
cr=js('commontok','--max-per-session','1','--since','5d'); assert cr['total']==6
passed('cap combines with a range')
for bad in ['0','-1','x']: fails('captok','--max-per-session',bad)
fails('--sessions','captok','--max-per-session','2')
passed('invalid cap values and cap with --sessions are errors')
for flag in [['--since','7d'],['--until','today'],['--facets'],['--max-per-session','2']]:
	for head in (['show','x:0'],['index']):
		p=run([*head,*flag],ok=False); assert p.returncode!=0 and p.stdout=='',(head,flag,p.stdout)
passed('range, facet and cap flags are rejected on show and index')

# ---- TUI must not silently ignore the flags ---------------------------------------------
def tty_output(args):
	pid,fd=pty.fork()
	if pid==0:
		os.execve(binary,[binary,*args],dict(env,TERM='xterm-256color'));
	out=b''
	while True:
		r,_,_=select.select([fd],[],[],15)
		if not r: break
		try: chunk=os.read(fd,65536)
		except OSError: break
		if not chunk: break
		out+=chunk
	os.waitpid(pid,0); return out.decode('utf8','replace')
o=tty_output(['--since','7d','agetok'])
assert '\x1b[?1049h' not in o and 'hits in' in o,o[:300]
passed('on a terminal, --since prints a page instead of opening a TUI that would ignore it')

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

result={'baseline':base_ref,'pass':len(checks),'fail':0,'checks':checks}
pathlib.Path(test/'range-e2e-result.json').write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n')
print(f'{len(checks)} PASS / 0 FAIL -- test/range-e2e-result.json')
PY

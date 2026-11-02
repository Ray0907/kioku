#!/bin/bash
# E2E for `show --format md`, `kioku stats` and the TUI date range key.
# Uses only a synthetic HOME. Compares text and JSON output with the baseline commit.
# Writes test/views-e2e-result.json; the file has no timestamps, so reruns match.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
rm -f "$ROOT/test/views-e2e-result.json"
BIN=${BIN:-$ROOT/kioku}
BASE_REF=${BASE_REF:-6a64f08}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/kioku-views.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

(cd "$ROOT" && make build >/dev/null)
BASE_BIN=${BASE_BIN:-}
if [ -z "$BASE_BIN" ]; then
	mkdir -p "$TMP/base"
	git -C "$ROOT" archive "$BASE_REF" | tar -x -C "$TMP/base"
	(cd "$TMP/base" && make build >/dev/null 2>&1)
	BASE_BIN="$TMP/base/kioku"
fi

command -v tmux >/dev/null || { echo "tmux is required for the TUI checks" >&2; exit 1; }
python3 - "$BIN" "$BASE_BIN" "$BASE_REF" "$TMP" "$ROOT/test" <<'PY'
import json,os,pathlib,re,shlex,sqlite3,subprocess,sys,time
binary,base_bin,base_ref,tmp,test=sys.argv[1:]
tmp=pathlib.Path(tmp); test=pathlib.Path(test)
home=tmp/'home'
checks=[]
def passed(name): checks.append(name)

subprocess.run([str(test/'fixture.sh'),str(home)],check=True,stdout=subprocess.DEVNULL)
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'),TZ='UTC')
for k in ['KIOKU_CLAUDE_DIR','KIOKU_CODEX_DIR','KIOKU_PI_DIR','KIOKU_GROK_DIR','KIOKU_OPENCODE_DB','KIOKU_CURSOR_DIR','KIOKU_AGY_DIR','XDG_DATA_HOME','CLAUDE_CONFIG_DIR','CODEX_HOME','PI_CODING_AGENT_DIR']:
	env.pop(k,None)
def run(args,exe=None,ok=True,tz=None):
	p=subprocess.run([exe or binary,*args],env=dict(env,TZ=tz or 'UTC'),text=True,capture_output=True,timeout=60)
	if ok: assert p.returncode==0,(args,p.stderr)
	return p
def js(*args): return json.loads(run(['--json',*args]).stdout)
other=home/'.pi/agent/sessions/-work-otherp'; other.mkdir(parents=True)
rows=[('2026-09-20T16:30:00Z','user','otherp question'),('2026-09-20T16:31:00Z','assistant','otherp answer')]
(other/'other-1.jsonl').write_text('\n'.join(json.dumps(x) for x in [{'type':'session','version':3,'id':'other-1','timestamp':rows[0][0],'cwd':'/work/otherp'}]+
	[{'type':'message','id':f'o{i}','timestamp':t,'message':{'role':r,'content':[{'type':'text','text':x}]}} for i,(t,r,x) in enumerate(rows)])+'\n')
run(['index','--rebuild'])

# ---- show --format md ------------------------------------------------------------------
ref=js('snapshot')['hits'][0]['ref']
sess=ref.split(':')[0]
for args in (['show',ref],['show',ref,'--context','1'],['show',ref,'--query','snapshot'],['show',sess,'--all'],['show',ref,'--full']):
	for extra in ([],['--json']):
		a,b=run([*args,*extra]),run([*args,*extra],exe=base_bin)
		assert a.stdout==b.stdout,(args,extra)
passed(f'show text and --json output is byte-identical to {base_ref} for hit, context, query, --all and --full')
md=run(['show',ref,'--format','md']).stdout
assert md.startswith('### ') and '\n**' in md and '\n> ' in md and 'Resume: `' in md,md
assert '· hit\n' in md and md==run(['show',ref,'--format','md']).stdout
passed('md renders heading, metadata, resume command, quoted messages and the hit marker, repeatably')
whole=run(['show',sess,'--all','--format','md']).stdout
assert 'hit' not in whole.split('Resume:')[0] and whole.count('\n**')>=3,whole
passed('md of a whole session has no hit marker')
for args in (['show',ref,'--format','html'],['show',ref,'--format','md','--json'],['snapshot','--format','md']):
	p=run(args,ok=False); assert p.returncode!=0 and p.stdout=='',(args,p.stdout)
passed('invalid --format values, md with --json and md outside show are errors with no output')

# ---- stats -------------------------------------------------------------------------------
db=sqlite3.connect(home/'index.db')
def truth(col):
	q=f'SELECT s.{col},count(DISTINCT m.session_uid),count(*) FROM messages m JOIN sessions s ON s.uid=m.session_uid WHERE m.self=0 GROUP BY 1'
	return {v:(a,b) for v,a,b in db.execute(q)}
st=js_stats=json.loads(run(['stats','--json']).stdout)
for col,key in (('harness','harness'),('project','project')):
	assert {r['value']:(r['sessions'],r['messages']) for r in st[key]}==truth(col),(key,st[key])
assert st['total']['sessions']==sum(a for a,_ in truth('harness').values()) and st['total']['messages']==sum(b for _,b in truth('harness').values())
newest=db.execute("SELECT strftime('%Y-%m-%dT%H:%M:%SZ',max(datetime(ts))) FROM messages WHERE self=0").fetchone()[0]
assert st['total']['last_active']==newest,(st['total'],newest)
passed('stats per harness, per project and total equal a direct count of the index; last active is the newest message')
assert [r['value'] for r in st['project']][0]=='demo' and 'otherp' in [r['value'] for r in st['project']]
txt=run(['stats']).stdout
assert txt.startswith(f"{st['total']['sessions']} sessions · {st['total']['messages']} messages · last active {newest[:10]} {newest[11:16]}\n") and 'HARNESS' in txt and 'PROJECT' in txt and 'otherp' in txt,txt
assert txt==run(['stats']).stdout
passed('stats text shows totals and both tables, repeatably')
only=json.loads(run(['stats','--json','--harness','pi','-p','OTHERP']).stdout)
assert only['total']=={'sessions':1,'messages':2,'last_active':'2026-09-20T16:31:00Z'} and [r['value'] for r in only['harness']]==['pi'] and [r['value'] for r in only['project']]==['otherp'],only
passed('stats --harness and -p filter every table and the total (project match ignores case)')
def total(*a,tz=None): return json.loads(run(['stats','--json',*a],tz=tz).stdout)['total']
assert total('--since','2026-09-25')['sessions']==st['total']['sessions']-1
assert total('--until','2026-09-20')['messages']==2 and total('--until','2026-09-19')['messages']==0
assert total('--since','2026-09-28')['messages']==0 and total('--since','2026-09-27','--until','2026-09-27')['messages']==st['total']['messages']-2
passed('stats --since/--until use the CLI bound rules: a date as --until covers that whole day, sessions without messages in range drop out')
assert total('--until','2026-09-20',tz='Asia/Taipei')['messages']==0 and total('--until','2026-09-21',tz='Asia/Taipei')['messages']==2
assert '2026-09-21 00:31' in run(['stats','-p','otherp'],tz='Asia/Taipei').stdout
passed('stats ranges and last-active times follow local time')
assert 'no messages' not in run(['stats','--since','2030-01-01']).stdout and run(['stats','--since','2030-01-01']).stdout=='0 sessions · 0 messages\n'
assert json.loads(run(['stats','--json','--since','2030-01-01']).stdout)=={'total':{'sessions':0,'messages':0,'last_active':''},'harness':[],'project':[]}
passed('an empty range prints zero totals and empty lists')
for args in (['extra'],['--harness','nope'],['--since','bogus'],['--since','2026-02-01','--until','2026-01-01']):
	p=run(['stats',*args],ok=False); assert p.returncode!=0 and p.stdout=='',(args,p.stdout)
passed('stats rejects positional arguments, unknown harnesses and bad ranges with no output')
hits=js('--','stats'); assert 'hits' in hits
passed('a search for the word stats still works after the subcommand: kioku -- stats')

# ---- TUI date range, in a real terminal (tmux) on the demo fixture --------------------
demo=tmp/'demo'
subprocess.run([sys.executable,str(test.parent/'demo/make_demo_home.py'),str(demo)],check=True,stdout=subprocess.DEVNULL)
SESSION='kioku-views-e2e'
def tmux(*a): return subprocess.run(['tmux',*a],text=True,capture_output=True,timeout=20)
def screen(): return tmux('capture-pane','-p','-t',SESSION).stdout
def wait_for(pattern,timeout=15):
	end=time.time()+timeout
	while time.time()<end:
		cur=screen()
		if re.search(pattern,cur): return cur
		time.sleep(0.15)
	raise AssertionError(f'timed out waiting for {pattern!r}:\n{screen()}')
def keys(*k):
	for x in k:
		tmux('send-keys','-t',SESSION,*(['-l',x] if len(x)==1 and x not in '\n' else [x])); time.sleep(0.25)
def type_text(text): tmux('send-keys','-t',SESSION,'-l',text); time.sleep(0.4)
def cli_total(tz,*a):
	e=dict(env,HOME=str(demo),KIOKU_INDEX=str(demo/'index.db'),TZ=tz)
	p=subprocess.run([binary,'--json','--limit','300',*a],env=e,text=True,capture_output=True,timeout=60); assert p.returncode==0,p.stderr
	j=json.loads(p.stdout); return j['total'],j['total_sessions']
def header(cur): return re.search(r'(\d+) messages · (\d+) sessions',cur)
def tui(tz,steps):
	tmux('kill-session','-t',SESSION)
	cmd=f"HOME={shlex.quote(str(demo))} KIOKU_INDEX={shlex.quote(str(demo/'index.db'))} KIOKU_THEME=dark TZ={tz} TERM=xterm-256color {shlex.quote(binary)} checkout"
	assert tmux('new-session','-d','-s',SESSION,'-x','130','-y','32',cmd).returncode==0
	try: return steps()
	finally: tmux('kill-session','-t',SESSION)
results={}
for tz in ('UTC','Asia/Taipei'):
	def steps():
		cur=wait_for(r'\d+ messages · \d+ sessions')
		all_total=header(cur).groups()
		keys('Escape'); keys('d')
		wait_for(r'range ▸ ')
		type_text('3d..yesterday'); keys('Enter')
		cur=wait_for(r'3d|\d{4}-\d\d-\d\d to \d{4}-\d\d-\d\d')
		cur=wait_for(r'\d{4}-\d\d-\d\d to \d{4}-\d\d-\d\d · \d+ messages')
		got=tuple(map(int,header(cur).groups()))
		want=cli_total(tz,'--since','3d','--until','yesterday','checkout')
		assert got==want,(tz,got,want,cur)
		assert want[0]>0 and want[0]<int(all_total[0]),(want,all_total)
		keys('d'); type_text('2099-01-01'); keys('Enter')
		wait_for(r'No message in this date range')
		keys('D'); cur=wait_for(r'\d+ messages · \d+ sessions')
		assert 'from 20' not in cur and 'No message in this date range' not in cur
		assert header(cur).groups()==all_total,(header(cur).groups(),all_total)
		keys('d'); type_text('nonsense'); keys('Enter')
		cur=wait_for(r'invalid nonsense')
		assert header(cur).groups()==all_total
		return want
	results[tz]=tui(tz,steps)
passed('TUI d sets the range with the CLI bounds: header shows it and the counts equal the CLI total for --since 3d --until yesterday, in UTC and Asia/Taipei')
passed('TUI shows the empty-range message for a range with no matches, D restores the original results, and a bad value keeps them and says why')

result={'baseline':base_ref,'pass':len(checks),'fail':0,'checks':checks}
pathlib.Path(test/'views-e2e-result.json').write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n')
print(f'{len(checks)} PASS / 0 FAIL -- test/views-e2e-result.json')
PY

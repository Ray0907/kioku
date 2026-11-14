#!/bin/bash
# Empty-query browse latency on a synthetic HOME; no live index is opened.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN=${BIN:-$ROOT/kioku}
[ -x "$BIN" ] || (cd "$ROOT" && make build >/dev/null)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/kioku-browse.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
TMP=$(cd "$TMP" && pwd)
export HOME="$TMP/home" KIOKU_INDEX="$TMP/index.db"
python3 - "$BIN" "$TMP" <<'PY'
import json,os,pathlib,statistics,subprocess,sys,time
binary,tmp=sys.argv[1:]; tmp=pathlib.Path(tmp); binary=str(pathlib.Path(binary).resolve())
env=os.environ.copy()
# Explicit roots also isolate callers with agent-specific environment overrides.
for key,rel in {'KIOKU_CLAUDE_DIR':'.claude/projects','KIOKU_CODEX_DIR':'.codex/sessions',
 'KIOKU_PI_DIR':'.pi/agent/sessions','KIOKU_GROK_DIR':'.grok/sessions',
 'KIOKU_CURSOR_DIR':'.cursor/chats','KIOKU_AGY_DIR':'.gemini/antigravity-cli',
 'KIOKU_OPENCODE_DB':'.local/share/opencode/opencode.db','KIOKU_HERMES_DB':'.hermes/state.db'}.items():
 env[key]=str(pathlib.Path(env['HOME'])/rel)
started=time.perf_counter(); root=pathlib.Path(env['KIOKU_CODEX_DIR'])/'2026/09/27'; root.mkdir(parents=True)
messages=[]
for j in range(400):
 role='user' if j%2==0 else 'assistant'
 messages.append(json.dumps({'timestamp':'2026-09-27T10:00:01Z','type':'response_item','payload':{
  'type':'message','role':role,'content':[{'type':'input_text' if role=='user' else 'output_text',
  'text':f'synthetic browse message {j}'}]}})+'\n')
body=''.join(messages)
for i in range(3000):
 meta={'timestamp':'2026-09-27T10:00:00Z','type':'session_meta','payload':{'id':f'{i:032x}','cwd':'/work/browse'}}
 (root/f'rollout-{i:04}.jsonl').write_text(json.dumps(meta)+'\n'+body)
p=subprocess.run([binary,'index','--rebuild'],env=env,capture_output=True,text=True)
assert p.returncode==0, p.stderr
assert '1200000 messages' in p.stdout+p.stderr, p.stdout+p.stderr
fixture_ms=(time.perf_counter()-started)*1000
metrics={'sessions':3000,'messages':1200000,'fixture_ms':round(fixture_ms,2),'n':5,
 'budget_ms':float(os.environ.get('KIOKU_E2E_BROWSE_P50_MS','250')),'commands':{}}
for name,args in [('json',['--json']),('sessions',['--json','--sessions']),('facets',['--json','--facets']),('stats',['stats'])]:
 samples=[]
 for _ in range(5):
  start=time.perf_counter_ns(); p=subprocess.run([binary,*args],env=env,capture_output=True)
  samples.append((time.perf_counter_ns()-start)/1e6)
  assert p.returncode==0, p.stderr.decode()
  if name!='stats': assert json.loads(p.stdout)['total']==3000
 metrics['commands'][name]={'median_ms':round(statistics.median(samples),2),'samples_ms':[round(x,2) for x in samples]}
metrics['elapsed_ms']=round((time.perf_counter()-started)*1000,2)
metrics['pass']=metrics['commands']['json']['median_ms']<=metrics['budget_ms']
(tmp/'metrics.json').write_text(json.dumps(metrics,indent=2)+'\n')
print(json.dumps(metrics,indent=2))
print('PASS' if metrics['pass'] else 'FAIL','empty-query --json p50 budget')
sys.exit(0 if metrics['pass'] else 1)
PY

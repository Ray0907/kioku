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

python3 - "$BIN" "$BASE_BIN" "$BASE_REF" "$TMP" "$ROOT/test" <<'PY'
import json,os,pathlib,subprocess,sys
binary,base_bin,base_ref,tmp,test=sys.argv[1:]
tmp=pathlib.Path(tmp); test=pathlib.Path(test)
home=tmp/'home'
checks=[]
def passed(name): checks.append(name)

subprocess.run([str(test/'fixture.sh'),str(home)],check=True,stdout=subprocess.DEVNULL)
env=dict(os.environ,HOME=str(home),KIOKU_INDEX=str(home/'index.db'),TZ='UTC')
for k in ['KIOKU_CLAUDE_DIR','KIOKU_CODEX_DIR','KIOKU_PI_DIR','KIOKU_GROK_DIR','KIOKU_OPENCODE_DB','KIOKU_CURSOR_DIR','KIOKU_AGY_DIR','XDG_DATA_HOME','CLAUDE_CONFIG_DIR','CODEX_HOME','PI_CODING_AGENT_DIR']:
	env.pop(k,None)
def run(args,exe=None,ok=True):
	p=subprocess.run([exe or binary,*args],env=env,text=True,capture_output=True,timeout=60)
	if ok: assert p.returncode==0,(args,p.stderr)
	return p
def js(*args): return json.loads(run(['--json',*args]).stdout)
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

result={'baseline':base_ref,'pass':len(checks),'fail':0,'checks':checks}
pathlib.Path(test/'views-e2e-result.json').write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n')
print(f'{len(checks)} PASS / 0 FAIL -- test/views-e2e-result.json')
PY

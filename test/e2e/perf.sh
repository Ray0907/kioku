# Large fixture: 2,000 Codex sessions x 100 user/assistant pairs = 200,000 messages.
PERF="$TMP/perfhome"; mkdir -p "$PERF"; export PERF
python3 - <<'PY'
import json,os,pathlib
root=pathlib.Path(os.environ['PERF'])/'.codex/sessions/2026/09/27'; root.mkdir(parents=True)
for i in range(2000):
 p=root/f'rollout-{i:04}.jsonl'; sid=f'{i:032x}'
 with p.open('w') as f:
  f.write(json.dumps({'timestamp':'2026-09-27T10:00:00Z','type':'session_meta','payload':{'id':sid,'cwd':'/work/perf'}})+'\n')
  for j in range(100):
   role='user' if j%2==0 else 'assistant'; typ='input_text' if role=='user' else 'output_text'
   text='perfneedle large fixture message 魚池 紅茶 日本語 한국어' if j==0 else f'neutral synthetic message {j}'
   f.write(json.dumps({'timestamp':'2026-09-27T10:00:01Z','type':'response_item','payload':{'type':'message','role':role,'content':[{'type':typ,'text':text}]}})+'\n')
PY
python3 - <<'PY'
import json,os,pathlib
root=pathlib.Path(os.environ['PERF'])
claude=root/'.claude/projects/newest-first/000-new.jsonl'; claude.parent.mkdir(parents=True)
rows=[{'type':'user','uuid':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1','timestamp':'2026-10-01T10:00:00Z','cwd':'/work/new','sessionId':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','message':{'role':'user','content':'synthetic newest session'}},{'type':'assistant','uuid':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2','timestamp':'2026-10-01T10:00:01Z','cwd':'/work/new','sessionId':'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','message':{'role':'assistant','content':[{'type':'text','text':'synthetic NEWEST_MATCH_MARKER'}]}}]
claude.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
pi=root/'.pi/agent/sessions/zz-old/999-old.jsonl'; pi.parent.mkdir(parents=True)
rows=[{'type':'session','version':3,'id':'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','timestamp':'2025-01-01T00:00:00Z','cwd':'/work/old'},{'type':'message','id':'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb1','timestamp':'2025-01-01T00:00:01Z','message':{'role':'user','content':[{'type':'text','text':'synthetic OLDEST_MATCH_MARKER'}]}}]
pi.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
export HOME="$PERF" KIOKU_INDEX="$TMP/perf.db"
python3 - "$BIN" "$TMP/perf-metrics.json" <<'PY'
import json,math,os,statistics,subprocess,sys,time
binary,outfile=sys.argv[1:]; env=os.environ.copy(); env['KIOKU_DEBUG_TIMING']='1'
t=time.perf_counter_ns(); index=subprocess.run([binary,'index','--rebuild'],env=env,text=True,capture_output=True); index_ms=(time.perf_counter_ns()-t)/1e6
queries=['perfneedle','synthetic','neutral','"large fixture"','"synthetic message"','-perfneedle','large','fixture','message','neutral 42','neutral 17','魚池','紅茶','日本語','한국어','perfneed','"neutral synthetic"','-neutral','"fixture message"','absenttoken']
latencies=[]; failures=[]; results=[]; samples=[]
for q in queries:
 t=time.perf_counter_ns(); p=subprocess.run([binary,'--json',q],env=env,text=True,capture_output=True); elapsed=(time.perf_counter_ns()-t)/1e6; latencies.append(elapsed)
 if p.returncode: failures.append({'query':q,'error':p.stderr.strip()})
 results.append(sum(1 for x in p.stdout.splitlines() if x.strip())); samples.append({'query':q,'ms':round(elapsed,2),'timing':p.stderr.strip().replace('\\n','; ')})
sorted_ms=sorted(latencies); p95=sorted_ms[math.ceil(.95*len(sorted_ms))-1]
json.dump({'index_rc':index.returncode,'index_out':index.stdout+index.stderr,'index_ms':round(index_ms,2),'n':len(latencies),'median_ms':round(statistics.median(latencies),2),'p95_ms':round(p95,2),'failures':failures,'results':results,'slowest':sorted(samples,key=lambda x:x['ms'],reverse=True)[:3]},open(outfile,'w'))
PY
metrics=$(<"$TMP/perf-metrics.json"); index_ms=$(jq -r .index_ms <<<"$metrics"); median_ms=$(jq -r .median_ms <<<"$metrics"); p95_ms=$(jq -r .p95_ms <<<"$metrics"); failures=$(jq -r '.failures|length' <<<"$metrics"); index_rc=$(jq -r .index_rc <<<"$metrics"); details="index=${index_ms}ms queries=$(jq -r .n <<<"$metrics") median=${median_ms}ms p95=${p95_ms}ms slowest=$(jq -c .slowest <<<"$metrics")"
if ((index_rc==0 && failures==0)) && grep -q '200000 messages' <<<"$(jq -r .index_out <<<"$metrics")" && awk -v p="$p95_ms" 'BEGIN{exit !(p<=200)}'; then record 'Performance: 2k sessions / 200k messages (20 varied queries)' PASS 0 "$details"; else record 'Performance: 2k sessions / 200k messages (20 varied queries)' FAIL 0 "$details; index_rc=$index_rc query_failures=$failures $(jq -r .index_out <<<"$metrics")"; fi

# Very common term: newest session file is indexed first, oldest matching file last.
start=$SECONDS; semantic=$("$BIN" --json synthetic 2>&1); semantic_rc=$?; printf '%s\n' "$semantic" > "$SCREENS/common-word.jsonl"
if ((semantic_rc==0)) && python3 - "$SCREENS/common-word.jsonl" > "$TMP/semantic-summary.txt" 2> "$TMP/semantic-error.txt" <<'PY'
import json,sys
page=json.load(open(sys.argv[1])); rows=page['hits']; harnesses={r['harness'] for r in rows}
assert 'claude' in harnesses and 'codex' in harnesses, f'only harnesses in common-word results: {sorted(harnesses)}'
assert rows and rows[0]['harness']=='claude' and 'NEWEST_MATCH_MARKER' in rows[0]['snippet'], 'newest Claude match was absent or misranked at the top'
print(f'{len(rows)} rows; harnesses={sorted(harnesses)}; newest={rows[0]["snippet"]}')
PY
then record 'Search semantics: common term is ranked by message time, not insertion order' PASS $((SECONDS-start)) "$(<"$TMP/semantic-summary.txt")"; else record 'Search semantics: common term is ranked by message time, not insertion order' FAIL $((SECONDS-start)) "exit=$semantic_rc error=$(<"$TMP/semantic-error.txt") output=$(tail -3 "$SCREENS/common-word.jsonl")"; fi

# Appends (including an incomplete final record) must be ingested without rebuilding the corpus.
append_file="$PERF/.codex/sessions/2026/09/27/rollout-0000.jsonl"
python3 - "$append_file" "$TMP/partial-rest" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); full=json.dumps({'timestamp':'2026-09-27T12:00:00Z','type':'response_item','payload':{'type':'message','role':'user','content':[{'type':'input_text','text':'appendtoken now searchable'}]}}).encode()+b'\n'
with p.open('ab') as f: f.write(full)
partial=json.dumps({'timestamp':'2026-09-27T12:01:00Z','type':'response_item','payload':{'type':'message','role':'user','content':[{'type':'input_text','text':'partialtoken completed later'}]}}).encode()
with p.open('ab') as f: f.write(partial[:-1])
pathlib.Path(sys.argv[2]).write_bytes(partial[-1:]+b'\n')
PY
start=$SECONDS; appended=$(KIOKU_DEBUG_TIMING=1 "$BIN" --json appendtoken 2>"$TMP/append-timing.txt"); append_rc=$?; append_ms=$(sed -nE 's/^timing sync_changes=([0-9.]+)ms$/\1/p' "$TMP/append-timing.txt"); after_append=$("$BIN" index 2>&1)
if ((append_rc==0)) && grep -q appendtoken <<<"$appended" && grep -q 'codex: 200001 messages' <<<"$after_append" && grep -q '0 changed' <<<"$after_append" && [[ -n $append_ms ]] && awk -v x="$append_ms" 'BEGIN{exit !(x<200)}'; then record 'Append sync: next CLI query finds new line; only tail parsed' PASS $((SECONDS-start)) "sync_changes=${append_ms}ms; $after_append"; else record 'Append sync: next CLI query finds new line; only tail parsed' FAIL $((SECONDS-start)) "query=$appended timing=$(cat "$TMP/append-timing.txt") index=$after_append"; fi
partial=$(KIOKU_DEBUG_TIMING=1 "$BIN" --json partialtoken 2>"$TMP/partial-timing.txt"); partial_rc=$?; if ((partial_rc==0)) && jq -e '.total==0 and (.hits|length)==0' <<<"$partial" >/dev/null 2>&1; then record 'Partial final line is withheld until newline' PASS 0 "$(cat "$TMP/partial-timing.txt")"; else record 'Partial final line is withheld until newline' FAIL 0 "unexpected query output: $partial"; fi
cat "$TMP/partial-rest" >> "$append_file"; completed=$("$BIN" --json partialtoken 2>&1); complete_index=$("$BIN" index 2>&1)
if grep -q partialtoken <<<"$completed" && grep -q 'codex: 200002 messages' <<<"$complete_index" && grep -q '0 changed' <<<"$complete_index"; then record 'Partial line is re-read and indexed when completed' PASS 0 "$complete_index"; else record 'Partial line is re-read and indexed when completed' FAIL 0 "query=$completed index=$complete_index"; fi


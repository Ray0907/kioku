# One transcript deliberately puts matching user/asst messages before a short tool row.
python3 - "$H" <<'PY'
import json,pathlib,sys
h=pathlib.Path(sys.argv[1]); p=h/'.claude/projects/-work-demo/55555555-5555-4555-8555-555555555555.jsonl'
sid='55555555-5555-4555-8555-555555555555'; filler=' '.join(['conversation']*70)
rows=[
 {'type':'user','uuid':'55555555-5555-4555-8555-555555555551','timestamp':'2026-09-27T11:00:00Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'user','content':'rankprobe '+filler}},
 {'type':'assistant','uuid':'55555555-5555-4555-8555-555555555552','timestamp':'2026-09-27T11:00:03Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'assistant','content':[{'type':'text','text':'rankprobe '+filler},{'type':'tool_use','id':'tool-rank-1','name':'Bash','input':{'command':'rankprobe'}}]}},
 {'type':'user','uuid':'55555555-5555-4555-8555-555555555553','timestamp':'2026-09-27T11:00:04Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':'user','content':[{'type':'tool_result','tool_use_id':'tool-rank-1','content':'toolprobeonly output from helper'}]}}
]
p.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; if ((rc==0)) && grep -q '4 files, 1 changed' <<<"$out"; then record 'Tool-order fixture indexed' PASS $((SECONDS-start)) "$out"; else record 'Tool-order fixture indexed' FAIL $((SECONDS-start)) "$out"; fi
ranked=$("$BIN" --json rankprobe 2>&1); printf '%s\n' "$ranked" > "$SCREENS/tool-order.jsonl"
if python3 - "$SCREENS/tool-order.jsonl" <<'PY' > "$TMP/tool-order.txt" 2> "$TMP/tool-order.err"
import json,sys
rows=json.load(open(sys.argv[1]))['hits']; roles=[r['role'] for r in rows]
conversation=[i for i,r in enumerate(roles) if r in ('user','asst')]; tools=[i for i,r in enumerate(roles) if r=='tool']
assert conversation and tools, f'expected user/asst and tool hits, got {roles}'
assert max(conversation)<min(tools), f'tool row ranked before conversation hits: {roles}'
for row in rows:
 text=row['snippet'].encode('utf-16-le'); assert row['highlights'], row
 for r in row['highlights']:
  assert text[2*r['location']:2*(r['location']+r['length'])].decode('utf-16-le').lower()=='rankprobe', row
print(f'roles={roles}')
PY
then record 'Conversation hits precede matching tool row in JSON order' PASS 0 "$(<"$TMP/tool-order.txt")"; else record 'Conversation hits precede matching tool row in JSON order' FAIL 0 "$(<"$TMP/tool-order.err")"; fi
tool_only=$("$BIN" --json toolprobeonly 2>&1); if jq -e 'any(.hits[]; .role=="tool" and (.snippet|contains("toolprobeonly")))' <<<"$tool_only" >/dev/null; then record 'Tool-only match remains searchable' PASS 0 ''; else record 'Tool-only match remains searchable' FAIL 0 "$tool_only"; fi

# Dedicated 23-hit session for stateless pages and show/context behavior.
python3 - "$H" <<'PY'
import json,pathlib,sys
h=pathlib.Path(sys.argv[1]); p=h/'.claude/projects/-work-demo/66666666-6666-4666-8666-666666666666.jsonl'; sid='66666666-6666-4666-8666-666666666666'; rows=[]
for i in range(23):
 text=f'pageprobe PAGE_HIT_{i:02d} '+('LongText '+('boundary '*120) if i==10 else 'short context')
 role='user' if i%2==0 else 'assistant'
 rows.append({'type':role,'uuid':f'66666666-6666-4666-8666-{i:012d}','timestamp':f'2026-09-28T12:{i:02d}:00Z','cwd':str(h/'work/demo'),'sessionId':sid,'message':{'role':role,'content':text if role=='user' else [{'type':'text','text':text}]}})
p.write_text('\n'.join(json.dumps(x) for x in rows)+'\n')
PY
start=$SECONDS; out=$("$BIN" index 2>&1); rc=$?; if ((rc==0)) && grep -q '5 files, 1 changed' <<<"$out"; then record 'Pagination/show fixture indexed' PASS $((SECONDS-start)) "$out"; else record 'Pagination/show fixture indexed' FAIL $((SECONDS-start)) "$out"; fi
full_page=$("$BIN" --json --limit 500 pageprobe 2>&1); printf '%s\n' "$full_page" > "$TMP/page-full.json"
: > "$TMP/page-refs.txt"; page=$("$BIN" --json --limit 5 pageprobe 2>&1); page_count=0; cursor=''
while [[ -n $page ]]; do
  printf '%s\n' "$page" | jq -r '.hits[].ref' >> "$TMP/page-refs.txt" || break
  cursor=$(printf '%s\n' "$page" | jq -r '.next_cursor // empty'); page_count=$((page_count+1))
  [[ -z $cursor || $page_count -ge 20 ]] && break
  page=$("$BIN" --json --limit 5 --cursor "$cursor" pageprobe 2>&1)
done
python3 - "$TMP/page-full.json" "$TMP/page-refs.txt" <<'PY' > "$TMP/page-check.txt" 2> "$TMP/page-error.txt"
import json,sys
full=json.load(open(sys.argv[1])); expected=[x['ref'] for x in full['hits']]; actual=[x.strip() for x in open(sys.argv[2]) if x.strip()]
assert full['total']==23 and len(expected)==23, f'full page expected 23 hits, got total={full["total"]} shown={len(expected)}'
assert len(actual)==len(set(actual)), 'cursor pages contain duplicate refs'
assert actual==expected, f'cursor union skipped/reordered refs: {len(actual)} vs {len(expected)}'
print(f'{len(actual)} unique refs across cursor pages; union equals --limit 500')
PY
if [[ -s "$TMP/page-check.txt" ]]; then record 'Cursor pages cover every hit exactly once' PASS "$page_count" "$(<"$TMP/page-check.txt")"; else record 'Cursor pages cover every hit exactly once' FAIL "$page_count" "$(<"$TMP/page-error.txt")"; fi

# Default compact page is ten; the cursor footer/JSON field exist iff more remain.
text_first=$("$BIN" pageprobe 2>&1); json_first=$("$BIN" --json pageprobe 2>&1)
text_cursor=$(sed -n 's/^cursor: //p' <<<"$text_first"); json_cursor=$(jq -r '.next_cursor // empty' <<<"$json_first")
start=$SECONDS; text_second=$("$BIN" --cursor "$text_cursor" pageprobe 2>&1); text_cursor2=$(sed -n 's/^cursor: //p' <<<"$text_second"); text_last=$("$BIN" --cursor "$text_cursor2" pageprobe 2>&1)
json_second=$("$BIN" --json --cursor "$json_cursor" pageprobe 2>&1); json_cursor2=$(jq -r '.next_cursor // empty' <<<"$json_second"); json_last=$("$BIN" --json --cursor "$json_cursor2" pageprobe 2>&1)
if grep -q '^10/23 hits' <<<"$text_first" && [[ $(grep -Ec '^[[:xdigit:]]{12}:[0-9]+  ' <<<"$text_first") == 10 ]] && [[ -n $text_cursor && -n $json_cursor ]] && jq -e '.shown==10 and .total==23' <<<"$json_first" >/dev/null && grep -q '^10/23 hits' <<<"$text_second" && [[ -n $text_cursor2 && -n $json_cursor2 ]] && grep -q '^3/23 hits' <<<"$text_last" && ! grep -q '^cursor:' <<<"$text_last" && jq -e '.shown==3 and .total==23 and (has("next_cursor")|not)' <<<"$json_last" >/dev/null; then record 'Default page size and cursor footer/JSON parity' PASS $((SECONDS-start)) '10 + 10 + 3 rows; text and JSON cursors appear only while more remain'; else record 'Default page size and cursor footer/JSON parity' FAIL $((SECONDS-start)) "first=$text_first last=$text_last json_last=$json_last"; fi
bad_query=$("$BIN" --json --cursor "$json_cursor" snapshot 2>&1); bad_query_rc=$?
bad_flags=$("$BIN" --json --limit 5 --cursor "$json_cursor" pageprobe 2>&1); bad_flags_rc=$?
if ((bad_query_rc!=0 && bad_flags_rc!=0)) && grep -q 'cursor does not match this query or flags' <<<"$bad_query" && grep -q 'cursor does not match this query or flags' <<<"$bad_flags"; then record 'Cursor rejects changed query and flags clearly' PASS 0 ''; else record 'Cursor rejects changed query and flags clearly' FAIL 0 "query=$bad_query flags=$bad_flags"; fi

# Grouped session totals/hit counts agree with message-level results and top session.
hits_snapshot=$("$BIN" --json snapshot 2>&1); sessions_snapshot=$("$BIN" --json --sessions snapshot 2>&1)
printf '%s\n' "$hits_snapshot" > "$TMP/snapshot-hits.json"; printf '%s\n' "$sessions_snapshot" > "$TMP/snapshot-sessions.json"
python3 - "$TMP/snapshot-hits.json" "$TMP/snapshot-sessions.json" <<'PY' > "$TMP/sessions-check.txt" 2> "$TMP/sessions-error.txt"
import json,sys
h=json.load(open(sys.argv[1])); s=json.load(open(sys.argv[2]))
assert s['total']==h['total_sessions'] and sum(x['hits'] for x in s['sessions'])==h['total'], f'hit/session totals disagree: {h["total"]}/{h["total_sessions"]} vs {s["total"]}/{sum(x["hits"] for x in s["sessions"])}'
assert s['sessions'] and h['hits'][0]['ref'].split(':',1)[0]==s['sessions'][0]['ref'], 'sessions are not ranked by their best hit'
print(f'{h["total"]} hits across {h["total_sessions"]} sessions; counts sum and best-hit rank agrees')
PY
if [[ -s "$TMP/sessions-check.txt" ]]; then record '--sessions ranking/counts agree with hit list' PASS 0 "$(<"$TMP/sessions-check.txt")"; else record '--sessions ranking/counts agree with hit list' FAIL 0 "$(<"$TMP/sessions-error.txt")"; fi

# show ref exposes context, resume metadata, hit marker, truncation, and --all cursor pages.
show_ref=$(jq -r '.hits[] | select(.ref|endswith(":10")) | .ref' "$TMP/page-full.json")
show_json=$("$BIN" show "$show_ref" --context 2 --query PAGE_HIT_10 --json 2>&1); printf '%s\n' "$show_json" > "$TMP/show-context.json"
show_text=$("$BIN" show "$show_ref" --context 2 2>&1)
python3 - "$TMP/show-context.json" <<'PY' > "$TMP/show-check.txt" 2> "$TMP/show-error.txt"
import json,sys
p=json.load(open(sys.argv[1])); selected=[m for m in p['messages'] if m['hit']]
assert p['shown']==5 and p['total']==5 and len(selected)==1, f'expected selected hit and 2 context messages each side, got {p["shown"]}/{p["total"]}'
assert 'PAGE_HIT_08' in p['messages'][0]['text'] and 'PAGE_HIT_12' in p['messages'][-1]['text'], 'context bounds incorrect'
assert p['resume_cmd']=='claude --resume 66666666-6666-4666-8666-666666666666', p['resume_cmd']
assert p['resume_argv']==['claude','--resume','66666666-6666-4666-8666-666666666666'], p
assert selected[0]['matches'] and all(not m['matches'] for m in p['messages'] if not m['hit']), p
text=selected[0]['text'].encode('utf-16-le')
assert any(text[2*r['location']:2*(r['location']+r['length'])].decode('utf-16-le')=='PAGE_HIT_10' for r in selected[0]['highlights']), selected
assert p['project']=='demo' and p['cwd'].endswith('/work/demo'), f'missing project/cwd: {p["project"]} {p["cwd"]}'
assert len(selected[0]['text'])<=400 and selected[0]['text'].endswith('…'), 'long selected text was not truncated'
print('5-message context, selected hit, resume command, project/cwd, and 400-cell truncation verified')
PY
if [[ -s "$TMP/show-check.txt" ]] && grep -Eq '^> [0-9]{2}:[0-9]{2} user:' <<<"$show_text" && grep -q 'resume: claude --resume 66666666-6666-4666-8666-666666666666' <<<"$show_text"; then record 'show ref context and text hit marker' PASS 0 "$(<"$TMP/show-check.txt")"; else record 'show ref context and text hit marker' FAIL 0 "$(<"$TMP/show-error.txt") $show_text"; fi
show_all=$("$BIN" show "$show_ref" --all --json 2>&1); printf '%s\n' "$show_all" > "$TMP/show-all-first.json"; : > "$TMP/show-all-markers.txt"; show_pages=0
while [[ -n $show_all ]]; do
 printf '%s\n' "$show_all" | jq -r '.messages[].text' | grep -oE 'PAGE_HIT_[0-9]{2}' >> "$TMP/show-all-markers.txt" || :
 show_cursor=$(printf '%s\n' "$show_all" | jq -r '.next_cursor // empty'); show_pages=$((show_pages+1)); [[ -z $show_cursor || $show_pages -ge 10 ]] && break
 show_all=$("$BIN" show "$show_ref" --all --json --cursor "$show_cursor" 2>&1)
done
if python3 - "$TMP/show-all-markers.txt" <<'PY' > "$TMP/show-all-check.txt" 2> "$TMP/show-all-error.txt"
import sys
found=[x.strip() for x in open(sys.argv[1]) if x.strip()]; expected=[f'PAGE_HIT_{i:02}' for i in range(23)]
assert len(found)==23 and len(set(found))==23 and set(found)==set(expected), f'--all pages skipped/duplicated messages: {len(found)} unique={len(set(found))}'
print('all 23 messages returned once across show --all pages')
PY
then record 'show --all paginates the whole session' PASS "$show_pages" "$(<"$TMP/show-all-check.txt")"; else record 'show --all paginates the whole session' FAIL "$show_pages" "$(<"$TMP/show-all-error.txt")"; fi

# --help is immediate and must not create/open the index or run a search.
export KIOKU_INDEX="$H/index/help-must-not-exist.db"; rm -f "$KIOKU_INDEX"
help_out=$("$BIN" --help 2>&1); help_rc=$?
if ((help_rc==0)) && grep -q '^Usage:' <<<"$help_out" && grep -q 'kioku show <ref|session-id>' <<<"$help_out" && [[ ! -e $KIOKU_INDEX ]]; then record 'kioku --help exits with usage before search/index' PASS 0 ''; else record 'kioku --help exits with usage before search/index' FAIL 0 "$help_out index_exists=$([[ -e $KIOKU_INDEX ]] && echo yes || echo no)"; fi
export KIOKU_INDEX="$H/index/index.db"

# Query correctness cases from SPEC's Expected behavior plus languages/filters.
check_query(){ local q=$1 mode=$2 want=${3:-}; local t=$SECONDS out rc; out=$("$BIN" --json "$q" 2>&1); rc=$?; local ok=1
 if ((rc)); then ok=0; elif [[ $mode == empty ]]; then jq -e '.total==0 and .shown==0 and (.hits|length)==0' <<<"$out" >/dev/null 2>&1 || ok=0; elif ! grep -Fq "$want" <<<"$out"; then ok=0; fi
 if ((ok)); then record "query: $q ($mode)" PASS $((SECONDS-t)) ''; else record "query: $q ($mode)" FAIL $((SECONDS-t)) "expected $mode $want; got: $out"; failbug "Query $q" "HOME=$HOME KIOKU_INDEX=$KIOKU_INDEX ./kioku --json '$q'" "${mode} ${want}" "$out"; fi
}
check_query snapshot contains SNAPSHOT
check_query recoverytoken contains recoverytoken
check_query resum contains resuming
check_query sume empty
check_query '"tea black"' empty
check_query naive contains 'naïve'
for q in json_extract low-water S3 cjk.c; do check_query "$q" contains "$q"; done
check_query 魚池 contains 魚池
check_query 南投魚池 empty
check_query 茶 contains 茶
check_query 日本語 contains 日本語
check_query 한국어 contains 한국어
check_query --flag empty
start=$SECONDS; version=$("$BIN" --version 2>&1); rc=$?; if ((rc==0)) && [[ -n $version ]]; then record 'CLI --version' PASS $((SECONDS-start)) "$version"; else record 'CLI --version' FAIL $((SECONDS-start)) "$version"; fi
start=$SECONDS; recent=$("$BIN" --json 2>&1); rc=$?; if ((rc==0)) && jq -e '.shown>0 and (.hits|length)==.shown and all(.hits[]; has("ref") and has("harness") and has("snippet"))' <<<"$recent" >/dev/null 2>&1; then record 'Empty query lists recent messages as compact JSON' PASS $((SECONDS-start)) "$(jq -r '.shown' <<<"$recent") rows"; else record 'Empty query lists recent messages as compact JSON' FAIL $((SECONDS-start)) "$recent"; fi
start=$SECONDS; malformed=$("$BIN" --json '"' 2>&1); rc=$?; if ((rc==0)) && jq -e '.total==0 and (.hits|length)==0' <<<"$malformed" >/dev/null 2>&1; then record 'Malformed quote query does not crash' PASS $((SECONDS-start)) ''; else record 'Malformed quote query does not crash' FAIL $((SECONDS-start)) "$malformed"; fi

start=$SECONDS; out=$("$BIN" --json snapshot 2>&1); rc=$?; printf '%s\n' "$out" > "$SCREENS/json-snapshot.jsonl"
if ((rc==0)) && jq -e 'has("shown") and has("total") and has("total_sessions") and (.hits|length)==.shown and all(.hits[]; ([keys[]]|sort)==(["ref","harness","project","age","role","snippet","highlights"]|sort))' <<<"$out" >/dev/null; then record 'Compact JSON search-page schema' PASS $((SECONDS-start)) "$(jq -r '.shown' <<<"$out") hits"; else record 'Compact JSON search-page schema' FAIL $((SECONDS-start)) "$out"; fi
for h in claude codex pi; do start=$SECONDS; out=$("$BIN" --json --harness "$h" snapshot 2>&1); rc=$?; if ((rc==0)) && [[ -n $out ]] && ! grep -Ev '"harness":"'"$h"'"' <<<"$out" | grep -q .; then record "JSON harness filter $h" PASS $((SECONDS-start)) ''; else record "JSON harness filter $h" FAIL $((SECONDS-start)) "$out"; fi; done


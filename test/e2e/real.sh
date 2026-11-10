# Independent read-only real-store count versus app indexing with an isolated DB.
if [[ ${KIOKU_E2E_REAL:-0} == 1 ]]; then
REALHOME="$REALHOME_DEFAULT"; REALTMP="$TMP/real"; mkdir -p "$REALTMP" "$TEST/out"; export REALHOME REALTMP
python3 - <<'PY' > "$REALTMP/independent.txt"
import glob,json,os
home=os.environ['REALHOME']
patterns={'claude':home+'/.claude/projects/**/*.jsonl','codex':home+'/.codex/sessions/**/*.jsonl','pi':home+'/.pi/agent/sessions/**/*.jsonl'}
for h,pat in patterns.items():
 files=glob.glob(pat,recursive=True); n=0
 for p in files:
  try:
   for line in open(p,encoding='utf8'):
    try:d=json.loads(line)
    except:continue
    m=d.get('message') or {}; payload=d.get('payload') or {}
    if h=='claude' and d.get('type') in ('user','assistant') and not d.get('isMeta') and not d.get('isSidechain'):
     c=m.get('content',''); n+=bool(c) if isinstance(c,str) else any(x.get('type')=='text' and x.get('text') for x in c if isinstance(x,dict))
    elif h=='codex' and d.get('type')=='response_item' and payload.get('type')=='message' and payload.get('role') in ('user','assistant'):
     n+=sum(bool(x.get('text')) for x in payload.get('content',[]) if isinstance(x,dict) and x.get('type') in ('input_text','output_text'))
    elif h=='pi' and d.get('type')=='message' and m.get('role') in ('user','assistant'):
     c=m.get('content',[]); n+=bool(c) if isinstance(c,str) else sum(bool(x.get('text')) for x in c if isinstance(x,dict) and x.get('type')=='text')
  except (OSError,UnicodeError): pass
 print(h,len(files),n)
PY
export HOME="$REALHOME" KIOKU_INDEX="$REALTMP/index.db"
start=$SECONDS; realout=$("$BIN" index --rebuild 2>&1); rc=$?; python3 - "$REALTMP/independent.txt" "$REALTMP/app.txt" <<'PY'
import sqlite3,sys
rows=sqlite3.connect(sys.argv[2].replace('app.txt','index.db')).execute('select harness,count(distinct uid),sum((select count(*) from messages m where m.session_uid=s.uid)) from sessions s group by harness').fetchall()
open(sys.argv[2],'w').write('\n'.join('%s %s %s'%r for r in rows)+'\n')
PY
{ printf 'independent: harness total_jsonl raw_user_assistant_text_blocks\n'; cat "$REALTMP/independent.txt"; printf 'indexed: harness sessions message_rows\n'; cat "$REALTMP/app.txt"; } > "$TEST/out/real-store-counts.txt"
# Raw user/assistant text blocks are a conservative lower bound: indexed rows also include tools.
if ((rc==0)) && python3 - "$REALTMP/independent.txt" "$REALTMP/app.txt" <<'PY'
import sys
ind={x.split()[0]:int(x.split()[2]) for x in open(sys.argv[1])}; app={x.split()[0]:int(x.split()[2]) for x in open(sys.argv[2])}
for h,n in ind.items():
 a=app.get(h,0)
 if n and a<n: print(f'{h}: indexed={a} below independent user/assistant lines={n}'); sys.exit(1)
 if n and not a: print(f'{h}: independent={n}, indexed=0'); sys.exit(1)
PY
then record 'Real stores: read-only independent lower-bound sanity' PASS $((SECONDS-start)) "$(tr '\n' ';' < "$TEST/out/real-store-counts.txt"); lower-bound only: indexed counts include tool rows and split content blocks"; else record 'Real stores: read-only independent lower-bound sanity' FAIL $((SECONDS-start)) "$realout; $(tr '\n' ';' < "$TEST/out/real-store-counts.txt")"; failbug 'Real store message undercount' 'KIOKU_INDEX=<temp>/index.db ./kioku index --rebuild; compare to test/out/real-store-counts.txt' 'indexed rows >= independent user/assistant text blocks' "$realout; see test/out/real-store-counts.txt"; fi
else
  skip 'Real stores: read-only independent lower-bound sanity' 'Set KIOKU_E2E_REAL=1 to opt in to reading this machine’s ~/.claude, ~/.codex, and ~/.pi stores.'
fi


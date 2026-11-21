#!/bin/sh
# Launches the real desktop app on a synthetic HOME four times (full, loading, empty, error) and checks what the window drew.
# Artifact: test/out/e2e-{full,loading,empty,error}.json (the app writes each itself, then exits).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out="$here/out"; mkdir -p "$out"
work=$(mktemp -d "${TMPDIR:-/tmp}/kioku-desktop-e2e.XXXXXX")
(cd "$root" && go build -tags sqlite_fts5 -o "$work/kioku" .)
sh "$root/test/fixture.sh" "$work/home" >/dev/null
HOME="$work/home" "$work/kioku" index >/dev/null 2>&1
(cd "$here/../src-tauri" && cargo build -q)
printf '#!/bin/sh\nsleep 2\nexec "%s" "$@"\n' "$work/kioku" > "$work/slow"; chmod +x "$work/slow"
rm -f "$out"/e2e-*.json "$out/e2e-report.json"
run() { # name query bin
  rm -f "$out/e2e-$1.json"
  HOME="$work/home" KIOKU_BIN="$3" KIOKU_E2E_OUT="$out/e2e-$1.json" KIOKU_E2E_QUERY="$2" \
    "$here/../src-tauri/target/debug/kioku-desktop" &
  pid=$!
  i=0; while [ ! -s "$out/e2e-$1.json" ] && [ $i -lt 40 ]; do sleep 1; i=$((i+1)); done
  kill $pid 2>/dev/null || true
  [ -s "$out/e2e-$1.json" ] || { echo "FAIL: no $1 report in 40s" >&2; exit 1; }
}
run full 紅茶 "$work/kioku"
run loading 紅茶 "$work/slow"
run empty zzqxnomatch "$work/kioku"
run error 紅茶 "$work/missing"
python3 - "$out" <<'PY'
import json,sys,re,math
def rgb(c):
    m=re.match(r'rgba?\(([^)]*)\)',c)
    if m: return [(float(x)/255) for x in re.split(r'[ ,/]+',m.group(1).strip())[:3]],False
    L,C,h=[float(x) for x in re.match(r'oklch\(([^ ]+) ([^ ]+) ([^ )]+)',c).groups()]; h=math.radians(h)
    a,b=C*math.cos(h),C*math.sin(h)
    l,m_,s=(L+.3963377774*a+.2158037573*b)**3,(L-.1055613458*a-.0638541728*b)**3,(L-.0894841775*a-1.291485548*b)**3
    return [max(0,min(1,v)) for v in (4.0767416621*l-3.3077115913*m_+.2309699292*s,-1.2684380046*l+2.6097574011*m_-.3413193965*s,-.0041960863*l-.7034186147*m_+1.707614701*s)],True
def lum(c):
    v,lin=rgb(c)
    if not lin: v=[x/12.92 if x<=.04045 else ((x+.055)/1.055)**2.4 for x in v]
    return .2126*v[0]+.7152*v[1]+.0722*v[2]
def ratio(a,b):
    x,y=sorted([lum(a),lum(b)],reverse=True); return (x+.05)/(y+.05)
R={n:json.load(open(f"{sys.argv[1]}/e2e-{n}.json")) for n in("full","loading","empty","error")}
for n,r in R.items():
    for k,v in r["contrast"].items():
        if v: assert ratio(*v)>=4.5, (n,k,v,ratio(*v))
    assert r["hidden"]==[1,1], (n,r["hidden"])
r=R["full"]; print(json.dumps(r,ensure_ascii=False,indent=1))
assert r["state"]=="full", r
assert r["rows"]==3, r
assert r["tapesDrawn"]==3 and r["litTicks"]>=3, r
assert r["count"]=="5 messages in 3 sessions", r
assert r["marks"]>=1 and r["mapTicks"]>=5, r
assert r["command"] and "resume" in r["command"], r
assert len(r["when"])==3 and all(re.match(r"^\d{1,2} [A-Z][a-z]{2} \d{2}:\d{2} [+-]\d{2}",w) for w in r["when"]), r["when"]
print("PASS full")
r=R["loading"]; e=r["early"]
assert e["state"]=="loading" and e["busy"] and e["skeletons"]==5, e
assert r["state"]=="full" and not r["busy"], r
print("PASS loading")
r=R["empty"]
assert r["state"]=="empty" and r["rows"]==0 and r["alert"]==0, r
assert r["readerNone"] and not r["sessionShown"], r
print("PASS empty")
r=R["error"]
assert r["state"]=="error" and r["alert"]==1 and r["retry"] and r["readerNone"] and r["errorText"], r
print("PASS error")
PY

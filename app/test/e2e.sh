#!/bin/bash
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
TEST="$ROOT/app/test"; SCREENS="$TEST/screens"; REPORT="$TEST/e2e-report.md"
rm -rf "$SCREENS"
mkdir -p "$SCREENS"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/kioku-mac-e2e.XXXXXX")
TMP=$(cd "$TMP" && pwd -P)
BUILD="$TMP/build"; RESULT="$TEST/Result.xcresult"
PASS=0; FAIL=0; BLOCKED=0
printf '# App E2E report\n\nRun: %s\n\n| Check | Result | Details |\n|---|---:|---|\n' "$(date -u '+%Y-%m-%d %H:%M:%S UTC')" > "$REPORT"
record() { printf '| %s | %s | %s |\n' "$1" "$2" "$3" >> "$REPORT"; }
cleanup() {
 if [[ -f "$TMP/preferences.plist" ]]; then defaults import com.kioku.mac "$TMP/preferences.plist" >/dev/null 2>&1; else defaults delete com.kioku.mac >/dev/null 2>&1 || :; fi
 printf '\n**Summary:** %s PASS, %s FAIL, %s BLOCKED.\n' "$PASS" "$FAIL" "$BLOCKED" >> "$REPORT"
 printf '%s PASS / %s FAIL / %s BLOCKED — %s\n' "$PASS" "$FAIL" "$BLOCKED" "$REPORT"
 # Keep fixture/build/logs for diagnosis; never committed.
 printf '\nFixture and build: `%s`\n' "$TMP" >> "$REPORT"
}
trap cleanup EXIT
defaults export com.kioku.mac "$TMP/preferences.plist" >/dev/null 2>&1 || rm -f "$TMP/preferences.plist"
if ! "$ROOT/test/fixture.sh" "$TMP/home" > "$SCREENS/fixture.log" 2>&1; then FAIL=$((FAIL+1)); record Fixture FAIL fixture.log; exit 1; fi
HOME_FIXTURE="$TMP/home"
# All cwd/launcher data is synthetic. Make the demo projects real directories.
if ! python3 - "$HOME_FIXTURE" <<'PY'
import json,os,pathlib,sys,plistlib
home=pathlib.Path(sys.argv[1]); (home/'work/demo').mkdir(parents=True)
for path in home.rglob('*.jsonl'):
 path.write_text(path.read_text().replace('/work/demo',str(home/'work/demo')))
rows=[
 {'type':'user','timestamp':'2026-09-27T12:00:00Z','cwd':str(home/'work/demo'),'sessionId':'rank-session','message':{'role':'user','content':'rankprobe conversation first'}},
 {'type':'assistant','timestamp':'2026-09-27T12:00:01Z','cwd':str(home/'work/demo'),'sessionId':'rank-session','message':{'role':'assistant','content':[{'type':'text','text':'rankprobe conversation second'},{'type':'tool_use','id':'rank-tool','name':'bash','input':{'command':'rankprobe rankprobe rankprobe'}}]}},
 {'type':'user','timestamp':'2026-09-27T12:00:02Z','cwd':str(home/'work/demo'),'sessionId':'rank-session','message':{'role':'user','content':'rankprobe conversation third'}}
]
(home/'.claude/projects/-work-demo/rank.jsonl').write_text('\n'.join(map(json.dumps,rows))+'\n')
# Exercise real two-line layout with fallback CJK fonts and unbroken Latin.
stress=[{'type':role,'timestamp':f'2026-09-27T14:00:0{i}Z','cwd':str(home/'work/demo'),'sessionId':'layout-session','message':{'role':role,'content':('layoutprobe '+text) if role=='user' else [{'type':'text','text':'layoutprobe '+text}]}} for i,(role,text) in enumerate([
 ('user', '漢字排版測試邊界應保留空間' * 24 + '\n' + 'UnbrokenLatinToken' * 24),
 ('assistant', 'VeryLongUnbrokenLatinToken' * 24 + '漢字字形尾端省略測試' * 16)
])]
(home/'.claude/projects/-work-demo/layout.jsonl').write_text('\n'.join(map(json.dumps,stress))+'\n')
arrow=[{'type':'user','timestamp':f'2026-09-27T13:00:{i:02d}Z','cwd':str(home/'work/demo'),'sessionId':'arrow-session','message':{'role':'user','content':f'arrowprobe {i:02d}'}} for i in range(35)]
arrow.append({'type':'user','timestamp':'2026-09-27T13:01:00Z','cwd':str(home/'work/demo'),'sessionId':'arrow-session','message':{'role':'user','content':'紅色的筆記，只有單字。'}})
(home/'.claude/projects/-work-demo/arrows.jsonl').write_text('\n'.join(map(json.dumps,arrow))+'\n')
for name,id in [('Default Terminal.app','com.kioku.fixture.terminal'),('Default Editor.app','com.kioku.fixture.editor'),('Alternate Terminal.app','com.kioku.fixture.alternate-terminal'),('Ghostty Fixture.app','com.mitchellh.ghostty'),('Alternate Editor.app','com.kioku.fixture.alternate-editor')]:
 app=home/name/'Contents'; app.mkdir(parents=True)
 with (app/'Info.plist').open('wb') as f: plistlib.dump({'CFBundleIdentifier':id,'CFBundleName':name.removesuffix('.app'),'CFBundlePackageType':'APPL'},f)
bin=home/'bin'; bin.mkdir()
terminal='''#!/usr/bin/python3
import json,os,pathlib,shlex,subprocess,sys
home=pathlib.Path(os.environ['KIOKU_TEST_RECORDS']); home.mkdir(parents=True,exist_ok=True); args=sys.argv[1:]
if args[0]=='-na':
 assert args[2]=='--args' and args[3]=='--working-directory='+os.getcwd() and args[4]=='-e',args
 argv=args[5:]
 (home/'terminal.json').write_text(json.dumps({'argv':args,'cwd':os.getcwd(),'resume_argv':argv}))
 subprocess.run(argv,check=True)
else:
 assert args[0]=='-a',args
 script=pathlib.Path(args[2]); text=script.read_text()
 argv=next(shlex.split(line)[1:] for line in text.splitlines() if line.startswith('exec '))
 (home/'terminal.json').write_text(json.dumps({'argv':args,'cwd':os.getcwd(),'resume_argv':argv,'script':str(script)}))
 subprocess.run(['/bin/zsh',str(script)],check=True)
'''
editor='''#!/usr/bin/python3
import json,os,pathlib,sys
home=pathlib.Path(os.environ['KIOKU_TEST_RECORDS']); home.mkdir(parents=True,exist_ok=True)
(home/'editor.json').write_text(json.dumps({'argv':sys.argv[1:],'cwd':os.getcwd()}))
'''
harness='''#!/usr/bin/python3
import json,os,pathlib,sys
home=pathlib.Path(os.environ['KIOKU_TEST_RECORDS']); home.mkdir(parents=True,exist_ok=True)
(home/'session-launch.json').write_text(json.dumps({'argv':[pathlib.Path(sys.argv[0]).name]+sys.argv[1:],'cwd':os.getcwd()}))
'''
gate='''#!/bin/bash
if [[ "$1" == show && "$4" == layoutprobe ]]; then
 : > "$KIOKU_TEST_RECORDS/show-pending"
 IFS= read -r -t 15 signal < "$KIOKU_TEST_RECORDS/show-gate" || exit 70
fi
exec "$(dirname "$0")/real-kioku" "$@"
'''
for name,text in [('engine-gate',gate),('terminal-stub',terminal),('editor-stub',editor),('claude',harness),('codex',harness),('pi',harness)]:
 p=bin/name;p.write_text(text);p.chmod(0o700)
PY
then FAIL=$((FAIL+1)); record 'Fixture launcher setup' FAIL fixture.log; exit 1; fi
PASS=$((PASS+1)); record 'Isolated synthetic HOME and launcher stubs' PASS fixture.log
rm -rf "$RESULT"
ARGS=(-project "$ROOT/app/Kioku.xcodeproj" -scheme Kioku -configuration Debug -destination "platform=macOS,arch=$(uname -m)" -derivedDataPath "$BUILD" ONLY_ACTIVE_ARCH=YES "KIOKU_E2E_HOME=$HOME_FIXTURE")
if xcodebuild "${ARGS[@]}" build-for-testing > "$SCREENS/build.log" 2>&1; then
 PASS=$((PASS+1)); record 'App and XCUITest runner build (macOS 26 SDK, target 14)' PASS build.log
else FAIL=$((FAIL+1)); record Build FAIL build.log; tail -40 "$SCREENS/build.log"; exit 1; fi
cp "$BUILD/Build/Products/Debug/Kioku.app/Contents/Resources/kioku" "$HOME_FIXTURE/bin/real-kioku"
if python3 "$TEST/sqlite-extension-lock.py" "$HOME_FIXTURE/bin/real-kioku" > "$SCREENS/sqlite-extension-lock.log" 2>&1; then
 PASS=$((PASS+1)); record 'CJK initialization under a database writer lock (CLI E2E)' PASS sqlite-extension-lock.log
else FAIL=$((FAIL+1)); record 'CJK initialization under a database writer lock (CLI E2E)' FAIL sqlite-extension-lock.log; exit 1; fi
TEST_RUNNER_KIOKU_E2E_HOME="$HOME_FIXTURE" xcodebuild "${ARGS[@]}" -resultBundlePath "$RESULT" test-without-building > "$SCREENS/ui-tests.log" 2>&1
RC=$?
if [[ -d "$RESULT" ]]; then
 xcrun xcresulttool get test-results summary --path "$RESULT" --format json > "$SCREENS/summary.json" 2> "$SCREENS/result-error.log" || :
 xcrun xcresulttool export attachments --path "$RESULT" --output-path "$SCREENS" > "$SCREENS/attachments.log" 2>&1 || :
 # Preserve XCTest originals and also export stable, readable screenshot names.
 python3 - "$SCREENS" <<'PY'
import json,pathlib,re,shutil,sys
directory=pathlib.Path(sys.argv[1])
for case in json.loads((directory/'manifest.json').read_text()):
 for attachment in case.get('attachments',[]):
  source=attachment.get('exportedFileName','')
  name=attachment.get('suggestedHumanReadableName','')
  if source.endswith('.png'):
   name=re.sub(r'_\d+_[A-F0-9-]+\.png$', '.png', name)
   if name and pathlib.Path(name).name == name:
    shutil.copyfile(directory/source,directory/name)
PY
fi
# Count only XCTest's actual test-case results, never build success as UI success.
read -r UI_PASS UI_FAIL < <(python3 - "$SCREENS/ui-tests.log" <<'PY'
import re,sys
text=open(sys.argv[1]).read()
print(len(re.findall(r"Test Case '-\[.*?\]' passed",text)),len(re.findall(r"Test Case '-\[.*?\]' failed",text)))
PY
)
PASS=$((PASS+UI_PASS)); FAIL=$((FAIL+UI_FAIL))
python3 - "$SCREENS/ui-tests.log" "$REPORT" <<'PY'
import re,sys
text=open(sys.argv[1]).read()
with open(sys.argv[2],'a') as report:
 for name,result in re.findall(r"Test Case '-\[(.*?)\]' (passed|failed)",text):
  report.write('| '+name+' | '+result.upper()+' | ui-tests.log / screenshot attachments |\n')
PY
if (( (RC != 0 && UI_FAIL == 0) || UI_PASS + UI_FAIL == 0 )); then
 BLOCKED=$((BLOCKED+1))
 record 'XCUITest execution' BLOCKED 'See ui-tests.log for the exact signing/automation/runner error; no UI pass claimed.'
 grep -E 'error:|Error|denied|permission|not authorized|Failed' "$SCREENS/ui-tests.log" | tail -20 >> "$REPORT" || :
fi
if ! mdfind 'kMDItemCFBundleIdentifier == com.mitchellh.ghostty' | grep -q .; then
 printf '\nGhostty is not installed. Real Ghostty launching was NOT exercised. Terminal/editor launches use recorder stubs; the .command executes a fixture harness stub to verify argv and cwd.\n' >> "$REPORT"
fi
(( FAIL == 0 && BLOCKED == 0 )) && exit 0
exit 1

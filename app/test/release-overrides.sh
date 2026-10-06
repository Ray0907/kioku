#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${1:-$(mktemp -d /tmp/kioku-release-overrides.XXXXXX)}
mkdir -p "$OUT"
export SDKROOT=$(xcrun --sdk macosx --show-sdk-path)
# Optimized Swift can encode short strings in instructions. Also inspect Release
# without optimization: the same compilation conditions, with inspectable literals.
for variant in Debug Release Release-unoptimized; do
    configuration=${variant%%-*}
    optimization=-Onone
    if [[ "$variant" == Release ]]; then optimization=-O; fi
    xcodebuild -project "$ROOT/app/Kioku.xcodeproj" -scheme Kioku -configuration "$configuration" \
        -destination "platform=macOS,arch=$(uname -m)" -derivedDataPath "$OUT/$variant-build" \
        ONLY_ACTIVE_ARCH=YES ENABLE_DEBUG_DYLIB=NO "SWIFT_OPTIMIZATION_LEVEL=$optimization" build > "$OUT/$variant-build.log" 2>&1
    xcrun strings -a "$OUT/$variant-build/Build/Products/$configuration/Kioku.app/Contents/MacOS/Kioku" > "$OUT/$variant-strings.txt"
done
python3 - "$OUT" <<'PY'
import pathlib,sys
out=pathlib.Path(sys.argv[1])
debug=(out/'Debug-strings.txt').read_text().splitlines()
keys=['KIOKU_ENGINE','KIOKU_TERMINAL_LAUNCHER','KIOKU_EDITOR_LAUNCHER',
      'KIOKU_SYSTEM_TERMINAL','KIOKU_SYSTEM_EDITOR','KIOKU_EDITOR',
      'KIOKU_REDUCE_MOTION','KIOKU_REDUCE_TRANSPARENCY','KIOKU_INCREASE_CONTRAST','KIOKU_APPEARANCE']
missing=[key for key in keys if key not in debug]
if missing: sys.exit('Debug positive control missing: '+', '.join(missing))
for variant in ['Release','Release-unoptimized']:
    leaked=[line for line in (out/f'{variant}-strings.txt').read_text().splitlines() if 'KIOKU_' in line]
    if leaked: sys.exit(variant+' contains test overrides: '+', '.join(leaked))
print('PASS: Debug retains all 10 override keys; optimized and unoptimized Release contain no KIOKU_* override keys')
print('Proof artifacts: '+str(out))
PY

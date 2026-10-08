#!/usr/bin/env bash
# Record assets/demo.gif from the real binary on a synthetic demo home.
# Needs: vhs (brew install vhs), which brings ttyd; ffmpeg.
# Usage: demo/record.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd); cd "$ROOT"
command -v vhs >/dev/null || { echo "vhs not found: brew install vhs" >&2; exit 1; }
WORK=$(mktemp -d "${TMPDIR:-/tmp}/kioku-rec.XXXXXX"); trap 'rm -rf "$WORK"' EXIT
make build >/dev/null
mkdir -p "$WORK/bin" && ln -s "$ROOT/kioku" "$WORK/bin/kioku"
python3 demo/make_demo_home.py "$WORK/home" >/dev/null
HOME="$WORK/home" KIOKU_INDEX="$WORK/index.db" ./kioku index >/dev/null
mkdir -p assets
sed -e "s|@HOME@|$WORK/home|" -e "s|@INDEX@|$WORK/index.db|" -e "s|@BIN@|$WORK/bin|" demo/demo.tape > "$WORK/demo.tape"
vhs "$WORK/demo.tape"
echo "assets/demo.gif"

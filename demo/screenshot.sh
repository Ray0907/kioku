#!/usr/bin/env bash
# Render README screenshots from the real binary running on a synthetic demo home.
# Usage: demo/screenshot.sh   (writes assets/*.png)
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd); cd "$ROOT"
CHROME=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
COLS=${COLS:-128}; ROWS=${ROWS:-30}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/kioku-shot.XXXXXX"); trap 'rm -rf "$WORK"; tmux kill-session -t hsshot 2>/dev/null || true' EXIT
make build >/dev/null
HOMEDIR="$WORK/home"; python3 demo/make_demo_home.py "$HOMEDIR" >/dev/null
export HOME="$HOMEDIR" KIOKU_INDEX="$WORK/index.db" KIOKU_THEME=dark KIOKU_EDITOR=zed
./kioku index >/dev/null
mkdir -p assets

shot() { # name query [keys...]
  rm -f "$ROOT/assets/$1.png"
  local name=$1 query=$2; shift 2
  tmux kill-session -t hsshot 2>/dev/null || true
  tmux new-session -d -s hsshot -x "$COLS" -y "$ROWS" \
    "HOME='$HOME' KIOKU_INDEX='$KIOKU_INDEX' KIOKU_THEME=dark KIOKU_EDITOR=zed TERM=xterm-256color COLORTERM=truecolor '$ROOT/kioku' '$query'"
  sleep 1.5
  for k in "$@"; do tmux send-keys -t hsshot "$k"; sleep 0.4; done
  sleep 0.8
  tmux capture-pane -e -p -t hsshot > "$WORK/$name.ansi"
  python3 demo/ansi2html.py "$WORK/$name.ansi" "$WORK/$name.html"
  "$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
    --window-size=1240,810 --screenshot="$ROOT/assets/$name.png" "file://$WORK/$name.html" >/dev/null 2>&1 || true  # Chrome exits non-zero on harmless macOS warnings
  [[ -s "$ROOT/assets/$name.png" ]] || { echo "screenshot failed: $name" >&2; exit 1; }
  echo "assets/$name.png"
}

shot kioku checkout Escape Down Down
shot kioku-cjk 結帳 Escape h P a y Enter
shot kioku-help checkout Escape Down '?'

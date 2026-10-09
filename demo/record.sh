#!/usr/bin/env bash
# Record assets/demo.gif by driving the real binary in tmux on a synthetic demo home.
# Each step is captured, rendered with the same HTML/Chrome path as demo/screenshot.sh
# (so the GIF matches the README screenshots), then joined with ffmpeg.
# Needs: tmux, ffmpeg, Google Chrome. Usage: demo/record.sh
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd); cd "$ROOT"
CHROME=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
COLS=${COLS:-128}; ROWS=${ROWS:-30}
SESSION=hsrec
WORK=$(mktemp -d "${TMPDIR:-/tmp}/kioku-rec.XXXXXX"); trap 'rm -rf "$WORK"; tmux kill-session -t $SESSION 2>/dev/null || true' EXIT
make build >/dev/null
HOMEDIR="$WORK/home"; python3 demo/make_demo_home.py "$HOMEDIR" >/dev/null
export HOME="$HOMEDIR" KIOKU_INDEX="$WORK/index.db" KIOKU_THEME=dark KIOKU_EDITOR=zed
./kioku index >/dev/null
mkdir -p assets "$WORK/frames"

tmux kill-session -t $SESSION 2>/dev/null || true
tmux new-session -d -s $SESSION -x "$COLS" -y "$ROWS" \
  "HOME='$HOME' KIOKU_INDEX='$KIOKU_INDEX' KIOKU_THEME=dark KIOKU_EDITOR=zed TERM=xterm-256color COLORTERM=truecolor '$ROOT/kioku'"
sleep 1.5

n=0; : > "$WORK/list.txt"
frame() { # hold-ms: capture the pane, render it, and note how long to show it
  local hold=$1 id; id=$(printf '%03d' "$n"); n=$((n + 1))
  tmux capture-pane -e -p -t $SESSION > "$WORK/frames/$id.ansi"
  python3 demo/ansi2html.py "$WORK/frames/$id.ansi" "$WORK/frames/$id.html"
  "$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
    --window-size=1240,810 --screenshot="$WORK/frames/$id.png" "file://$WORK/frames/$id.html" >/dev/null 2>&1 || true
  [[ -s "$WORK/frames/$id.png" ]] || { echo "frame failed: $id" >&2; exit 1; }
  printf "file '%s'\nduration %s\n" "$WORK/frames/$id.png" "$(awk "BEGIN{print $hold/1000}")" >> "$WORK/list.txt"
}
keys() { tmux send-keys -t $SESSION "$@"; sleep 0.35; }
typed() { # type text one character at a time, one frame each
  local s=$1 i; for ((i = 0; i < ${#s}; i++)); do tmux send-keys -t $SESSION -l "${s:i:1}"; sleep 0.3; frame 160; done
}

frame 900
typed "checkout"; frame 700
keys Escape; frame 700           # focus the results
keys Down; frame 800
keys Down; frame 900
keys n; frame 1000               # next hit in the transcript
keys n; frame 1000
keys v; frame 1500               # full transcript
keys v; frame 700
keys /; keys C-u; frame 400
typed "結帳"; frame 1100
keys Escape; frame 600
keys h; typed "Pay"; keys Enter; frame 1800
keys '?'; frame 2600
cp "$WORK/frames/$(printf '%03d' $((n - 1))).png" "$WORK/last.png"
printf "file '%s'\n" "$WORK/last.png" >> "$WORK/list.txt"   # concat needs the last frame listed once more

ffmpeg -v error -y -f concat -safe 0 -i "$WORK/list.txt" \
  -vf "scale=1240:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=none" \
  -loop 0 assets/demo.gif
echo "assets/demo.gif"

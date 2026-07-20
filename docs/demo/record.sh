#!/bin/sh
# Re-record docs/demo.gif. Requires: asciinema (>= 3), agg, expect, go.
#   sh docs/demo/record.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

mkdir -p "$work/bin" "$work/state"
go build -o "$work/bin/todomd" "$repo"
cp "$repo/TODO.md" "$work/TODO.md"

cd "$work"
env DEMO_DIR="$here" \
    XDG_STATE_HOME="$work/state" \
    GLAMOUR_STYLE=dark \
    TERM=tmux-256color COLORTERM=truecolor COLORFGBG="15;0" \
    PATH="$work/bin:$PATH" \
  asciinema rec --headless --window-size 100x28 \
    -c "sh $here/driver.sh" -q --overwrite "$work/demo.cast"

agg "$work/demo.cast" "$repo/docs/demo.gif" \
  --font-size 15 --theme dracula --fps-cap 20
echo "wrote $repo/docs/demo.gif"

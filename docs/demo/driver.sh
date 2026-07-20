#!/bin/sh
# Runs inside the asciinema recording session: fake-types a few CLI commands,
# then hands the TUI over to the expect script. Expects `todomd` on PATH and
# cwd to contain the demo TODO.md.
type_cmd() {
  printf '\033[1;32m❯\033[0m '
  s="$1"
  i=0
  while [ $i -lt ${#s} ]; do
    printf '%s' "$(printf '%s' "$s" | cut -c$((i+1)))"
    sleep 0.028
    i=$((i+1))
  done
  sleep 0.25
  printf '\n'
}
sleep 0.6
type_cmd 'todomd list'
todomd list
sleep 1.6
type_cmd 'todomd add "Record a demo gif" --tag docs'
todomd add "Record a demo gif" --tag docs
sleep 1.2
type_cmd 'todomd comment y96v --author ai "Working on it - will open a PR shortly."'
todomd comment y96v --author ai "Working on it - will open a PR shortly."
sleep 1.4
type_cmd 'todomd'
# `script` gives expect a pty so its relay of the TUI output is unbuffered.
case "$(uname -s)" in
  Darwin) script -q /dev/null expect "$DEMO_DIR/tui.exp" ;;
  *)      script -q -c "expect $DEMO_DIR/tui.exp" /dev/null ;;
esac

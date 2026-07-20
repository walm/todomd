# Re-recording the demo gif

`docs/demo.gif` is generated, not hand-recorded. To refresh it after a
feature change:

```sh
brew install asciinema agg   # once; expect ships with macOS
sh docs/demo/record.sh
```

That builds the current binary, records the session headless with asciinema
against a scratch copy of the repo's `TODO.md`, and renders the gif with agg
(100×28 terminal, dracula theme). Nothing outside the temp dir and
`docs/demo.gif` is touched.

## Changing what the demo shows

- **CLI part**: edit `driver.sh` — `type_cmd` fake-types a command, then run
  it for real.
- **TUI part**: edit `tui.exp` — `send` presses keys, `pause` waits. Keep
  the pacing generous (viewers read slower than scripts type).

## Hard-won gotchas — read before debugging

These cost real time to discover; don't rediscover them:

1. **expect only reads (and relays) spawned output during `expect`
   commands.** A plain `sleep` between `send`s buffers the whole TUI session
   and dumps it at the end — the recording looks empty until the last frame.
   That's why `pause` polls `expect -timeout 0` in a loop instead of
   sleeping.
2. **Poll fast (10ms).** Polling at 100ms slices bubbletea's repaints into
   separate asciinema events, which agg renders as torn half-painted frames
   (visible flicker). 10ms polling plus agg's `--fps-cap 20` coalesces each
   repaint into one clean frame.
3. **`TERM=tmux-256color` is required.** Importing bubbletea queries the
   terminal's background color (OSC 11) at startup; recording ptys never
   answer, so every `todomd` invocation would stall for the 5s termenv
   timeout. termenv skips the query when `TERM` starts with `tmux`, using
   `COLORFGBG` instead — hence the env in `record.sh`.
4. **`script` wraps expect** so expect's own stdout is a pty (unbuffered);
   without it the relay can buffer. The invocation differs between macOS and
   Linux (`driver.sh` handles both).
5. The comment target `y96v` in `driver.sh` is a task id from the repo's
   `TODO.md` — if that task ever disappears, pick another id that exists.

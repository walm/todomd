# Changelog

All notable changes to todomd are documented here. The project follows
[semver](https://semver.org); while on 0.x, minor versions may include
breaking changes to the file format or CLI (they will be called out).

## v0.9.0 — 2026-08-13

### Added

- `todomd boards delete <name>` removes a board. An empty one goes straight
  away; one that still holds tasks needs `--force`, because its tasks are
  deleted with it. If the file isn't committed to git, the forced case warns
  that those tasks aren't recoverable.
- `X` in the TUI deletes the current board — immediately when it's empty, and
  after a `delete board X and its N tasks? (y/n)` confirmation when it isn't.

## v0.8.0 — 2026-08-11

### Added

- `g`/`G` in the open task jump to the top and bottom of its content, matching
  the board's own `g`/`G`.

### Changed

- The open task now keeps its title, id, board, tags, priority and due date
  pinned above the scrolling body, so you can still tell what you're reading
  half way down a long description.

## v0.7.0 — 2026-08-07

### Added

- `/` in the TUI filters the board as you type, matching titles, tags,
  descriptions, comment text and ids. `enter` keeps the query, `esc` restores
  what was there before.
- The TUI shows its version in the bottom-right corner — the release tag, or
  `dev` for a build from source. It yields to the key help on narrow
  terminals.
- `u` filters the board down to cards that changed since your last visit, and
  `esc` clears any active filter. The two compose, column headers show
  `(shown/total)` while filtering, and the footer names what is applied.
  `J`/`K` reordering is refused while filtered, since positions are relative
  to the whole board.

## v0.6.0 — 2026-08-04

### Added

- `A` in the TUI marks every card as read, clearing all unread badges in one
  keystroke instead of opening each card. Later changes are still noticed —
  the cursor advances rather than switching off.

## v0.5.0 — 2026-07-27

### Added

- `todomd archive` clears every task from a board (`Done` by default) in one
  step. It confirms before acting and refuses when the removal wouldn't be
  recoverable: the file must be committed to git, or `--to FILE` must be given
  to move the tasks into another markdown board instead. `--dry-run` previews,
  `--force` overrides the git check, `--yes` skips the prompt and is required
  when stdin isn't a terminal. `--json` reports what went.
- Benchmarks for the paths that scale with file size (`go test ./... -bench .
  -run XXX`): parse, write, round trip, store load/mutate, `changes.Diff`, and
  the TUI board render.

### Changed

- The board render no longer scales with how many tasks the file holds. Each
  column now draws only the cards in its visible window, so a keystroke costs
  the same on a 25,000-task board as on a ten-task one: 339 ms → 0.5 ms
  (678×) at 25,000 tasks, 68 ms → 0.5 ms at 5,000. Column scrolling moved to
  card granularity, with the top card clipped so the window still fills
  exactly as before.

## v0.4.0 — 2026-07-26

### Added

- Task priority: `high`, `normal` (default) or `low`. Set it with
  `add --priority` / `update --priority`, filter with `list --priority`, cycle
  it in the TUI with `p`, or pick it from the `←`/`→` select in the add/edit
  form (also clickable, with hover). Cards show `▲ high`
  / `▼ low`; `"priority"` is always present in JSON. The convention is to work
  High first, then Normal, then Low — todomd records it and leaves scheduling
  to you.
- In the file it is a `**priority:** high` token on the metadata line. The
  default `normal` is never written, so existing files and ordinary tasks are
  untouched.

### Note

- Files that use a priority are read by todomd ≤ v0.3.0 as if the metadata
  line were description text (its parser rejects the unknown token). Upgrade
  every todomd that shares a file before setting priorities.

## v0.3.0 — 2026-07-25

### Added

- `todomd upgrade` installs the latest release over the running binary, with
  the same sha256 verification `install.sh` does. `--check` reports without
  installing, `--force` overrides the up-to-date and source-build guards, and
  `--json` reports the outcome. A failed upgrade always leaves the existing
  binary in place.
- An upgrade hint where a human is looking — the TUI footer, and the end of
  `todomd --help` (on stderr, leaving help output itself unchanged). No other
  command prints it, so scripts and agents see nothing new. The check happens
  in the background at most once a day and is cached;
  `TODOMD_NO_UPDATE_CHECK=1` disables it.

## v0.2.0 — 2026-07-25

### Fixed

- `changes` no longer reports a consumer's own mutations back to it
  ([#1](https://github.com/walm/todomd/issues/1)). Attribution previously
  stopped at `comment_added`, so an agent that moved tasks through the board
  was woken by every one of its own moves.

### Added

- `--as <cursor>` on the mutating commands (`add`, `update`, `move`, `done`,
  `comment`, `delete`): the write is folded into that cursor's snapshot, so
  `changes --as <cursor>` skips it. Only the fields that write touched are
  folded in, so another writer's change to the same task stays pending.
- `TODOMD_CURSOR` env var, used by both `changes` and the mutating commands,
  so a long-running consumer names itself once.

## v0.1.0 — 2026-07-20

Initial release.

### Added

- **Markdown format**: boards as `##` headings, tasks as `###` headings with
  stable IDs in HTML comments, inline-code tags + due dates, verbatim
  markdown descriptions, dated comment lists. Injection-safe (structural
  lines are escaped bijectively), tolerant of markdown formatters, CRLF
  input, and hand edits; parse errors carry line numbers.
- **CLI**: `init`, `list`, `show`, `add`, `update`, `move`, `done`,
  `comment`, `delete`, `boards`, `changes` — all with `--json`, unique
  ID-prefix addressing, and exit codes 0/1/2/3. Writes are serialized via
  an advisory lock in the per-file state dir and applied atomically.
- **`changes`**: per-cursor semantic change feed for agents
  (`--as`, `--peek`, `--ignore-author`) diffing snapshots stored under
  `$XDG_STATE_HOME/todomd` — catches changes from any source.
- **TUI**: responsive Kanban board with vim keys; task detail as a modal
  over the board (glamour-rendered); add/edit/comment forms with in-form
  validation; `E` opens the task as markdown in `$VISUAL`/`$EDITOR`;
  unread badges (`●` new / `○` updated) with per-card read state persisted
  across sessions; auto-reload while idling on the board; full mouse
  support (click to select/open, tap outside to close, clickable
  hover-underlined action labels, wheel scrolling).

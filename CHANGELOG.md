# Changelog

All notable changes to todomd are documented here. The project follows
[semver](https://semver.org); while on 0.x, minor versions may include
breaking changes to the file format or CLI (they will be called out).

## Unreleased

### Added

- `todomd download <id> [file] [-o dir] [--json]` fetches a file a task
  links to on todomd.online (any type, up to 25 MB), using the stored login,
  and checks it against its attachment id. It never overwrites a file.

### Fixed

- Sync leaves links to files that aren't images (PDF, zip, CSV…, which
  todomd.online now accepts as attachments) as todomd.online links, quietly:
  no download attempt and no "download failed" note on every sync. Images
  still download, whether linked as `![]()` or as a plain link.

## v0.11.0 — 2026-11-07

### Added

- `todomd boards add <name>` creates an empty board — before `Done` by
  default, as todomd.online does, or `--after`/`--before` another board.
- `todomd boards rename <board> <new name>` renames a board, keeping its tasks
  and position; a change of case only is fine.
- `todomd boards move <board>` moves a board with `--to <pos>` (1-based),
  `--after <board>` or `--before <board>`.
- The three take `--json` (the board, its position, task count and the board
  order after the change) and `--as`, like every other write. A name another
  board already has, ignoring case, exits with the new code `8`.
- `todomd sync` reports board changes: adding, reordering or renaming a board
  read as "up to date" before, because only task changes were counted. The
  summary now says e.g. `pushed 1 board change`, and `sync --json` has
  `pushed_board_ops` and `pulled_board_ops`. No new sync ops: these are the
  `board.*` ops sync already sends.

### Changed

- An unknown board is now exit code `2` (not found), as for tasks; `boards
  delete` used to exit `1`.

## v0.10.0 — 2026-11-06

### Added

- Sync with [todomd.online](https://todomd.online): `todomd login` (device
  flow: approve the machine in a browser), `logout`, `whoami`, `link`
  (create a project or pick one, matched by git remote; merges an existing
  board after a preview), `unlink`, `sync` (push and pull in one round,
  merged per task and field, online wins conflicts and yours is kept as a
  `todomd-sync` comment), `sync --watch` (live both ways over server-sent
  events), `sync status` and `open`.
- Images sync too: `todomd attach <id> <image>` copies an image into
  todomd's attachment folder and links it from the task; sync uploads it.
  Images pasted on the web board are downloaded and linked by local path.
  Only images inside todomd's own folder are ever uploaded.
  `sync --no-attachments` skips images; `link` offers to bring over images
  attached in todomd-web.
- The TUI syncs a linked file on its own while it's open, with the state in
  the footer corner.
- `todomd changes --wait [--timeout]` blocks until there are changes to
  report.
- New exit codes for sync: `4` not logged in, `5` not linked, `6`
  todomd.online unreachable, `7` this todomd is too old for the service.
- Hidden `canonicalize`, `delta` and `merge` commands, for checking the Go
  and todomd.online implementations of the format against each other.

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

# Changelog

All notable changes to todomd are documented here. The project follows
[semver](https://semver.org); while on 0.x, minor versions may include
breaking changes to the file format or CLI (they will be called out).

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

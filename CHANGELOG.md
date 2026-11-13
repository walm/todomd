# Changelog

All notable changes to todomd are documented here. The project follows
[semver](https://semver.org); while on 0.x, minor versions may include
breaking changes to the file format or CLI (they will be called out).

## v0.16.0 — 2026-11-13

Comment reactions, together with todomd.online's reaction support.
todomd.online keeps syncing with older todomd versions: while a project has
reactions, they sync whole files (no `todomd upgrade` needed).

### Added

- **Comment reactions.** Any emoji on any comment, by any number of people
  (todomd.online's quick set: 👍 ❤️ 🎉 👀 ✅ 🙏). TODO.md stores them as a
  hidden last line of the comment,
  `<!-- reactions: 👍 walm,anna; 👀 agent -->`.
- `todomd react <id> <comment> <emoji> --author <name>` adds a reaction,
  `--remove` takes it back, and without an emoji it prints the comment's
  reactions. The comment is its number as `show` prints it, or `last`;
  `--match <text>` refuses to react unless the comment's text contains that
  text, in case the list changed since you looked. ❤ and ❤️ are the same
  reaction; skin tones are separate ones. Names are lowercased.
- `show` numbers the comments and prints reactions under each one; task
  JSON (`show`, `list`, mutations) and `comment_added` events have
  `"reactions": [{"emoji", "by"}]` on every comment (`[]` for none).
- TUI: reactions under each comment in the open card. A reaction alone
  doesn't mark a card unread.
- `todomd changes`/`watch` report reaction changes as
  `fields.reactions {old, new}` in `task_updated` (`{comment, emoji, by}`
  for the comments that changed). A change to reactions alone is reported
  only when it is on a comment the watcher wrote: by its `--as` name, or
  the new `--author <name>` (repeatable), so an agent wakes when you ✅ its
  plan, not for every reaction on the board.
- `todomd skill` suggests agents react 👀 when they pick up a comment and ✅
  when it's done, instead of "on it" comments, where the user's rules allow.

### Changed

- TODO.md format version 4 (the reactions line). Files without reactions are
  unchanged, and a line that isn't exactly a reactions line, as the
  comment's last line, stays comment text. Older todomd versions read the
  line as the comment's last text line and write it back byte for byte.
- Sync sends `reactions` on comments in its change list. Reactions merge as
  a set: when two people react at once, both count, and a reaction is never
  a conflict. Against a todomd.online server on format version 3 or older, a
  round sends the whole file while the file has reactions.
- `show` prints comments numbered (`1. anna (2026-11-12): …`).

## v0.15.0 — 2026-11-12

Card stickers, together with todomd.online's sticker support. todomd.online
keeps syncing with older todomd versions: while a project has stickers, they
sync whole files (no `todomd upgrade` needed).

### Added

- **Card stickers.** A card can carry one sticker so it stands out on the
  board: an emoji from a fixed set (fire 🔥, star ⭐, eyes 👀, wip 🚧,
  check ✅, target 🎯, zap ⚡, idea 💡, bug 🐛, ship 🚀, question ❓,
  heart ❤️) or a ribbon colour (red, amber, green, blue, violet). TODO.md
  stores it as a hidden line after the metadata line,
  `<!-- sticker: fire -->`.
- `todomd sticker <id> <name|emoji|none>` sets or removes it; `todomd
  sticker <id>` prints it and `todomd sticker` lists the set. Names are
  case-insensitive and the emoji work with or without a variation selector
  (❤ or ❤️).
- `add --sticker`, `update --sticker` and `list --sticker <sticker|any|none>`.
- `show` prints the sticker; task JSON (`show`, `list`, mutations) has
  `"sticker": "fire"` or `null`. The text `list` shows it in a last column
  when a listed card has one.
- TUI: the sticker sits in the top-right corner of the card (an emoji on the
  border, or a coloured corner for a ribbon) and next to the metadata in the
  open card. `s` opens a picker on the board and in the open card (arrows
  and `enter`, `1`–`9` for the first nine emoji, `0` for none). Search takes
  `sticker:fire`, `sticker:🔥`, `has:sticker` (or `sticker:any`) and
  `sticker:none`.
- `todomd changes`/`watch` report sticker changes as
  `fields.sticker {old, new}` in `task_updated`.
- `todomd skill` tells agents to set stickers only when the user asks.

### Changed

- TODO.md format version 3 (the sticker line). Files without stickers are
  unchanged, and a line that isn't exactly a sticker line with a known name,
  in that one position, stays description text. Older todomd versions read
  the sticker line as the first description line and write it back byte for
  byte; on a card with a sticker, replacing its description with an older
  version (`update --desc`) drops it.
- Sync sends `sticker` in its change list; when both sides change a card's
  sticker, the later write wins without a conflict note. Against a
  todomd.online server on format version 2 or older, a round sends the whole
  file while the file has stickers.

## v0.14.0 — 2026-11-11

todomd.online syncs card links only with todomd 0.14.0 or newer, and will
ask older versions to upgrade (`todomd upgrade`).

### Added

- **Card links.** A card can link to other cards: `split from`/`split into`,
  `blocks`/`blocked by`, `related`, `duplicate of`/`duplicated by`. Links
  live in TODO.md as a `#### Links` section between the description and
  `#### Comments`, one per line (`- split from 3f2a`). A link is stored on
  the card where it was added; the other card shows it from its side.
  Links to cards that are no longer on the board (archived, deleted) stay
  and show as "not on the board".
- `todomd split <id> "Title"… [--board B]`: creates one card per title on
  the original's board (or `--board`), with its tags, each linked
  "split from <id>". The original card isn't changed. `--json` prints the
  new cards.
- `todomd links <id>` lists a card's links in both directions;
  `todomd links add <id> <kind> <id>` adds one (the other card must be on
  the board; a link the board already has, from either end, is left as is);
  `todomd links rm <id> <id> [--kind K]` removes the links between two cards
  wherever they are stored (exit 2 if there are none).
- `todomd add --link kind:id` (repeatable), e.g. `--link split-from:3f2a`.
- `show` lists links after the metadata; `show --json`, `list --json` and
  the other commands' task JSON have `links: [{kind, id, title, board,
  stored_on}]`, both directions computed (`title` and `board` are `null`
  for a card not on the board).
- The TUI's open card lists its links under the description, numbered;
  `1`–`9` open the linked card.
- `todomd changes`/`watch` report link changes as `fields.links {old, new}`
  in `task_updated`.
- `todomd skill` tells agents to split cards with `todomd split` and link
  cards with `todomd links add` instead of pasting ids in comments, and not
  to start a card that is blocked by an open one without asking.

### Changed

- TODO.md format version 2 (the `#### Links` section). Files without links
  are unchanged, and a `#### Links` heading that isn't followed only by
  link items stays description text, so existing files read and write back
  byte for byte. Older todomd versions read a links section as description
  text and keep it; on a card with links, replacing its description with an
  older version (`update --desc`) drops them.
- Sync sends `links` in its change list. Against a todomd.online server on
  format version 1, a round whose changes include links sends the whole
  file instead.

## v0.13.0 — 2026-11-08

### Added

- `todomd watch [--as name] [--ignore-author name] [--timeout 10m]
  [--no-wait] [--json]`: the agent loop in one command. It syncs a linked
  file (pushing the agent's own writes, pulling the board's), waits for card
  changes while syncing live, syncs once more and prints the events like
  `changes --wait`, one round per run. A file that isn't linked is just
  watched; when another process (`sync --watch`, the TUI) holds the file's
  sync lock, `watch` leaves syncing to it and waits. `--no-wait` syncs and
  reports without waiting (for an agent's last writes). `--json` adds a
  `sync` object (`linked`, `project`, `rev`, `watcher_pid`, `problem`).
- `todomd skill` prints a skill (SKILL.md) teaching an agent the todomd CLI:
  reading the board, writing as itself, the `todomd watch` loop, the default
  board rules (which are the user's to change) and attachments. The same
  text serves local and todomd.online boards. `todomd skill --install
  [--dir .claude/skills/todomd] [--force]` writes it to `<dir>/SKILL.md`,
  never overwriting one without `--force`.

### Changed

- `todomd --help` groups its commands under Tasks, Boards, todomd.online,
  Agents and Other, in a working order rather than alphabetically. Headings
  and command names are coloured on a terminal only: never when piped, with
  `NO_COLOR` set, or with `TERM=dumb`.
- `archive`'s help says what it clears: the Done board, or the one given
  with `--board`. Without a Done board it now says so, lists the boards and
  points at `--board`; a missing board exits with code 2 (board not found)
  instead of 1.

### Fixed

- `changes --wait` no longer waits silently on a stale file when it can't
  sync (not logged in, offline, a paused sync): it says why on stderr.
- `changes --wait` cut short by Ctrl-C or SIGTERM (an agent's tool timeout)
  no longer advances the cursor, which could consume events nobody read.

## v0.12.0 — 2026-11-07

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

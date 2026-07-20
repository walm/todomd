# todomd

A Kanban TUI and agent-friendly CLI over a plain markdown `TODO.md`.

The markdown file is the single source of truth — readable and diffable for
humans, renderable on GitHub, and deterministic for the tool. Humans use the
TUI; AI agents drive the CLI (`--json` everywhere, stable task IDs, meaningful
exit codes).

## Install

```sh
go build -o todomd .
```

## Quick start

```sh
todomd init                 # creates TODO.md with Backlog / In Progress / Done
todomd                      # opens the Kanban TUI
```

## CLI (for agents and scripts)

```sh
todomd add "Fix the parser" --tag parser --due 2026-08-01 --desc "Details..." --json
todomd list --json                    # everything, grouped by board
todomd show 3f2a --json               # full task detail
todomd update 3f2a --title "New" --tag a --tag b --clear-due
todomd move 3f2a --to "In Progress" --pos 1
todomd done 3f2a
todomd comment 3f2a --author ai "Tried X, going with Y."
todomd delete 3f2a --yes
todomd boards --json
todomd changes --as claude --ignore-author claude --json
```

### Change tracking for agents

`todomd changes` answers "what happened since I last looked" without the
agent having to read or diff the whole file. Each consumer names a cursor
with `--as`; reading advances it (`--peek` doesn't). The first call just
initializes the cursor. Because it diffs snapshots of the file (stored under
`$XDG_STATE_HOME/todomd`, default `~/.local/state/todomd`, keyed by the
file's path), it catches *every* source of change: CLI, TUI, `$EDITOR`,
hand edits, formatters, git pulls.

Events: `task_added` (includes the full task as `detail`), `task_deleted`,
`task_moved` (`from`/`to`), `task_updated` (`fields` with old/new per
changed field — renames stay the same task, identity is the ID),
`comment_added` (the comment). Reorders within a board are not reported.
The typical agent loop:

```sh
todomd comment 3f2a --author claude "done, please review"
# …later…
todomd changes --as claude --ignore-author claude --json   # only others' activity
```

- **File resolution**: `--file` flag > `TODOMD_FILE` env > `TODO.md` searched
  from the cwd upward (stopping at the repo root).
- **IDs** are stable 4-char base36 (they survive renames and moves) and may be
  abbreviated to any unique prefix. Deleted IDs may be reused later — don't
  hold IDs across deletions.
- **Exit codes**: `0` ok · `1` general error · `2` task not found ·
  `3` ambiguous ID prefix.
- Boards are matched case-insensitively and created on demand (new boards land
  before `Done`).
- Concurrent invocations are safe: every write takes an advisory lock
  (kept in `~/.local/state/todomd`, never next to your file) and replaces
  the file atomically.
- Any text is safe to pass — titles reject newlines, and description/comment
  lines that would read as file structure are escaped on write and unescaped
  on parse. The one restriction: multi-line text must not contain an unclosed
  ``` code fence.

## TUI

| Keys | Action |
|---|---|
| `h`/`l` `j`/`k` | Navigate columns / cards (`g`/`G` first/last) |
| `H`/`L` | Move task to previous / next board |
| `J`/`K` | Reorder task within its board |
| `Enter` | Open task detail (modal over the board; `q`/`esc` back) |
| `a` / `e` | Add / edit task (`tab` next field, `ctrl+s` save, `esc` cancel) |
| `E` | Edit the task as markdown in `$VISUAL`/`$EDITOR` (title, tags, due, description, comments) |
| `c` | Comment on task |
| `d` / `D` | Delete (confirm) / move to Done |
| `r` | Reload from disk |
| `?` / `q` | Toggle help / quit |

Inside the open task, `e`, `E`, and `c` work too and return you to the task
afterwards.

Mouse works alongside the keys: click a card to select it, click it again to
open; inside the open task the footer hints (`e edit · E editor ·
c comment`) are clickable and tapping outside the card closes it. Column
headers select their column, and the wheel scrolls (cards on the board, text
in the open task). Terminals need shift-click to select text for copying
while mouse mode is on.

### Unread badges

The TUI tracks what changed since *you* last looked (its own `tui` change
cursor): cards added by someone else show `●` with a green border, cards
updated/moved/commented show `○` with a yellow border, and the status line
counts them on startup. Opening a card marks it read; your own actions never
badge; unread state persists across sessions.

While you idle on the board, the TUI auto-reloads: it stats the file every
2s and refreshes (badging changed cards, keeping your selection) whenever
the file actually changed — so agent activity appears live. Auto-reload
pauses while a task, form, or confirm prompt is open; `r` still forces a
reload any time.

The TUI auto-detects light/dark background at startup. Set `GLAMOUR_STYLE`
(`dark`, `light`, `notty`, …) to pin the theme and skip the terminal query —
useful for terminals that don't answer OSC color queries.

## The file format

```markdown
# TODO

Free-form preamble — never touched by the tool.

## Backlog

### Implement markdown parser
<!-- id:3f2a -->
`#parser` `#core` **due:** 2026-08-01

Description: any markdown, multiple paragraphs, code fences, lists.

#### Comments

- **ai** (2026-07-18): Considered goldmark; hand-rolled round-trips better.
- **andreas** (2026-07-18): Agreed.

## In Progress

## Done
```

Rules, briefly:

- `##` headings are boards (column order = file order); `###` headings are
  tasks (card order = file order). Empty boards persist.
- The `<!-- id:… -->` comment is the task's stable ID (invisible when
  rendered). Hand-added tasks without one get an ID on the next write.
- The metadata line (tags + due) is the first non-blank line below the ID
  comment (formatter-inserted blank lines are fine).
- Everything up to `#### Comments` / the next heading is the description,
  preserved verbatim.
- Comments are one list item each: `- **author** (YYYY-MM-DD): text`, with
  continuation lines indented two spaces.
- Hand-editing is fine; the tool re-canonicalizes spacing on its next write
  and reports malformed content with line numbers.

The full specification lives in [`plans/todomd-plan.md`](plans/todomd-plan.md).

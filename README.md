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
- Concurrent invocations are safe: every write takes an advisory lock and
  replaces the file atomically.
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
| `Enter` | Open task detail (rendered markdown; `q`/`esc` back) |
| `a` / `e` | Add / edit task (`tab` next field, `ctrl+s` save, `esc` cancel) |
| `c` | Comment on task |
| `d` / `D` | Delete (confirm) / move to Done |
| `r` | Reload from disk |
| `?` / `q` | Toggle help / quit |

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

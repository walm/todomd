# todomd — Kanban TUI + agent-friendly CLI for markdown todos

## Overview

`todomd` is a single Go binary that manages tasks stored in a plain markdown file
(`TODO.md` by default). It has two faces:

1. **TUI** (default, no subcommand): a responsive Kanban board built with Charm's
   Bubble Tea, for humans.
2. **CLI subcommands**: `add`, `update`, `move`, `done`, `comment`, `delete`,
   `list`, `show`, `boards`, `init` — designed primarily for AI agents to
   drive, with `--json` output and non-interactive semantics.

The markdown file is the single source of truth. It must stay pleasant to read
and diff for humans (and renderable on GitHub), while being deterministic to
parse and rewrite for the tool.

## Goals

- Human-readable, git-friendly markdown format; renders nicely on GitHub.
- Stable task IDs so agents can address tasks across renames and moves.
- Full CRUD + move/complete/comment from the CLI; machine-readable output.
- Kanban TUI with vim navigation, responsive layout, and a task detail view.
- Safe writes: advisory file lock + atomic replacement, in both CLI and TUI.
- Arbitrary agent-supplied text can never corrupt the file structure.

## Non-goals (v1)

- Multiple files / directories of tasks (one file per board set).
- Board rename, delete, or reorder (boards are created implicitly; reorder by
  editing the file by hand).
- Sync, remote storage, or cross-machine locking.
- Arbitrary markdown preservation — the tool canonicalizes structure on write
  (task *content* — description and comments — is preserved verbatim, modulo
  the escaping rules and LF normalization below).
- Subtasks / checklists inside tasks, dependencies, recurring tasks.

## The markdown format

```markdown
# TODO

Optional free-form preamble — preserved verbatim, never touched by the tool.

## Backlog

### Implement markdown parser
<!-- id:3f2a -->
`#parser` `#core` **due:** 2026-08-01

Line-based parser and canonical writer for the TODO.md format.
Descriptions can span multiple paragraphs, contain lists, code blocks,
even `####`-level headings (except the literal `#### Comments`).

#### Comments

- **ai** (2026-07-18): Consider goldmark, but a hand-rolled parser
  round-trips more predictably for this constrained format.
- **andreas** (2026-07-18): Agreed, keep it hand-rolled.

### Another task
<!-- id:9c41 -->

## In Progress

### Set up CI
<!-- id:b7d0 -->
`#infra`

## Done

### Choose tech stack
<!-- id:11ab -->
```

### Structural rules

- `# <title>` — file title. Anything between it and the first `##` is
  preamble, preserved byte-for-byte.
- `## <board>` — a Kanban column. Board order in the file = column order in
  the TUI. Default boards on `init`: `Backlog`, `In Progress`, `Done`. Empty
  boards render as empty columns and always survive rewrites.
- `### <title>` — a task. Task order within a board = card order.
- `<!-- id:xxxx -->` — first non-blank line after the task heading. 4-char
  lowercase base36 ID. Invisible in rendered markdown.
- Metadata line (optional): the first non-blank line after the ID comment
  (blank lines are tolerated — markdown formatters like to insert one after
  the comment) **iff** the entire line consists of space-separated tokens,
  each either `` `#tag` `` or (at most once) `**due:** YYYY-MM-DD`, with a
  valid date. Any other line — including one containing a bad date — is
  description. A description whose first line genuinely matches the grammar
  is backslash-escaped by the writer (same scheme as headings), so it
  round-trips as description. Tags match `[a-z0-9_-]+`.
- Description: everything after the metadata line up to the exact line
  `#### Comments`, the next `#`/`##`/`###` heading, or EOF. Other `####`
  headings are legal description content. Preserved verbatim.
- `#### Comments` (optional, at most one per task; a second occurrence is
  part of the comments parse and therefore a parse error): a markdown list,
  one comment per top-level item, `- **<author>** (<YYYY-MM-DD>): <text>`.
  Continuation lines are indented 2 spaces (the writer emits exactly 2; the
  parser strips up to 2). Blank lines inside a comment are allowed when
  followed by another continuation line. A top-level line in the comments
  section that matches none of these is a **parse error with a line number**.

### Parsing & robustness policy

- Line-based, tracking fenced code blocks (``` ``` ``` and `~~~`) — headings
  and ID comments inside fences are content, not structure.
- CRLF input is accepted; the file is normalized to LF on every write.
- Lenient where safe, loud where not:
  - Missing `# title` → title becomes `TODO` on next write.
  - Task without an ID comment (hand-added) → adopted; ID assigned on next
    write. Duplicate IDs → first occurrence keeps it, later ones are
    reassigned on next write.
  - Invalid metadata (bad date, stray text) → the line is simply description.
  - `### task` before any `## board`, or malformed comment items → hard parse
    error with file:line and a hint.
- IDs are 4 random base36 chars (crypto/rand), regenerated on collision with
  the current file. Deleted IDs may recur later; agents should not hold IDs
  across deletions (documented in README).

### Escaping (injection safety)

Agent-supplied text must never corrupt structure:

- **Single-line fields** (title, board name, tag, author): newlines are
  rejected at the store boundary with a clear error; leading/trailing
  whitespace trimmed; empty title/board rejected. Tags validated against
  `[a-z0-9_-]+` (CLI accepts `parser` or `#parser`).
- **Multi-line fields** (description, comment text): on write, any line
  matching `^\\*(#{1,3} |#### Comments\s*$|<!-- id:)` gets one extra leading
  `\`; on parse, one `\` is stripped from such lines. This is bijective, so
  any text round-trips exactly, and a rendered `\# heading` still reads fine.

## The data model

```go
type File struct {
    Title    string
    Preamble string  // verbatim
    Boards   []*Board
}

type Board struct {
    Name  string
    Tasks []*Task
}

type Task struct {
    ID          string
    Title       string
    Tags        []string
    Due         *Date         // date only, no time (small local type)
    Description string        // verbatim markdown
    Comments    []Comment
}

type Comment struct {
    Author string
    Date   Date
    Text   string
}
```

## Store & concurrency

`internal/store` is the single mutation API shared by CLI and TUI. Every
mutation is expressed by task ID (never by pointer/index into a stale model)
and executes as: **acquire advisory lock (`flock` on a lock file in the
per-file state dir, `$XDG_STATE_HOME/todomd/<path-hash>/lock`, so repos stay
free of sidecars) → load → apply → atomic write (temp file + rename,
preserving permissions) → release**.
Concurrent `todomd add` calls from parallel agents therefore serialize instead
of losing writes. The TUI holds a model for display only; each keystroke
mutation re-issues a store mutation against a fresh load, then re-renders from
the result — no re-apply/merge logic needed. Reorder is expressed as
"move to position N", which is well-defined against any fresh state.

Board lookup (`--board`, `--to`) is case-insensitive against existing boards;
when no board matches, `add`/`move` create one using the exact given name,
inserted before `Done` if present, else appended.

## CLI design

Root: `todomd [--file F] [command]`. No command → TUI.

File resolution (all commands including `init`): `--file` flag > `TODOMD_FILE`
env > search for `TODO.md` from the cwd upward, stopping at a directory
containing `.git` (inclusive); `init` defaults to `./TODO.md` and fails if the
target exists.

Exit codes: `0` success · `1` general error (including file not found — the
message hints `run 'todomd init'`) · `2` task not found · `3` ambiguous ID
prefix (message lists candidates). Errors are plain text on stderr.

| Command | Behavior |
|---|---|
| `todomd init [--title T]` | Create the file with default boards. |
| `todomd list [--board B] [--tag T] [--json]` | Text: one aligned line per task — `id  board  title  #tags  due`. |
| `todomd show <id> [--json]` | Full task detail (description + comments). |
| `todomd add <title> [--board B] [--desc D] [--tag T ...] [--due YYYY-MM-DD] [--json]` | Add to end of board (default: first board). Prints new ID. |
| `todomd update <id> [--title] [--desc] [--tag T ...] [--due] [--clear-due] [--clear-tags] [--json]` | Only given flags change; `--tag` replaces the whole tag set. |
| `todomd move <id> [--to B] [--pos N] [--json]` | At least one flag required. `--to` defaults to current board. `--pos` is 1-based position in the target after removal; `N > len` appends; `N < 1` errors. |
| `todomd done <id> [--json]` | `move --to Done` (creates `Done` if missing). |
| `todomd comment <id> --author A <text> [--json]` | Append comment dated today. |
| `todomd delete <id> [--yes] [--json]` | Refuses without `--yes` when stdin is a TTY; proceeds in non-TTY (agent) use. |
| `todomd boards [--json]` | Boards with task counts. |

IDs may be abbreviated to a unique prefix everywhere `<id>` is accepted.

Mutating commands print a one-line confirmation, or with `--json` the full
affected task. Library: `spf13/cobra`.

### JSON schema (pinned — agents depend on it)

```jsonc
// Task (used by show, add, update, move, done, comment, delete)
{"id":"3f2a","board":"Backlog","title":"…","tags":["parser"],
 "due":"2026-08-01",            // or null
 "description":"…","comments":[{"author":"ai","date":"2026-07-18","text":"…"}]}

// list
{"file":"/abs/path/TODO.md","boards":[{"name":"Backlog","tasks":[Task…]}]}

// boards
{"boards":[{"name":"Backlog","count":3}]}
```

`delete --json` prints the task as it was before deletion.

## TUI design

Stack: `charmbracelet/bubbletea` + `lipgloss` + `bubbles` (viewport,
textinput, textarea, help, key). Detail view rendered with
`charmbracelet/glamour`, falling back to plain text if rendering fails.
If no TODO.md is found, the TUI prints the same error + `todomd init` hint
and exits 1.

### Kanban view (main)

- One column per board (name + count in header), equal widths filling the
  terminal, minimum usable column width ~26 cols. When all boards don't fit,
  show as many whole columns as fit and page horizontally so the selected
  column is always visible, with `‹ ›` overflow indicators.
- Cards: title (ansi-aware wrap, max 2 lines — lipgloss handles CJK/emoji
  widths), tag chips, due date (red when overdue, yellow within 3 days), and
  an ASCII `(n)` comment-count badge. No emoji glyphs — terminal width safety.
- Selected card gets an accent border; selected column a highlighted header.
- Tall columns scroll vertically, following the selection.
- Footer: condensed key help (`bubbles/help`), expandable with `?`; a status
  line shows transient messages (saved, reloaded, errors).

### Detail view

`Enter` opens a full-screen task view: title, board, tags, due, glamour-
rendered description, comment list — scrollable viewport.

### Keys (vim-first, scoped per view)

Board view:

| Key | Action |
|---|---|
| `h`/`l`, `←`/`→` | Select previous / next column |
| `j`/`k`, `↓`/`↑` | Select previous / next card |
| `g`/`G` | First / last card in column |
| `H`/`L` | Move selected task to previous / next board |
| `J`/`K` | Reorder selected task down / up |
| `Enter` | Open detail view |
| `a` | Add task to current column (overlay form: title, tags, due, description) |
| `e` | Edit selected task (same form, prefilled) |
| `c` | Comment on selected task (author defaults to `$USER`) |
| `d` | Delete selected task (y/n confirm) |
| `D` | Move selected task to `Done` |
| `r` | Reload file from disk |
| `?` | Toggle full help |
| `q`, `Ctrl-C` | Quit |

Detail view: `j`/`k` scroll, `q`/`Esc` back to board, `Ctrl-C` quit.
Forms: `Tab`/`Shift-Tab` between fields, `Ctrl-S`/`Enter`-on-last-field
submit, `Esc` cancel.

## Project layout

```
todomd/
├── go.mod                  (module github.com/walm/todomd)
├── main.go                 (thin: calls cli.Execute)
├── internal/
│   ├── task/               (model, Date type, ID generation)
│   ├── markdown/           (parser + canonical writer + escaping)
│   ├── store/              (lock, load/save, discovery, ID-based mutations)
│   ├── cli/                (cobra commands, JSON output)
│   └── tui/                (app.go, board.go, detail.go, form.go, styles.go)
├── plans/todomd-plan.md
└── TODO.md                 (dogfood: this project's own tasks)
```

## Implementation milestones

1. **Core**: `task` model; `markdown` parser + writer with round-trip tests;
   `store` with flock, atomic write, discovery, prefix resolution, mutations.
2. **CLI**: all commands; pinned JSON; table-driven tests against temp dirs;
   exit codes.
3. **TUI**: kanban view (layout, navigation, paging) → detail view →
   mutations → forms → status line.
4. **Polish**: adaptive light/dark colors, README (format spec + agent usage
   examples), `go vet` clean, dogfood TODO.md.

## Testing strategy

- `markdown`: round-trip golden tests (parse→write→parse equality; write is a
  fixed point); edge cases: empty boards, fences containing `## fake`,
  CRLF, missing/duplicate IDs, escaping bijectivity (property-style test over
  nasty descriptions), malformed comments → line-numbered errors.
- `store`: mutation semantics, prefix ambiguity, board case-insensitivity,
  concurrent-writer test (two goroutines adding under flock, both survive).
- `cli`: per-command end-to-end against `t.TempDir()`, asserting file bytes,
  JSON output, and exit codes.
- `tui`: model-level tests for key handling and column-paging math; no golden
  screens in v1.

## Dependencies

- `github.com/charmbracelet/bubbletea`, `lipgloss`, `bubbles`
- `github.com/charmbracelet/glamour` (detail view only — note it pulls
  goldmark transitively; we still don't use goldmark for parsing, since the
  constrained format round-trips more predictably with a line-based parser)
- `github.com/spf13/cobra`
- `golang.org/x/sys` (flock; already an indirect dep)

---

*Reviewed by a design-review sub-agent on 2026-07-18; all 18 findings
(3 blockers: heading injection, metadata-line grammar, description
termination) resolved in this revision.*

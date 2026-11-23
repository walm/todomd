# Card start date: file format, sync and merge (format version 6)

**Status: card utvc.** This is the reference for the `**start:**` field and
how sync and merge treat it. The Go code (`internal/markdown`,
`internal/task`, `internal/ops`, `internal/merge`, `internal/changes`,
`internal/cloudsync`, `internal/store`, `internal/cli`) implements it.
todomd.online's `packages/todofile` must match it byte for byte. The shared
golden files are in [`testdata/start/`](../testdata/start).

## The field

A card has **at most one start date**, a plain date like the due date. It
needs no due date. If both are set, the start is on or before the due date
(the same day is fine).

- **Writers refuse start > due.** `todomd add`/`update` print an error and
  change nothing. The server does the same for ops and whole files it is
  asked to write.
- **Readers keep it.** A file with `start` after `due` (a hand edit, a merge
  of two edits) parses as written and writes back byte for byte. Clients
  flag it (a warning, a red date) instead of repairing it.

## In TODO.md

The start goes on the metadata line, **just before** `**due:**` (or where
`**due:**` would go when there's no due date), after the priority:

```markdown
### Migrate attachments to R2 lifecycle rules
<!-- id:r209 -->
`#infra` **priority:** high **start:** 2026-11-10 **due:** 2026-11-14

<!-- assignee: anna -->

Move the attachment bucket to lifecycle rules.
```

Order on the line: `#tags`, `**priority:**`, `**start:**`, `**due:**`.

**Reading.** The metadata grammar gains one more token: at most one
`**start:** YYYY-MM-DD`, exactly that key (lowercase, with the colons inside
the bold), followed by a valid date. Tokens may come in any order. A line
with a bad date (`2026-13-40`), a second `**start:**` or a key without a
value is **not** a metadata line, and the whole line is description text,
like any other line that breaks the grammar.

**Writing.** Tokens go out in the fixed order above. A card whose
description would itself read as a metadata line with a start date gets the
existing one-backslash escape (`\**start:** …`).

## Compatibility with older clients (tested)

This is the same trade as the "option A" in
[card-assignees-format.md](card-assignees-format.md), chosen by the spec: a
format-5 metadata parser doesn't know `**start:**`, so the **whole metadata
line** of a card with a start date reads as the first line of its
description. Tested with the v0.22.0 binary on a card with
`` `#infra` **priority:** high **start:** 2026-11-10 **due:** 2026-11-14 ``:

- `show --json`: `tags: []`, `priority: "normal"`, `due: null`, and the
  description starts with the whole line. Nothing is deleted.
- `update --priority low` (also `--tag`): it writes a **new** metadata line
  (`` `#a` **priority:** low ``) above the old one. A format-6 reader then
  takes the new line as metadata, and the old line (tags, priority, start,
  due) is description text. The text is still there, but the card's real
  start and due are demoted into the description.

So an older CLI must never write to a file or project that has start dates
unless something protects it. The two protections:

1. **The server** (todomd.online) holds an old-client guard like
   `ASSIGNEES_CLI_VERSION`: a `todomd/x.y.z` User-Agent below the first
   release with start support syncs whole files while the project has any
   start date, and `format_version` 6 is announced in
   `/.well-known/todomd`. (Server side; not in this repo.)
2. **This CLI against an older server.** `cloudsync` sends the whole file,
   never ops, when the server's format is below 6 and the base or the local
   file has any start date (`hasStart`, the same guard as `hasStickers`,
   `hasReactions` and `hasAssignees`). An older server then keeps the
   metadata line as description text, byte for byte, and a later sync to a
   v6 server reads it back as the field. Without a start date anywhere the
   CLI keeps sending ops as before. Once the last start date is removed the
   link goes back to ops.

A local TODO.md edited by an older CLI isn't protected by either; upgrade
todomd where the file has start dates.

## Downgrade line (older clients)

todomd.online sends a client below format 6 each start date as a hidden
first description line, `<!-- start: YYYY-MM-DD -->`, instead of
`**start:**` (a format-5 reader keeps such a line byte for byte). It reads
the line back as the field on whole-file pushes from new clients. So a
TODO.md that todomd v0.22 pulled may hold such lines.

`markdown.UpgradeStart` (run when the CLI loads the file and its sync base)
turns an unescaped `<!-- start: DATE -->` first description line, and the
blank lines after it, into the card's start. A card that already has a
start keeps its description. `Parse` itself does not do this (todofile's
`parse` doesn't either; it is `upgradeStart`, applied to old clients'
pushes). A backslash-escaped line is left as it is, one backslash and all:
`write` doesn't escape such lines, so unescaping one here would turn real
text into a start date on the next read.

## Sync ops (wire format)

There are no new op names.

- `task.update.set.start` is `"YYYY-MM-DD"`, or `null` to clear. The keys
  go in this order: `title, tags, priority, due, start, sticker, assignee,
  description, links`.
- `task.add.task.start` is present **only when set**, so a card without a
  start has ops byte-identical to format 5. (`due` is always present, as
  before.)
- `Delta` emits `set.start` when the start differs.
- The CLI sends `format_version` (6) in every sync body and
  `?format_version=6` on `GET …/document`, besides the `todomd/0.23.0`
  User-Agent, so the server treats it as a client that reads start dates.

## Merge

`start` is a scalar like `due`: `pick(base, ours, theirs)`.

| Case | Result |
|---|---|
| Only one side changed it | That side's value |
| Both set the same date | That date |
| Both changed it differently | **theirs**, a field conflict `start`, and a todomd-sync comment: `Sync conflict on start date — this machine's version was: 2026-11-10` |
| Baseless | theirs if set, else ours |

A merge can combine one side's new start with the other side's new due into
start > due. It is kept (readers keep it); the clients flag it.

## Events

`changes.Diff` (`todomd changes`/`watch`) adds `fields.start: {old, new}` to
`task_updated`; each value is a date string or `null`.
`show --json` and `list --json` get `"start": "2026-11-10"` or `null`, next
to `due`.

## CLI

```sh
todomd add "Title" --start 2026-11-10 --due 2026-11-14
todomd update r209 --start 2026-11-12          # same date form as --due
todomd update r209 --start none                # or --clear-start
```

`--start` takes exactly what `--due` takes (`YYYY-MM-DD`). `show` prints
`start:` above `due:`. The filter language has no date terms today, so
`start:`/`due:` range filters are not part of this change.

## Rollout

1. **todomd.online** first: todofile with `FORMAT_VERSION = 6`, ops, merge,
   events, and the old-client guard. `/.well-known/todomd` announces 6.
2. **todomd CLI** is released only after production reports format 6. Until
   then no released CLI writes `**start:**`.
3. Against a server still on format 5, this CLI sends whole files while any
   card has a start date (see above), so nothing is lost either way.

## Decisions

- **`**start:**` on the metadata line**, before `**due:**`, as specified.
  The cost is the older-client behaviour above.
- **A start without a due date is allowed.**
- **Writers refuse start > due; readers keep it.**
- **Format version 6.**

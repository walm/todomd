# Card links: file format, sync and merge (format version 2)

This is the reference for the `#### Links` section and how sync and merge
treat it. The Go code (`internal/markdown/links.go`, `internal/ops`,
`internal/merge`) implements it. todomd.online's `packages/todofile` must
match it byte for byte. The shared golden files are in
[`testdata/links/`](../testdata/links/README.md).

## In TODO.md

```markdown
### Replay queued writes on reconnect
<!-- id:zm4z -->
`#sync` **priority:** high

On reconnect, push the queue as one sync round.

#### Links

- split from mpbv
- related 8xto

#### Comments

- **agent** (2026-11-11): …
```

A link is stored on one card only, the card where it was written. Readers
compute the other side (`split from` ↔ `split into`, `blocks` ↔ `blocked by`,
`duplicate of` ↔ `duplicated by`, `related` ↔ `related`). A link may point at
a card that isn't on the board (archived or deleted). Nothing removes it
automatically.

### Reading

The section belongs to a card's description span: everything from after the
metadata line up to the next `#`, `##` or `###` heading or `#### Comments`
line. Only lines outside column-0 code fences ("structural" lines) count.

1. Find the **last** structural line in the span that matches
   `^\\*#### Links[ \t]*$`, which is the heading with any number of leading
   backslashes (zero or more).
2. Every later line in the span must be blank (whitespace only, as the
   description's blank-line rule uses: Unicode spaces such as U+00A0 and
   U+2028 count) or a structural **item**. There must be **at least one
   item**. If not, nothing happens: the description is the whole span, read
   as before.
3. If the line has no backslash, it is the section: the items are the card's
   links, and the description is the span before the heading.
4. If it has backslashes, it is description text. Drop **one** backslash from
   that line only. This is the escape (see Writing).

An **item** is `^-[ \t]+(.+?)[ \t]+([0-9a-z]{4})[ \t]*$`, where group 1 is a
kind:

- ASCII letters in any case, `^([A-Za-z]+)(?:(?:[ \t]+|-)([A-Za-z]+))?$`;
  the two words of a kind are separated by spaces/tabs or by exactly one `-`.
- Lowercase it and join the words with one space. The result must be one of
  `split from`, `split into`, `blocks`, `blocked by`, `related`,
  `duplicate of`, `duplicated by`.

So `- Blocked-By zm4z`, `-\tSPLIT  FROM\tzm4z ` and `- related zm4z` are
items. `- related ZM4Z`, `- related zm4z Title`, `* related zm4z`,
`- duplicate zm4z`, `- blocked--by zm4z` and `- bogus zm4z` are not. One of
these anywhere after the heading makes the whole section description text.

After reading, duplicates (same kind and id) are removed, keeping the first.
Self-links are removed **after** ids are final, i.e. after the non-strict
parser repairs missing or duplicate ids. Order is kept as written. There is
no limit on the number of items when reading.

`#### Links` after `#### Comments` is a parse error:
`line N: '#### Links' must come before '#### Comments'`. The heading is not
recognised inside the comments section. Older parsers also fail there, with
"unexpected line in comments section".

### Writing

```
### Title
<!-- id:xxxx -->
[metadata line]
[\n description lines]
[\n#### Links\n\n- <kind> <id>\n…]      only when the card has links
[\n#### Comments\n\n…]
```

Kinds are written in their canonical spelling, in model order.

**Escaping.** If the card has **no** links, run the reading steps 1–2 over the
description's own lines (structural per the description's fence mask). If they
find a line, write that one line with one more leading backslash. No other
description line is escaped for links. When the card has links, the real
section comes last, so nothing in the description can be mistaken for it.

This keeps every file older todomd versions wrote byte-stable. A
`#### Links` heading followed by text or URLs (`- https://…`) stays exactly
as it is. A literal `\#### Links` that wouldn't be read as the section also
stays exactly as it is.

### Older clients

Before format 2, `#### Links` wasn't special, so an older parser reads the
section as the end of the description and writes it back byte for byte. The
new writer's output for a card with links is exactly what the old writer
writes for a description that ends in that text. Checked: 3,000 random
format-2 files from `internal/todogen` and every `testdata/links/*.out.md`
are unchanged through the v0.13.0 parser and writer.

## Model and validation

`Task.Links []Link{Kind, ID}`, in file order. `nil`/empty means no links.

`merge.CheckTask` (and the server's "can be written and read back" check)
rejects a card whose links have:

- an unknown kind (the canonical spelling is the only accepted one);
- a malformed id;
- a link to the card itself;
- a duplicate;
- more than 50 links.

The target does not have to exist.

## Sync ops (wire format)

No new op names. A link is `{"kind": "<canonical kind>", "id": "<id>"}`.

- `task.update.set.links` is the card's whole list after the change, like
  `tags`. `[]` clears it. In `set` it comes after `description`.
- `task.add.task.links` is present **only when the card has links**. It goes
  between `description` and `comments`. A card without links has no `links`
  key, so those ops are byte-identical to format 1. Readers accept the key
  as optional.
- The decoder is strict: exactly the keys `kind` and `id`; `kind` must be the
  canonical spelling (`blocked-by` or `Blocks` are rejected); `id` must be a
  valid id. Duplicates, self-links and the limit are `CheckTask`'s job.

`Delta` emits `set.links` when the lists differ, order included.

The CLI sends ops with links only to a server whose `format_version` is 2 or
more. If the server reports 1, a round whose changes carry links sends the
whole file instead. The old server keeps the section as description text,
losslessly. Other rounds still use ops.

## Merge

- Links are merged as a set keyed by `(kind, id)`, with the tags rule:
  `(base ∪ ours' added ∪ theirs' added) − ours' removed − theirs' removed`,
  in theirs' order with ours' new links appended. This is never a conflict.
  `NoDeletesFromOurs` keeps links ours removed. Baseless means theirs plus
  ours' extra links.
- A links change counts as an edit for delete-vs-edit, so the card is kept.
- A link to a card the other side deleted stays.
- **Id collision remap.** Ours' card can get a new id: a fresh `stableID`,
  or a reused identical copy that theirs holds. Then every link on an ours
  card that points at the old id and is **not** on the base card with the
  same id is changed to point at the new id. Order is kept, and a duplicate
  created this way is dropped. This happens after the new ids are chosen,
  so `stableID` and the copy check see ours' card with its links unchanged.
  Links that were in the base, and theirs' links, keep the old id.

## Events

`changes.Diff` (`todomd changes`/`watch`) reports
`fields.links: {old: [{kind, id}], new: [{kind, id}]}` in `task_updated`.

## Decisions the plan left open

- **Empty section** (heading, no items): stays description text. The writer
  never writes one, and an old file that ends a description in `#### Links`
  stays as it is.
- **Escape only where needed** (the one line that would read as the section),
  not every `#### Links` line, so old files with a "Links" heading over URLs
  don't change.
- **Kinds are ASCII-only**, so Unicode case folding (Kelvin sign `K` →
  `k`) can't make Go and JavaScript disagree.
- **Ids in items are lowercase only**, as everywhere else.
- **More than 50 links** parse; validation rejects them with a clear error.
  A hand edit is never silently turned into text.
- **`#### Links` after `#### Comments`** is a parse error with a clear
  message. It is not reordered.
- **`task.add` omits `links` when empty**; `set.links` always lists the whole
  list.

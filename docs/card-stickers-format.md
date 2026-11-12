# Card stickers: file format, sync and merge (format version 3)

This is the reference for the sticker line and how sync and merge treat it.
The Go code (`internal/task/sticker.go`, `internal/markdown/stickers.go`,
`internal/ops`, `internal/merge`) implements it. todomd.online's
`packages/todofile` must match it byte for byte. The shared golden files are
in [`testdata/stickers/`](../testdata/stickers/README.md).

## The set

A card has at most one sticker: an emoji from a fixed set, or a ribbon
colour. It is stored by its ASCII name.

| Name | Emoji | | Name | Emoji |
|---|---|---|---|---|
| `fire` | 🔥 U+1F525 | | `zap` | ⚡ U+26A1 |
| `star` | ⭐ U+2B50 | | `idea` | 💡 U+1F4A1 |
| `eyes` | 👀 U+1F440 | | `bug` | 🐛 U+1F41B |
| `wip` | 🚧 U+1F6A7 | | `ship` | 🚀 U+1F680 |
| `check` | ✅ U+2705 | | `question` | ❓ U+2753 |
| `target` | 🎯 U+1F3AF | | `heart` | ❤️ U+2764 U+FE0F |

Ribbons: `red`, `amber`, `green`, `blue`, `violet`.

Picker order is the order above, read down each column (fire … target, zap …
heart), then the ribbons.

## In TODO.md

```markdown
### Migrate attachments to R2 lifecycle rules
<!-- id:r209 -->
`#infra` **priority:** high **due:** 2026-11-14

<!-- sticker: fire -->

Move the attachment bucket to lifecycle rules.
```

### Reading

After the id comment and the optional metadata line, take the **first
non-blank line** (blank as the description's blank-line rule uses: Unicode
spaces such as U+00A0 and U+2028 count) that is outside a column-0 code
fence. Match it against

```
^(\\*)<!-- sticker: ([a-z]+) -->$
```

and require group 2 to be one of the 17 names.

1. No match: there is no sticker. The line is description text as before.
2. Match with **no** backslash: it is the card's sticker. The line is
   consumed; the description starts after it (leading blank lines trimmed,
   as always).
3. Match with backslashes: it is the description's first line. Drop **one**
   backslash from that line only. This is the escape (see Writing).

The match is exact: one space after `<!--`, after `sticker:` and before
`-->`, lowercase, nothing before or after (no trailing spaces). So
`<!-- sticker: banana -->`, `<!-- sticker: Fire -->`, `<!-- sticker: 🔥 -->`,
`<!--sticker:fire-->`, `<!-- sticker: fire --> ` and ` <!-- sticker: fire -->`
are all description text.

Only that one position counts. A sticker line anywhere else (second line of
the description, at its end, in a fence, a second sticker line right after
the first) is description text. A sticker line before the metadata line is
read as the sticker, and the metadata line then becomes description text. A
sticker line in the comments section is the existing parse error
`unexpected line in comments section`.

### Writing

```
### Title
<!-- id:xxxx -->
[metadata line]
[\n<!-- sticker: NAME -->]            only when the card has a sticker
[\n description lines]
[\n#### Links\n\n…]
[\n#### Comments\n\n…]
```

The sticker line comes after a **blank line**. That is where older writers
put the blank line before a description, so for an older parser the sticker
line is simply the first description line and it writes the card back byte
for byte (see Older clients). The reader accepts the line without the blank
line too (as the plan's example writes it); the writer normalises to the
form above.

**Escaping.** If the card has **no** sticker and the description's first line
(structural, per the description's fence mask) matches the regex above with
a known name, with any number of backslashes, write it with one more leading
backslash. When the card has a sticker, nothing is escaped for stickers: the
real sticker line comes first, and the description's first line is never
read as one. The sticker escape and the metadata escape (`escapeMetaLine`)
can't both apply to one line.

## Older clients (format 2 and 1, todomd < 0.15)

An older parser reads the sticker line as the first line of the description
and writes it back byte for byte. Checked: 3,000 random format-3 files from
`internal/todogen` and every `testdata/stickers/*.out.md` are unchanged
through the v0.14.0 and v0.13.0 parser and writer, and after those versions
add a tag to every card, the new parser reads every sticker, description and
link back unchanged.

What older clients do with it:

- `show` prints the line at the top of the description.
- `update --desc` on a card with a sticker replaces the description and so
  drops the sticker, as they drop links.
- Sync: todomd.online sends a CLI older than 0.15 to whole-file sync while
  the project has stickers (the same guard as for links). The whole file
  carries the line as text, so nothing is lost.

## Model and validation

`Task.Sticker string`: a name from the set, or `""` for none.

`merge.CheckTask` (and the server's "can be written and read back" check)
rejects a sticker that isn't one of the 17 names in canonical spelling
(`Fire`, `🔥`, `none` are rejected; `""` is no sticker).

## Sync ops (wire format)

No new op names.

- `task.update.set.sticker` is the new name, or `null` to remove the
  sticker. In `set` it comes after `due` and before `description`.
- `task.add.task.sticker` is present **only when the card has a sticker**. It
  goes between `due` and `description`. A card without one has no `sticker`
  key, so those ops are byte-identical to format 2.
- The decoder accepts a known name or `null` (no sticker) in both places,
  and rejects anything else (`""`, `"Fire"`, `"🔥"`, `"none"`, numbers).

`Delta` emits `set.sticker` when the sticker differs.

The CLI sends ops to a server whose `format_version` is 3 or more. If the
server reports 2 or less and the base or the local file has any sticker, the
round sends the whole file instead: the old server's ops reader rejects the
`sticker` key, and it holds a sticker line as the first line of the card's
description, which a `set.description` op would overwrite. Other rounds still
use ops.

## Merge

- A scalar with **no conflict**: if only one side changed it, that side
  wins; if both changed it differently, theirs (the later write) wins, and no
  conflict is reported or noted on the card. Baseless: theirs.
  `NoDeletesFromOurs` does not keep a sticker theirs removed.
- A sticker change counts as an edit for delete-vs-edit, so the card is
  kept.

## Events

`changes.Diff` (`todomd changes`/`watch`) reports
`fields.sticker: {old, new}` in `task_updated`, each a name or `null`.

## Decisions the plan left open

- **Blank line before the sticker line** in written files, so older
  versions round-trip it byte for byte (without it, an older writer inserts
  a blank line after the metadata line).
- **Exact match only**, no whitespace or case variants, so Go and
  JavaScript can't disagree.
- **Escape only when the card has no sticker**, mirroring links.
- **Both sides change the sticker**: theirs wins silently. A sticker is
  decoration, not worth a conflict comment.
- **`null` is accepted in `task.add`** as no sticker, although writers leave
  the key out.

# Card assignee golden files

These files are shared by todomd (Go) and todomd.online's `packages/todofile`
(TypeScript). Both must give exactly these results. The format is described
in [docs/card-assignees-format.md](../../docs/card-assignees-format.md).

For each `NAME.md` input:

| File | Content |
|---|---|
| `NAME.out.md` | `write(parse(NAME.md))`, byte for byte. When it equals the input, the input is canonical. |
| `NAME.json` | The parsed model. `assignee` is always present here (a handle or `null`), and so are `sticker`, `links`, `comments` and each comment's `reactions`. Key order: `id, title, tags, priority, due, sticker, assignee, description, links, comments`. |
| `NAME.err` | Replaces both when parsing fails: the error, `line N: message`. |

All inputs parse in strict mode (every card has its id).

| Input | What it pins down |
|---|---|
| `basic` | The line with and without metadata, sticker, description, links, comments; an unassigned card. Canonical. |
| `crlf` | `basic` with CRLF line endings; the output is LF. |
| `handles` | 1 character, digits only, inner `--`, 39 characters; 40 is text. |
| `not-an-assignee` | Near misses that stay description text: `@`, case, lists, spacing, tabs, bad handles, other keys, text after the line, a later line, the visible `**assignee:**` form. |
| `escaped` | The backslash escape with and without a sticker; no unescape under a real assignee; a sticker-looking first line under an assignee is neither read as a sticker nor escaped; an escaped sticker keeps the next line text. |
| `order` | Assignee before the sticker (the sticker becomes text) or before the metadata line (the metadata line becomes text, escaped); two lines; in a fence; followed by links. |
| `no-blank-line` | The line right after metadata, sticker or id, and extra blank lines; the writer normalises. |
| `unicode-blank` | U+00A0 and U+2028 lines count as blank around the line. |
| `in-comments` | An indented assignee line in a comment is comment text. |
| `assignee-in-comments` | An unindented one is the existing parse error. |
| `no-assignees-old-format` | A format-4 file without assignees writes back unchanged. |

- `ops.json`: `[{name, a, b, ops}]`. `delta(parse(a), parse(b))` must
  serialize to `ops` (compare as JSON values: Go writes `<` as `<`),
  and `write(apply(parse(a), ops))` must equal `b`.
- `merge.json`: `[{name, options?, base, ours, theirs, merged, conflicts,
  renamed}]`. `merge(base, ours, theirs, options)` with the conflict date
  2026-11-05 must write `merged` and report those conflicts and renames.
  `options` uses `baseless` and `no_deletes_from_ours`.

Regenerate from Go after a deliberate change and review the diff:

```sh
go test ./internal/markdown ./internal/ops ./internal/merge -run 'GoldenAssignees|DeltaAssignees|MergeAssignees' -update
```

`crlf.md` uses CRLF line endings. Keep it that way: `.gitattributes` marks
this directory `-text`.

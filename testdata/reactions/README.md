# Comment reaction golden files

These files are shared by todomd (Go) and todomd.online's `packages/todofile`
(TypeScript). Both must give exactly these results. The format is described
in [docs/comment-reactions-format.md](../../docs/comment-reactions-format.md).

For each `NAME.md` input:

| File | Content |
|---|---|
| `NAME.out.md` | `write(parse(NAME.md))`, byte for byte. When it equals the input, the input is canonical. |
| `NAME.json` | The parsed model. `sticker` is always present (a name or `null`), and so are `links` and `comments`; every comment has `reactions`, an array of `{emoji, by}` (`[]` for none). |
| `NAME.err` | Replaces both when parsing fails: the error, `line N: message`. |

All inputs parse in strict mode (every card has its id).

- `basic.md`: reactions on single- and multi-line comments, the quick set,
  skin tones, keycaps, flags, ZWJ sequences, text-default symbols.
- `crlf.md`: `basic.md` with CRLF line endings. Keep it that way:
  `.gitattributes` marks this directory `-text`.
- `empty-text.md`: a comment with an empty text and reactions.
- `escaped.md`: comment text whose last line looks like a reactions line.
- `not-reactions.md`: near misses that stay comment text.
- `normalise.md`: hand-written lines the writer normalises (repeated names,
  ❤ and ❤️, a blank line or one-space indent before it, two lines).
- `no-reactions-old-format.md`: a format-3 file, unchanged.
- `unindented.md`: a reactions line at column 0, the existing parse error.
- `ops.json`: `[{name, a, b, ops}]`. `delta(parse(a), parse(b))` must
  serialize to `ops` exactly, and `write(apply(parse(a), ops))` must equal
  `b`.
- `merge.json`: `[{name, options?, base, ours, theirs, merged, conflicts,
  renamed}]`. `merge(base, ours, theirs, options)` with the conflict date
  2026-11-05 must write `merged` and report those conflicts and renames.
  `options` uses `baseless` and `no_deletes_from_ours`.

Regenerate from Go after a deliberate change and review the diff:

```sh
go test ./internal/markdown ./internal/ops ./internal/merge -run 'GoldenReactions|DeltaReactions|MergeReactions' -update
```

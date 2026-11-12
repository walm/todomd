# Card sticker golden files

These files are shared by todomd (Go) and todomd.online's `packages/todofile`
(TypeScript). Both must give exactly these results. The format is described
in [docs/card-stickers-format.md](../../docs/card-stickers-format.md).

For each `NAME.md` input:

| File | Content |
|---|---|
| `NAME.out.md` | `write(parse(NAME.md))`, byte for byte. When it equals the input, the input is canonical. |
| `NAME.json` | The parsed model. `sticker` is always present here (a name or `null`), and so are `links` and `comments`. |
| `NAME.err` | Replaces both when parsing fails: the error, `line N: message`. |

All inputs parse in strict mode (every card has its id).

- `ops.json`: `[{name, a, b, ops}]`. `delta(parse(a), parse(b))` must
  serialize to `ops` exactly, and `write(apply(parse(a), ops))` must equal
  `b`.
- `merge.json`: `[{name, options?, base, ours, theirs, merged, conflicts,
  renamed}]`. `merge(base, ours, theirs, options)` with the conflict date
  2026-11-05 must write `merged` and report those conflicts and renames.
  `options` uses `baseless` and `no_deletes_from_ours`.

Regenerate from Go after a deliberate change and review the diff:

```sh
go test ./internal/markdown ./internal/ops ./internal/merge -run 'GoldenStickers|DeltaStickers|MergeStickers' -update
```

`crlf.md` uses CRLF line endings. Keep it that way: `.gitattributes` marks
this directory `-text`.

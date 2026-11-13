# Comment reactions: file format, sync and merge (format version 4)

This is the reference for the reactions line and how sync and merge treat
it. The Go code (`internal/task/reaction.go`, `internal/markdown/reactions.go`,
`internal/ops`, `internal/merge/reactions.go`, `internal/changes`)
implements it. todomd.online's `packages/todofile` must match it byte for
byte. The shared golden files are in
[`testdata/reactions/`](../testdata/reactions/README.md).

## In TODO.md

```markdown
#### Comments

- **anna** (2026-11-12): Can you look at the export?
  <!-- reactions: 👍 walm,anna; 👀 agent -->
- **agent** (2026-11-12): On it. Plan:

  1. read the board
  2. write CSV
  <!-- reactions: ✅ walm -->
- **walm** (2026-11-13): No reactions here.
```

A comment's reactions are its **last line**, a continuation line indented by
two spaces like the comment's other lines. Each entry is an emoji, one
space, and the names of the people who reacted with it, joined by `,`.
Entries are joined by `; `.

### Grammar

```
line    = "<!-- reactions: " entry *( "; " entry ) " -->"
entry   = emoji " " name *( "," name )
```

**Emoji.** 1 to 16 code points, none of them

- ASCII, except the keycap bases `#`, `*` and `0`–`9`,
- U+0080–U+00A0 (C1 controls, NBSP),
- U+1680, U+2000–U+200A, U+2028, U+2029, U+202F, U+205F, U+3000, U+FEFF
  (spaces and the BOM),
- invalid UTF-8 (a lone surrogate in JavaScript),

and at least one code point that is not ASCII and not U+200D (ZWJ),
U+FE0E or U+FE0F (variation selectors). U+20E3 (the keycap) counts, so `1️⃣`
(`1`, U+FE0F, U+20E3) is valid. This doesn't check that the emoji is a real
one: that would need Unicode's tables, and Go and JavaScript must agree.
Writers should only let people pick real emoji.

**Name.** `^[a-z0-9][a-z0-9._-]{0,63}$`: the todomd.online handle, or an
agent's or a person's name in the CLI (`agent`, `claude`, `walm`). Lowercase
only, so case can't make two names of one person; the CLI lowercases what
you type.

### Reading

In a comment, take its text as today (continuation lines with their indent
stripped: two spaces, else one; trailing spaces and newlines trimmed from the
end of the text). If the text has **more than one line**, look at its last
line. Match it against

```
^(\\*)<!-- reactions: (.+) -->$
```

and parse group 2 by the grammar above (split on `"; "`, each entry at its
first space, the names on `","`). Any entry that doesn't parse, an empty
name, an invalid emoji: no match.

1. No match: there are no reactions. The line is comment text as before.
2. Match with **no** backslash: these are the comment's reactions. The line
   is dropped from the text, and the text is trimmed at the end again
   (trailing newlines and spaces).
3. Match with backslashes: it is the text's last line. Drop **one**
   backslash from it. This is the escape (see Writing).

Only the comment's last line counts. A reactions line on the
`- **author** (date):` line, in the middle of the comment, or above another
reactions line is text. Before the match, the existing trim has dropped
trailing spaces after the line, so `<!-- reactions: 👍 walm --> ` is read as
reactions. A reactions line at column 0 in a comments section is the
existing parse error `unexpected line in comments section`.

The match is otherwise exact: one space after `<!--` and after `reactions:`,
one before `-->`, single spaces between emoji and names, no spaces around
`,`, `; ` between entries. So `<!--reactions: 👍 walm-->`,
`<!-- reactions: 👍  walm -->`, `<!-- reactions: 👍 walm, anna -->`,
`<!-- reactions: 👍 walm;👀 anna -->`, `<!-- reactions: 👍 Walm -->`,
`<!-- reactions: ok walm -->` and a three-space indent are all text.

**Normalising.** After a match, entries are merged by emoji key (below): the
first entry's place, the better spelling, the names in order without
repeats. So `👍 a,a,b` reads as `👍 a,b`, and `❤ a; 👀 c; ❤️ b,a` as
`❤️ a,b; 👀 c`. The writer then writes the normal form.

### Writing

```
- **author** (date): first line
  more lines…
  <!-- reactions: E1 n1,n2; E2 n3 -->     only when the comment has reactions
```

The line goes right after the text's last line, no blank line before it,
indented by two spaces. A comment with an empty text writes its header as
today (`- **walm** (2026-11-13): `, with the trailing space) and the line
under it.

**Escaping.** If the comment has **no** reactions and its text has more than
one line and the text's last line matches the regex above and parses, with
any number of backslashes, write it with one more leading backslash. When
the comment has reactions, nothing is escaped: the real line comes last, and
the text's last line is never read as one.

### Emoji identity, spelling and skin tones

Two emoji are **the same reaction** when they are equal after removing every
U+FE0E and U+FE0F (the emoji key). So `❤` and `❤️` are one reaction.

When two spellings of one key meet (reading, merging), keep the one with
**more U+FE0F**; on a tie, the one already there (in a merge: theirs, then
ours, then base). The fully qualified form renders as an emoji everywhere.

Other than that, an emoji is **kept as given**. Skin tones are different
reactions: `👍` and `👍🏽` are two. No other normalisation (no NFC, no
gender or hair variants folded).

The CLI maps what you type onto the quick set first: `❤` becomes `❤️`.

### Order and limits

- Entries are in the order the emoji were first added; names in the order
  people reacted. Removing the last name removes the entry; removing the last
  entry removes the line.
- A writer refuses a 21st different emoji on one comment
  (`task.MaxReactions` = 20). Readers, merges and validation accept any
  number, so two people adding the 20th and 21st at once never break a file.
- No limit on names.

### Quick set

The web offers 👍 ❤️ 🎉 👀 ✅ 🙏 (in this order, `❤️` with U+FE0F) and any
other emoji from a picker.

## Older clients (format 3 and before, todomd < 0.16)

An older parser reads the reactions line as the comment's last text line
(it's an indented continuation line) and writes it back byte for byte.
Checked: 3,000 random format-4 files from `internal/todogen` (13,691 comments
with reactions) and every `testdata/reactions/*.out.md` are unchanged through
the v0.15.0 and v0.14.0 parser and writer; after those versions add a tag to
every card, the new parser reads every reaction, comment, description,
sticker and link back unchanged.

What older clients do with it:

- `show` and the TUI print the line at the end of the comment.
- Adding a comment keeps it; editing a comment's text (TUI editor) keeps the
  line if it stays the last line.
- Sync: todomd.online must send a CLI older than 0.16 to whole-file sync
  while the project has reactions (the same guard as for stickers and
  links): an old CLI's `comment.set` would carry the line as text, and a
  format-4 writer would escape it.
- The other way round, a todomd ≥ 0.16 talking to a server on format 3 or
  less sends the whole file instead of ops whenever the base or the local
  file has any reactions (`cloudsync`), because the old ops reader rejects
  the `reactions` key and holds the line as comment text.

## Model and validation

`Comment.Reactions []Reaction`, `Reaction{Emoji string; By []string}`; nil
for none.

`merge.CheckTask` (and the server's "can be written and read back" check)
rejects an invalid emoji, the same emoji key twice on one comment, an entry
with nobody on it, an invalid name, and a name twice on one emoji.

## Sync ops (wire format)

No new op names. Reactions ride on the comment object, wherever comments
appear (`comment.add`, `comment.set`, `task.add`):

```json
{"author":"anna","date":"2026-11-12","text":"Can you look?",
 "reactions":[{"emoji":"👍","by":["walm","anna"]},{"emoji":"👀","by":["agent"]}]}
```

- `reactions` is present **only when the comment has reactions**, after
  `text`. Comments without them have no key, so those ops are
  byte-identical to format 3.
- Each item is `{emoji, by}` in that order, both required, no other keys.
- The decoder accepts `[]` as none, and rejects an invalid emoji, an emoji
  key twice, an empty or repeated `by`, an invalid name, and anything that
  isn't an array (`null` included).

`Delta`: a reaction change on an existing comment makes the comment list
differ other than by appending, so it goes as `comment.set` with the whole
list. That's fine: the server doesn't apply a `comment.set` blindly, it
three-way merges the result against head (sync spec §6), where reactions
merge as a set (below).

## Merge

Comments merge as before, by `(author, date, text)`: **reactions are not
part of a comment's identity**, so a reaction is never a comment edit and
never a conflict.

Then each comment of the merged list gets merged reactions:

1. Find the comment on each side by `(author, date, text)` and occurrence
   (the n-th copy of a repeated comment meets the n-th copy).
2. A side without the comment counts as unchanged from the base; with no
   base copy either, as having none.
3. Merge the sets of `(emoji key, name)` pairs with the tags rule:
   `(base ∪ addedOurs ∪ addedTheirs) − removedOurs − removedTheirs`, in
   theirs' order with ours' new pairs appended. Without a base copy (or a
   baseless merge), the union, theirs first. `NoDeletesFromOurs` keeps
   pairs ours removed, as for tags.
4. Group the pairs by emoji key at the first pair's place; each emoji gets
   the spelling rule above over the three sides.

So two people reacting at once both count, one person's un-react and
another's react both apply, and both adding the same reaction gives it once.

- A reaction change counts as an edit for delete-vs-edit, so the card is
  kept (as for stickers).
- **Known gap:** if one side edits a comment's text while the other reacts
  to it, the comment's identity changed, so the reaction made on the old
  text is lost (whichever text wins). Rare, and nothing worse than a
  missing emoji.

## Events

`changes.Diff` (`todomd changes`/`watch`) reports, in `task_updated`,
`fields.reactions: {old, new}` when reactions changed on comments that were
already there (the comment list otherwise unchanged or only appended to).
Each is a list of `{comment, emoji, by}` for the changed comments only;
`comment` is the 1-based comment number. When the comment list was edited
mid-list, `fields.comments` (the counts) is reported instead, as before. A
`comment_added` event's comment carries its reactions.

**Unread:** a `task_updated` with nothing but `reactions` doesn't mark the
card unread (TUI). `changes`/`watch` still report it, so an agent can see
that someone reacted ✅ to its plan.

## Decisions

- **The last line of the comment, indented**: an unindented line is a parse
  error for older versions, and an indented one is ordinary comment text
  they keep byte for byte. The last line, so it sits "under" the comment and
  never shifts the text's own lines.
- **Exact match only**, no whitespace or case variants, so Go and
  JavaScript can't disagree. Hand-written duplicates are normalised.
- **Escape only when the comment has no reactions**, mirroring stickers.
- **Emoji kept as given**, only variation selectors ignored for identity;
  skin tones are separate reactions.
- **Lowercase ASCII names**: handles are already lowercase; agents' names
  get lowercased by the CLI.
- **Reactions on the comment object** rather than a new op: no new op for
  the server to learn, and the set merge happens where all merges happen.

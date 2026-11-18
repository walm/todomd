# Card assignees: file format, sync and merge (format version 5)

**Status: proposal (card v1do), for review. Nothing here is implemented
yet.** Once it's agreed, this becomes the reference, as for links, stickers and
reactions. The Go code (`internal/task`, `internal/markdown/assignee.go`,
`internal/ops`, `internal/merge`, `internal/changes`) will implement it, and
todomd.online's `packages/todofile` must match it byte for byte. The shared
golden files will be in `testdata/assignees/`.

## The field

A card has **at most one assignee**: a handle, or none.

- **One, not several.** The user asked for "a specific user". One owner
  also keeps three things simple. The agent rule ("unassigned or assigned to
  you") has a single answer. The merge is a scalar, not a set. The card face
  shows one avatar. If cards ever need several assignees, a later format can
  allow a list in the same line. Format-5 readers will keep such a line as
  description text, losslessly (see Reading).
- **Handle** uses the @mention rule, `^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`
  (`internal/mentions`, `shared/mentions.ts`). So "assigned to me" and
  "mentions me" compare the same strings. Writers lowercase it and drop a
  leading `@`. Agent names such as `agent` and `claude` are valid handles.
  The CLI rejects a `--as` name that isn't a valid handle (`my_agent`) as an
  assignee.
- **Not a project member** (someone who left, an agent, a typo): the file
  accepts it. Membership changes, and the file is the source of truth. The
  web shows such an assignee as a plain initial with the title "not a member
  of this project". Its picker offers members first, then the other names on
  the board (assignees, comment authors), then free text. The CLI doesn't
  check membership: it works offline and has no members API.
- **Local-only files** (no online project) work the same way, with any valid
  handle. `me` is resolved by the CLI (see CLI).

## In TODO.md

```markdown
### Migrate attachments to R2 lifecycle rules
<!-- id:r209 -->
`#infra` **priority:** high **due:** 2026-11-14

<!-- sticker: fire -->

<!-- assignee: anna -->

Move the attachment bucket to lifecycle rules.
```

### Options considered

| | Syntax | For | Against |
|---|---|---|---|
| A | `` `#infra` **priority:** high **assignee:** @anna `` on the metadata line | Reads best. It sits next to the other card fields. | **Breaks every older client** (tested below). The whole metadata line stops parsing, so old CLIs and today's web lose tags, priority, due *and* the sticker. An old CLI that sets the priority then writes a second metadata line above it. |
| B | `<!-- assignee: anna -->` on its own line, after the sticker | Same pattern as the sticker. Older clients keep it byte for byte (tested). An exact match, so it can't be confused with prose. Hidden when rendered, like the id and the sticker. | Not visible on GitHub's rendered view. |
| C | `**assignee:** @anna` on its own line, after the sticker | Visible when rendered. Older clients keep it byte for byte (tested). | Looks like a metadata token, so people will type it on the metadata line, which is option A's breakage. A description that starts with someone's own "**Assignee:** …" convention needs escaping. The web renders `@anna` there as a mention chip in older builds. |
| D | `` `#@anna` `` as a tag | Older clients would show it as a tag. | `@` isn't a tag character (`` ^`#([a-z0-9_-]+)`$ ``), so it breaks the metadata line just like A. |

**Recommendation: B.** It's the only form besides C that older clients keep
intact, and it has none of C's lookalike traps. The CLI, web and app show the
assignee as a field, so the raw line rarely needs to be visible.

### Reading

After the id comment, the optional metadata line and the optional sticker
line, take the **next non-blank line** outside a column-0 code fence ("blank"
as the description's rule defines it). Match it against

```
^(\\*)<!-- assignee: ([a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?) -->$
```

1. No match: there is no assignee. The line is description text as before.
2. A match with **no** backslash: this is the card's assignee. The line is
   consumed, and the description starts after it (leading blank lines are
   trimmed, as always).
3. A match with backslashes: this is the description's first line. Drop
   **one** backslash. This is the escape.

The match is exact, as for stickers: lowercase, single spaces, nothing
before or after, no `@`. So `<!-- assignee: @anna -->`,
`<!-- assignee: Anna -->`, `<!-- assignee: anna,bob -->`,
`<!--assignee:anna-->` and `<!-- assignee: anna --> ` (trailing space) are
all description text. Only that one position counts. An assignee line before
the sticker line makes the sticker line description text, which matches what
format-3 and format-4 parsers do.

### Writing

```
### Title
<!-- id:xxxx -->
[metadata line]
[\n<!-- sticker: NAME -->]
[\n<!-- assignee: HANDLE -->]         only when the card has an assignee
[\n description lines]
[\n#### Links\n\n…]
[\n#### Comments\n\n…]
```

The line goes after a blank line and after the sticker, so to an older
parser it's the first description line in exactly the bytes its writer
produces. Putting it **after** the sticker means v0.15–v0.19 still see the
sticker.

**Escaping.** If the card has **no** assignee and the description's first
line matches the regex with any number of backslashes, write it with one
more backslash. One change to the existing sticker escape is needed: escape a
sticker-looking first line only when the card has neither a sticker **nor an
assignee**. Otherwise the reader would no longer find that line at the
sticker position, so it wouldn't unescape it, and the line wouldn't
round-trip.

## Compatibility today (tested)

All tests ran on throwaway files in a temp directory. The fixture has three
cards: one with tags, priority, due, a sticker, a description, links and a
comment; one with nothing else; and one with tags and a description. I built
CLIs from the tags v0.19.0, v0.14.0 (before stickers) and v0.12.0 (before
links). For the web I bundled todofile from todomd-online `main` (format 4,
ce05c3e) and ran it in node.

**Reading, v0.19.0 `show aa01 --json`:**

```
A: tags=[] priority=normal due=None sticker=None
   description='`#infra` **priority:** high **due:** 2026-11-20 **assignee:** @anna\n\n<!-- sticker: fire -->\n\nExport …'
B: tags=['infra'] priority=high due=2026-11-20 sticker=fire
   description='<!-- assignee: anna -->\n\nExport the board as CSV.'
C: tags=['infra'] priority=high due=2026-11-20 sticker=fire
   description='**assignee:** @anna\n\nExport the board as CSV.'
D: like A
```

**Editing with old CLIs.** Each version ran `update --priority low`,
`update --title`, `update --tag web --tag ui`, `comment` and `move`. Then I
diffed the result against the original:

- **B and C, all of v0.19.0, v0.14.0 and v0.12.0:** only the edited lines
  changed (`priority`, the title, the tags, the new comment). Every assignee
  line was byte-identical, on all three cards.
- **A, all three versions:** `update --priority low` added a **new** metadata
  line, `**priority:** low`, above the old one. A format-5 parser would then
  read the old line (tags, due, assignee) as description text. That's data
  loss.
- **`update --desc` (B and C):** it replaces the description and so **drops
  the assignee**, the same way it drops stickers and links on older CLIs.
  The next sync then pushes that as an unassign. No guard can tell it apart
  from a real unassign, and it's the same accepted loss as for stickers.

**todofile on main (format 4):**

```
A: strict parse ok; write(parse(x)) === x: false
B: strict parse ok; write(parse(x)) === x: true;  checkTask accepts
C: strict parse ok; write(parse(x)) === x: true;  checkTask accepts
```

**Today's server merge, with B.** A new client changes `anna → bob` while
the web edits the same card's description body and priority:

```
merge(...)  conflicts=[]  →  prio=low, desc="<!-- assignee: bob -->\n\nExport the board as CSV and JSON."
delta(...)  [{"op":"task.update","id":"aa01","set":{"description":"<!-- assignee: bob -->\n\nExport the board as CSV."}}]
events(...) [{"type":"task_updated","fields":["description"]}]
bob || carl        → carl kept, conflict ["text","description","theirs"] (a whole-description conflict comment)
bob || unassign    → unassigned, conflict ["text","description","theirs"]
```

So today's server keeps the field through merges as description text.
Concurrent changes to the assignee become description conflicts. Format 5
replaces that with the rules under Merge.

**Not run:** the web UI in a browser. It renders descriptions with
react-markdown, rehype-sanitize and no rehype-raw, so an HTML comment
shouldn't show. Today's Expo app wasn't run either (see Rollout).

### What changes

**Go (`internal/…`):**

- `Task.Assignee string`, where `""` means none.
- `markdown/assignee.go` holds the reader and writer above, plus the change
  to the sticker escape. `FormatVersion = 5` and
  `AssigneesFormatVersion = 5`.
- `merge.CheckTask` rejects a handle that doesn't match the rule.
- Ops, merge and `changes`, as below.
- `cloudsync` sends the whole file when the server's format is below 5 and
  the base or local file has any assignee. This is the same guard as
  `hasStickers` and `hasReactions`.
- `todogen` generates assignees, and `testdata/assignees/` holds the golden
  files.

**todofile (TS):**

- `Task.assignee: string | null`.
- Parse, write and escape, byte-identical with Go against the goldens.
- `FORMAT_VERSION = 5`.
- `validate.checkTask`, `ops`/`delta`/`validateOps`, `merge` and `events`,
  as below.

**Server:** `ASSIGNEES_CLI_VERSION = '0.20.0'` and `SyncRequest.assignees`,
reported as `assignees` in the project JSON. A todomd CLI older than that
syncs whole files while the project has any assignee, the same guard as for
stickers.

## Sync ops (wire format)

There are no new op names.

- `task.update.set.assignee` is a handle, or `null` to unassign. It goes
  after `sticker` and before `description`.
- `task.add.task.assignee` is present **only when set**, after `sticker`. A
  card without an assignee has no key, so its ops are byte-identical to
  format 4.
- The decoder accepts a valid handle or `null`. It rejects `""`, `"@anna"`,
  `"Anna"` and anything that isn't a string.
- `Delta` emits `set.assignee` when the assignee differs.

## Merge

The assignee is a scalar. `pick(base, ours, theirs)` behaves as follows:

| Case | Result |
|---|---|
| Only one side changed it | That side's value |
| Both set the same handle | That handle |
| **Both set different handles** (two people assign at once) | **theirs**, a field conflict `assignee`, and a todomd-sync comment: `Sync conflict — assignee on this machine was: @bob` |
| **One side unassigned, the other set a new handle** | **the new handle**, with no conflict |
| Baseless | theirs if set, else ours |
| `NoDeletesFromOurs` | an unassign on ours doesn't apply (as for tags) |

- On the server, *theirs* is head: the write that reached todomd.online
  first. A claim race between two agents therefore goes to whoever synced
  first. The other one gets a conflict, which it sees on its next read (the
  agent rule says to check).
- **Set beats clear.** "Not me any more" is weaker than "give it to bob". If
  clear won, walm's reassignment could silently vanish because anna
  unassigned herself at the same moment.
- An assignee change counts as an edit for delete-vs-edit, so the card is
  kept.

## Events

`changes.Diff` (`todomd changes`/`watch`) adds `fields.assignee: {old, new}`
to `task_updated`. Each value is a handle or `null`. Every event also carries
`assignee`, the card's assignee after the change (before it, for
`task_deleted`). The key is left out when there is none, as `mentions` is.

```json
{"type":"task_updated","task":"r209","title":"Migrate attachments to R2 lifecycle rules",
 "board":"In Progress","assignee":"agent","fields":{"assignee":{"old":null,"new":"agent"}}}
```

`task_added`'s `detail`, `show --json` and `list --json` get
`"assignee": "anna"` or `null`, as for `sticker`.

**Filtering.** `changes` and `watch` take `--ignore-assigned-to-others <me>`
(repeatable). It drops events about cards that are assigned to someone other
than `<me>`. It keeps:

- events on unassigned cards;
- events where the assignee changed from or to `<me>`;
- a `comment_added` that @mentions `<me>`.

An assignee-only `task_updated` marks the card unread in the TUI.

## CLI (phase 2)

```sh
todomd assign r209 @anna          # @ optional; replaces any assignee
todomd assign r209 me --as agent  # me = you
todomd unassign r209              # clear
todomd unassign r209 agent        # clear only if it's agent (no-op otherwise)
todomd add "Title" --assignee anna
todomd update r209 --assignee none
todomd list --assignee me|<handle>|none|any --json
```

**`me`** is the `--as` name (or `TODOMD_CURSOR`), else the logged-in handle
(`whoami`). Without either, it's an error that suggests `--as`. Human `list`
output shows `@anna` after the tags.

## Agent rule

This is new rule 1 in `internal/cli/skill/SKILL.md` under **Board rules**.
The others move down one.

> 1. **Whose card**: work on cards that are unassigned or assigned to you
>    (your `--as` name). A card assigned to someone else is theirs: don't
>    start it, move it or answer on it, unless they or the board's owner ask
>    you to (an `@mention` of you counts). When you start an unassigned card,
>    assign it to yourself first (`todomd assign <id> me --as agent`), run
>    `todomd watch --as agent --no-wait --json`, and check with
>    `todomd show <id> --json` that it is still yours. If someone else got
>    it, leave it. If you stop working on a card, `todomd unassign <id> agent`.
>    `watch --ignore-assigned-to-others agent` drops events on other
>    people's cards.

The existing sentence above the rules already says that board and user rules
win. For an example of an override, the README and `web/src/docs/agents.md`
will show a "Rules for agents" card saying *"Only work on cards assigned to
@agent."*

The rule also goes in these places:

- the README's Agents paragraph on default rules, and its file-format list
  (one bullet that links here);
- `web/src/lib/agent-prompt.ts`, the default rules in "Connect an agent";
- `web/src/docs/agents.md`, under "Change the rules", with the override
  example;
- `web/src/docs/cli.md`, which lists the commands.

## Rollout

Every step is safe on its own, because each older reader keeps the line as
text (tested above). The steps:

1. **todomd v0.20.0:** the model, parser and writer, goldens, ops, merge,
   `changes`, the CLI, the skill and the README. Against today's server
   (format 4), `cloudsync` sends the whole file whenever a card has an
   assignee. The server keeps the line as description text, and the
   round-trip is byte for byte.
2. **todomd.online:** todofile ported from the goldens (`FORMAT_VERSION = 5`),
   ops, merge, events, and the guard with `ASSIGNEES_CLI_VERSION = '0.20.0'`.
   The web gets the picker, the avatar and the "Mine" filter. These can ship
   in one deploy. From then on, CLIs at v0.19 or older sync whole files on
   projects with assignees. Without that, their `set.description` would
   carry the line as text, and a format-5 writer would escape it into a
   visible duplicate.
3. **Apps:** the server's guard matches only `todomd/x.y.z` User-Agents. An
   installed Expo build with format-4 todofile will receive `assignee` in
   ops. Its `apply` then fails the checksum check, and it falls back to
   whole-file sync, so nothing is lost, but the gap is untidy. Ship an app
   build with the new todofile in step 2. The Bitrig app must carry
   `assignee` from its first release.

Between steps 1 and 2, a v0.20 CLI's assignees reach todomd.online as
description text. The web hides that text (an HTML comment), and step 2's
parser turns it back into the field. Nothing is lost.

## Open questions

- Syntax B (hidden, like the sticker) or C (visible `**assignee:** @anna`)?
- When two people assign different handles at once, should the losing side
  get a conflict comment, or should theirs win silently as stickers do?
- Should agents self-assign when they start an unassigned card (proposed),
  or only read the field?
- Should assigning someone notify them the way an @mention does (web push,
  the @ badge)?
- Is `me` reserved in CLI arguments? A todomd.online user whose handle is
  `me` would then need `@me`.

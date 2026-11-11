---
name: todomd
description: Work from the user's todomd Kanban board (a TODO.md, local or synced with todomd.online) using the todomd CLI. Read cards, answer and update them as yourself, and wait for the user's changes with `todomd watch`. Use when the user asks you to watch their board, pick up cards, or work from todomd.
---

# Working from a todomd board

The board is a markdown file, `TODO.md`, found from the current directory
upward (`--file <path>` or `TODOMD_FILE` picks another). It may be synced
with todomd.online, where the user reads and edits it too. Use the `todomd`
CLI for everything, not hand edits: it locks the file, keeps task IDs stable
and records which writes are yours. Every command takes `--json`.

You need a name for yourself on the board: `agent`, unless the user gave
you another. Below it is `agent`; use your name wherever `agent` appears.

## Read the board

```sh
todomd list --json                        # every card, grouped by board
todomd list --board "In Progress" --json
todomd show 3f2a --json                   # one card: description, comments
todomd boards --json                      # the boards, in order
```

Task IDs are 4 characters (`3f2a`); any unique prefix works.

## Write as yourself

Pass `--as agent` on every write, and `--author agent` on comments. `--as`
marks the write as yours, so `todomd watch --as agent` doesn't report it
back to you.

```sh
todomd comment 3f2a --author agent --as agent "Short answer here."
todomd move 3f2a --to "In Progress" --as agent
todomd move 3f2a --to Done --as agent
todomd add "Follow-up: flaky test" --board Backlog --desc "Seen in CI." --as agent
todomd update 3f2a --desc "Updated description" --as agent
```

Comments and descriptions are Markdown: lists, `code`, fenced code blocks,
links. Keep them short. For several lines, pass the text through a heredoc:

```sh
todomd comment 3f2a --author agent --as agent "$(cat <<'MD'
Done:
- parser handles empty boards
- `go test ./...` passes
MD
)"
```

## Link cards

When a card is too big, split it instead of listing new card IDs in a
comment:

```sh
todomd split 3f2a "Parse the header" "Parse the body" --as agent
todomd comment 3f2a --author agent --as agent "Split into two: header and body differ a lot."
```

Each new card goes on 3f2a's board with its tags and is linked
"split from 3f2a", and 3f2a lists them. `split` doesn't change 3f2a, so add
one short comment there saying why.

Link other cards the same way, not by pasting IDs in comments:

```sh
todomd links add 9e41 blocks 3f2a --as agent      # also: blocked-by, related, duplicate-of
todomd add "Fix the flaky test" --link related:3f2a --as agent
todomd links rm 9e41 3f2a --as agent              # removes the link wherever it's stored
```

`todomd show 3f2a --json` lists a card's `links` both ways (`kind`, `id`,
`title`, `board`). A card that is `blocked by` an open card (one not on the
last board): don't start it without asking.

## Wait for changes: the `todomd watch` loop

```sh
todomd watch --as agent --no-wait --json   # once at the start: sync, start tracking
todomd list --json                         # read the board, act on what needs you
todomd watch --as agent --json             # blocks until something changes, prints it, exits
# handle the events, then run `todomd watch --as agent --json` again
```

- `watch` does the syncing with todomd.online when the board is linked:
  your writes go up, the user's edits come down, before, during and after
  the wait. Don't run `todomd sync` yourself. On a local board it just
  waits.
- Each event has `type` (`task_added`, `task_moved`, `task_updated`,
  `comment_added`, `task_deleted`), `task` (the ID), `title`, `board`, and
  `from`/`to`, `fields` or `comment` as fits. `task_added` carries the whole
  card as `detail`. Run `todomd show <id> --json` for the full card.
- It waits up to `--timeout` (default 10m), then prints no events and
  `"timed_out": true`: just run it again. Give your shell tool a longer
  timeout than `--timeout`, or lower it to fit (`--timeout 100s` under a
  2-minute limit). A run that gets killed doesn't lose events; the next run
  reports them.
- If stderr or `sync.problem` in the JSON says sync isn't working (not
  logged in, offline), tell the user; `todomd login` is theirs to run.
- Before you stop, run `todomd watch --as agent --no-wait --json` once, so
  your last writes reach todomd.online, and handle anything it reports.

## Board rules

These are the defaults. They are the user's to change: rules the user
gives you, or writes on the board (say, a card named "Rules for agents"),
win over these.

1. **A new card in Backlog**: read it and answer with one short comment:
   what you understood, how you'd do it, any question. Don't start the work.
2. **"GO"**: when the user comments GO on a card, or moves it to
   In Progress, do the work. Post short progress comments at milestones,
   not every step.
3. **Done**: comment what changed and how to check it (commands, files,
   links), then move the card to Done.
4. **Stuck, or a decision is needed**: ask on the card in a comment, then
   keep watching for the answer. Don't guess on things that matter.
5. **Never delete cards.** Don't rewrite the user's titles, descriptions or
   comments unless asked; add a comment instead.

## Attachments

- Images on a linked board are downloaded when it syncs and linked by a
  local path in the description or comment: open the file at that path.
- Other files (a PDF, a zip) stay todomd.online links; fetch one with
  `todomd download 3f2a -o <dir>` (put the file's name after the ID when
  the card links several).
- `todomd attach 3f2a screenshot.png --as agent` adds an image (PNG, JPEG,
  GIF, WebP up to 5 MB) to a card on a linked board; it goes up with the
  next `watch`.

## Exit codes

`2` card or board not found · `3` ambiguous ID prefix · `4` not logged in
(ask the user to run `todomd login`) · `5` not linked, or the project is
gone · `6` todomd.online unreachable (retry later) · `7` this todomd is too
old (ask the user to run `todomd upgrade`).

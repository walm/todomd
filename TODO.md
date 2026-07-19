# todomd

## Backlog

### Optional: fsnotify auto-reload in TUI
<!-- id:8uf8 -->
`#tui`

### Example: rich task with everything
<!-- id:3d66 -->
`#example` `#docs` `#parser` **due:** 2026-08-15

This is an example task showing everything the format supports.

Markdown works throughout: **bold**, *italics*, `inline code`, and lists:

- parse the file with a line-based parser
- keep descriptions verbatim
- escape structural lines so nothing breaks

Code fences render too, and headings inside them are safe:

```go
func Parse(data []byte) (*task.File, error) {
    // ## this heading is shielded by the fence
    return parse(data)
}
```

A second paragraph after the code block, long enough to wrap around inside
the modal so you can see how longer prose behaves when the box is at its
maximum width and the content flows over multiple lines.

#### Comments

- **andreas** (2026-07-19): Looks good to me — can we make the due date stand out a bit more on the card?
- **ai** (2026-07-19): Sure. The card already colors it: red when overdue, yellow within 3 days.
  Comments can span multiple lines too — continuation lines are indented in the file.

## In Progress

### Package & release: goreleaser, homebrew tap
<!-- id:360k -->
`#release`

## Done

### Ship v1
<!-- id:c4q5 -->

#### Comments

- **ai** (2026-07-19): Core, CLI, and TUI implemented with full test coverage; plan reviewed by a sub-agent and all 18 findings resolved.

### Optional: board management commands (rename/reorder)
<!-- id:y96v -->
`#cli`

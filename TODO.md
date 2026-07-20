# todomd

## Backlog

### Add board management commands (rename/reorder)
<!-- id:y96v -->
`#cli`

### Publish a Homebrew tap
<!-- id:hb01 -->
`#release`

### Example: a task showing everything the format supports
<!-- id:ex01 -->
`#example` `#docs` **due:** 2026-12-31

Descriptions are verbatim markdown: **bold**, _italics_, `inline code`,
and lists all work:

- boards are `##` headings, tasks are `###` headings
- the HTML comment above holds the task's stable id
- tags and the due date live on the line under it

Code fences are safe too — structural-looking lines inside them are
just content:

```go
func Parse(data []byte) (*task.File, error) {
    // ## this heading is shielded by the fence
    return parse(data)
}
```

#### Comments

- **user** (2026-07-20): Comments capture the conversation on a task, from
  humans and agents alike.
- **ai** (2026-07-20): And they can span multiple lines — continuation
  lines are simply indented in the file.
  Agents add these with `todomd comment <id> --author ai "..."`.

## In Progress

## Done

### Ship v0.1.0
<!-- id:c4q5 -->
`#release`

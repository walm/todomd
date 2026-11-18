# TODO

## Backlog

### Escaped, no sticker
<!-- id:dd01 -->

\<!-- assignee: anna -->

The first line is text that looks like an assignee line.

### Escaped twice
<!-- id:dd02 -->

\\<!-- assignee: anna -->

### Escaped after a sticker
<!-- id:dd03 -->

<!-- sticker: star -->

\<!-- assignee: bob -->

Text.

### Escaped under a real assignee: no unescape
<!-- id:dd04 -->

<!-- assignee: anna -->

\<!-- assignee: bob -->

### Assignee-looking first line under a real assignee
<!-- id:dd05 -->

<!-- assignee: anna -->

<!-- assignee: bob -->

The second assignee line is description text.

### Sticker-looking first line under an assignee
<!-- id:dd06 -->
`#x`

<!-- assignee: anna -->

<!-- sticker: fire -->

It's past the sticker's place, so it's text and isn't escaped.

### Escaped sticker-looking first line under an assignee
<!-- id:dd07 -->

<!-- assignee: anna -->

\<!-- sticker: fire -->

### Escaped sticker, no assignee
<!-- id:dd08 -->

\<!-- sticker: fire -->

<!-- assignee: anna -->

The assignee line after an escaped sticker is description text.

### Escaped metadata line under an assignee
<!-- id:dd09 -->

<!-- assignee: anna -->

\`#tag` **priority:** high

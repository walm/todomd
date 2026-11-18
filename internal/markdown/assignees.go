package markdown

import (
	"regexp"

	"github.com/walm/todomd/internal/task"
)

// A card's assignee is one hidden line, the first non-blank line after the
// sticker line (or the metadata line, or the id comment):
//
//	### Title
//	<!-- id:abcd -->
//	`#tag` **priority:** high
//
//	<!-- sticker: fire -->
//
//	<!-- assignee: anna -->
//
//	Description…
//
// The line counts only when it is exactly `<!-- assignee: HANDLE -->` with a
// valid handle. Anything else stays description text, so files written
// before assignees existed read and write back unchanged. The writer puts a
// blank line before it, and after the sticker, so older todomd versions keep
// it as the description's first line byte for byte and still see the
// sticker.
//
// When a card has no assignee and its description's first line would read
// as one, the writer adds a leading backslash and the reader drops it again.
// See docs/card-assignees-format.md.
var assigneeLineRe = regexp.MustCompile(`^(\\*)<!-- assignee: ([a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?) -->$`)

// assigneeLine reads an assignee line, escaped or not: the handle and the
// number of leading backslashes.
func assigneeLine(line string) (handle string, backslashes int, ok bool) {
	m := assigneeLineRe.FindStringSubmatch(line)
	if m == nil || !task.ValidHandle(m[2]) {
		return "", 0, false
	}
	return m[2], len(m[1]), true
}

func writeAssigneeLine(handle string) string { return "<!-- assignee: " + handle + " -->" }

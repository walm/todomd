package markdown

import (
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// A comment's reactions are one hidden line, the comment's last line, a
// continuation line like any other:
//
//	#### Comments
//
//	- **anna** (2026-11-13): Can you look at the export?
//	  Second line.
//	  <!-- reactions: 👍 walm,anna; 👀 agent -->
//
// The line counts only when it is the comment's last line, isn't its first
// (the `- **author** (date):` line), and reads exactly as reactionsLine
// accepts it. Anything else stays comment text, so todomd < 0.16, which
// reads the line as the comment's last text line, writes it back byte for
// byte.
//
// When a comment has no reactions and its text's last line (not its first)
// would read as a reactions line, the writer adds a leading backslash and
// the reader drops it again. See docs/comment-reactions-format.md.
var reactionsLineRe = regexp.MustCompile(`^(\\*)<!-- reactions: (.+) -->$`)

// reactionsLine reads a reactions line, escaped or not: the reactions,
// normalised (see task.NormalizeReactions), and the number of leading
// backslashes.
func reactionsLine(line string) (rs []task.Reaction, backslashes int, ok bool) {
	m := reactionsLineRe.FindStringSubmatch(line)
	if m == nil {
		return nil, 0, false
	}
	for _, entry := range strings.Split(m[2], "; ") {
		emoji, names, found := strings.Cut(entry, " ")
		if !found || !task.ValidEmoji(emoji) {
			return nil, 0, false
		}
		r := task.Reaction{Emoji: emoji}
		for _, n := range strings.Split(names, ",") {
			if !task.ValidReactor(n) {
				return nil, 0, false
			}
			r.By = append(r.By, n)
		}
		rs = append(rs, r)
	}
	return task.NormalizeReactions(rs), len(m[1]), true
}

func writeReactionsLine(rs []task.Reaction) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.Emoji + " " + strings.Join(r.By, ",")
	}
	return "<!-- reactions: " + strings.Join(parts, "; ") + " -->"
}

// splitReactions takes a comment's reactions line off its text, as parsed
// by a reader that doesn't know reactions, and undoes the escape.
func splitReactions(c *task.Comment) {
	k := strings.LastIndexByte(c.Text, '\n')
	if k < 0 {
		return // one line: the `- **author** (date):` line
	}
	rs, n, ok := reactionsLine(c.Text[k+1:])
	switch {
	case !ok:
	case n == 0:
		c.Reactions = rs
		c.Text = strings.TrimRight(c.Text[:k], "\n ")
	default:
		c.Text = c.Text[:k+1] + c.Text[k+2:]
	}
}

// commentLines are a comment's text lines as the writer puts them: the
// last one escaped when it would read as reactions, then the reactions
// line.
func commentLines(c task.Comment) []string {
	lines := strings.Split(c.Text, "\n")
	if last := len(lines) - 1; last > 0 {
		if _, _, ok := reactionsLine(lines[last]); ok && len(c.Reactions) == 0 {
			lines[last] = `\` + lines[last]
		}
	}
	if len(c.Reactions) > 0 {
		lines = append(lines, writeReactionsLine(c.Reactions))
	}
	return lines
}

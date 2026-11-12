package markdown

import (
	"regexp"

	"github.com/walm/todomd/internal/task"
)

// A card's sticker is one hidden line, the first non-blank line after the
// metadata line (or the id comment when there is none):
//
//	### Title
//	<!-- id:abcd -->
//	`#tag` **priority:** high
//
//	<!-- sticker: fire -->
//
//	Description…
//
// The line counts only when it is exactly `<!-- sticker: NAME -->` with a
// known name. Anything else, an unknown name included, stays description
// text, so files written before stickers existed read and write back
// unchanged. The writer puts a blank line before it, where older todomd
// versions put the blank line before a description, so they keep the line
// as description text byte for byte.
//
// When a card has no sticker and its description's first line would read as
// one, the writer adds a leading backslash and the reader drops it again.
// See docs/card-stickers-format.md.
var stickerLineRe = regexp.MustCompile(`^(\\*)<!-- sticker: ([a-z]+) -->$`)

// stickerLine reads a sticker line, escaped or not: the name and the number
// of leading backslashes.
func stickerLine(line string) (name string, backslashes int, ok bool) {
	m := stickerLineRe.FindStringSubmatch(line)
	if m == nil || !task.ValidSticker(m[2]) {
		return "", 0, false
	}
	return m[2], len(m[1]), true
}

func writeStickerLine(name string) string { return "<!-- sticker: " + name + " -->" }

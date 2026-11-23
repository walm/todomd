package markdown

import (
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// todomd.online hands a client below format 6 (todomd v0.22 and older) a
// card's start date as a hidden first description line,
//
//	<!-- start: 2026-11-10 -->
//
// because those clients read a metadata line with `**start:**` as plain
// description text. A TODO.md that such a client pulled still holds the line.
// UpgradeStart turns it back into the card's start date, so upgrading
// loses nothing. See docs/card-start-format.md.

var startCompatRe = regexp.MustCompile(`^<!-- start: (\d{4}-\d{2}-\d{2}) -->$`)

// UpgradeStart makes each card's `<!-- start: DATE -->` first description
// line (and the blank lines after it) the card's start date, in place, and
// reports whether it changed anything. A card that already has a start keeps
// its description as it is. A backslash-escaped line is left alone: the
// writer doesn't escape such lines (the server does, on the way out to old
// clients), so unescaping one here would turn real text into a start date
// on the next read.
func UpgradeStart(f *task.File) bool {
	changed := false
	for _, t := range f.AllTasks() {
		if t.Start != nil || t.Description == "" {
			continue
		}
		lines := strings.Split(t.Description, "\n")
		m := startCompatRe.FindStringSubmatch(lines[0])
		if m == nil {
			continue
		}
		d, err := task.ParseDate(m[1])
		if err != nil {
			continue
		}
		k := 1
		for k < len(lines) && isBlank(lines[k]) {
			k++
		}
		t.Start = &d
		t.Description = strings.Join(lines[k:], "\n")
		changed = true
	}
	return changed
}

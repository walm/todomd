package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// ParseTaskStrict parses a WriteTask fragment the way todomd.online checks a
// card: strictly (the task must carry its own well-formed id), and the
// fragment must hold exactly one board and one task.
func ParseTaskStrict(data []byte) (*task.Task, error) {
	const wrapper = "# t\n\n## b\n" // 3 lines before the fragment
	f, err := ParseStrict([]byte(wrapper + string(data)))
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.Line -= 3
		}
		return nil, err
	}
	tasks := f.AllTasks()
	if len(tasks) != 1 || len(f.Boards) != 1 {
		return nil, &ParseError{Line: 1, Msg: fmt.Sprintf("fragment must contain exactly one task ('### title'), found %d", len(tasks))}
	}
	return tasks[0], nil
}

var markerRe = regexp.MustCompile("^(<<<<<<< |>>>>>>> )|^=======[ \t]*$")

// ConflictMarkerLine returns the 1-based line of the first git conflict
// marker outside a column-0 code fence, or 0 if there is none. A file in
// that state is mid-merge: its markers would parse as description text, so
// sync refuses it (and todomd.online rejects it with 422 conflict_markers).
func ConflictMarkerLine(text string) int {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var delim byte
	var delimLen int
	for i, l := range lines {
		if delim == 0 {
			if m := fenceOpenRe.FindString(l); m != "" {
				delim, delimLen = m[0], len(m)
				continue
			}
			if markerRe.MatchString(l) {
				return i + 1
			}
		} else {
			t := strings.TrimRight(l, " \t")
			if len(t) >= delimLen && strings.Trim(t, string(delim)) == "" {
				delim = 0
			}
		}
	}
	return 0
}

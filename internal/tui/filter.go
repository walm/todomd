package tui

import (
	"strings"

	"github.com/walm/todomd/internal/task"
)

// filter narrows what the board shows. The two halves compose: with both on,
// a card has to be unread *and* match the query.
type filter struct {
	unreadOnly bool
	query      string
}

func (f filter) active() bool { return f.unreadOnly || f.query != "" }

// describe is what the footer says while a filter is on.
func (f filter) describe() string {
	var parts []string
	if f.unreadOnly {
		parts = append(parts, "unread")
	}
	if f.query != "" {
		parts = append(parts, "/"+f.query)
	}
	return strings.Join(parts, " + ")
}

// matches reports whether a card survives the filter. The query is matched
// case-insensitively against everything a person might remember about a task:
// its id, title, tags, description and the text of its comments.
func (f filter) matches(t *task.Task, mark markKind) bool {
	if f.unreadOnly && mark == markNone {
		return false
	}
	if f.query == "" {
		return true
	}
	q := strings.ToLower(f.query)
	if strings.Contains(strings.ToLower(t.Title), q) ||
		strings.Contains(strings.ToLower(t.Description), q) ||
		strings.Contains(t.ID, q) {
		return true
	}
	for _, tag := range t.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	for _, c := range t.Comments {
		if strings.Contains(strings.ToLower(c.Text), q) ||
			strings.Contains(strings.ToLower(c.Author), q) {
			return true
		}
	}
	return false
}

// visibleTasks is the filtered contents of a board — the list the selection
// indexes into, so navigation and rendering always agree on what "card 3" is.
func (m *model) visibleTasks(bi int) []*task.Task {
	if bi < 0 || bi >= len(m.file.Boards) {
		return nil
	}
	all := m.file.Boards[bi].Tasks
	if !m.filter.active() {
		return all
	}
	out := make([]*task.Task, 0, len(all))
	for _, t := range all {
		if m.filter.matches(t, m.unread.marks[t.ID]) {
			out = append(out, t)
		}
	}
	return out
}

// matchCount totals the cards the filter leaves across every board.
func (m *model) matchCount() (shown, total int) {
	for bi, b := range m.file.Boards {
		shown += len(m.visibleTasks(bi))
		total += len(b.Tasks)
	}
	return shown, total
}

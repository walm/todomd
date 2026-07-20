package changes

import (
	"testing"

	"github.com/walm/todomd/internal/task"
)

func date(t *testing.T, s string) task.Date {
	t.Helper()
	d, err := task.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func file(boards ...*task.Board) *task.File {
	return &task.File{Title: "T", Boards: boards}
}

func board(name string, tasks ...*task.Task) *task.Board {
	return &task.Board{Name: name, Tasks: tasks}
}

func byType(evs []Event) map[string][]Event {
	m := map[string][]Event{}
	for _, e := range evs {
		m[e.Type] = append(m[e.Type], e)
	}
	return m
}

func TestDiffAll(t *testing.T) {
	d := date(t, "2026-08-01")
	old := file(
		board("Backlog",
			&task.Task{ID: "aaaa", Title: "Keep me", Tags: []string{"x"}},
			&task.Task{ID: "bbbb", Title: "Move me"},
			&task.Task{ID: "cccc", Title: "Delete me"},
			&task.Task{ID: "dddd", Title: "Comment on me",
				Comments: []task.Comment{{Author: "ai", Date: d, Text: "first"}}},
		),
		board("Done"),
	)
	cur := file(
		board("Backlog",
			// Renamed + retagged + due set: same ID, so this must be an
			// update, never delete+add.
			&task.Task{ID: "aaaa", Title: "Keep me (renamed)", Tags: []string{"x", "y"}, Due: &d},
			&task.Task{ID: "dddd", Title: "Comment on me",
				Comments: []task.Comment{
					{Author: "ai", Date: d, Text: "first"},
					{Author: "walm", Date: d, Text: "second"},
				}},
			&task.Task{ID: "eeee", Title: "Brand new"},
		),
		board("Done", &task.Task{ID: "bbbb", Title: "Move me"}),
	)
	m := byType(Diff(old, cur))

	if n := len(m[TaskUpdated]); n != 1 {
		t.Fatalf("updated = %d", n)
	}
	up := m[TaskUpdated][0]
	if up.Task.ID != "aaaa" {
		t.Errorf("updated id = %s", up.Task.ID)
	}
	if fc := up.Fields["title"]; fc.Old != "Keep me" || fc.New != "Keep me (renamed)" {
		t.Errorf("title change = %+v", fc)
	}
	if _, ok := up.Fields["tags"]; !ok {
		t.Error("tags change missing")
	}
	if fc := up.Fields["due"]; fc.Old != nil || fc.New != "2026-08-01" {
		t.Errorf("due change = %+v", fc)
	}

	if n := len(m[TaskMoved]); n != 1 || m[TaskMoved][0].From != "Backlog" || m[TaskMoved][0].To != "Done" {
		t.Errorf("moved = %+v", m[TaskMoved])
	}
	if n := len(m[TaskAdded]); n != 1 || m[TaskAdded][0].Task.ID != "eeee" {
		t.Errorf("added = %+v", m[TaskAdded])
	}
	if n := len(m[TaskDeleted]); n != 1 || m[TaskDeleted][0].Task.ID != "cccc" {
		t.Errorf("deleted = %+v", m[TaskDeleted])
	}
	if n := len(m[CommentAdded]); n != 1 || m[CommentAdded][0].Comment.Text != "second" {
		t.Errorf("comments = %+v", m[CommentAdded])
	}
}

func TestDiffNoChanges(t *testing.T) {
	f := file(board("B", &task.Task{ID: "aaaa", Title: "Same"}))
	g := file(board("B", &task.Task{ID: "aaaa", Title: "Same"}))
	if evs := Diff(f, g); len(evs) != 0 {
		t.Errorf("events = %+v", evs)
	}
}

func TestDiffReorderIsSilent(t *testing.T) {
	a := &task.Task{ID: "aaaa", Title: "A"}
	b := &task.Task{ID: "bbbb", Title: "B"}
	if evs := Diff(file(board("X", a, b)), file(board("X", b, a))); len(evs) != 0 {
		t.Errorf("reorder should not report: %+v", evs)
	}
}

func TestDiffEditedCommentFallsBackToFieldChange(t *testing.T) {
	d := date(t, "2026-08-01")
	old := file(board("B", &task.Task{ID: "aaaa", Title: "T",
		Comments: []task.Comment{{Author: "ai", Date: d, Text: "original"}}}))
	cur := file(board("B", &task.Task{ID: "aaaa", Title: "T",
		Comments: []task.Comment{{Author: "ai", Date: d, Text: "edited!"}}}))
	m := byType(Diff(old, cur))
	if len(m[CommentAdded]) != 0 {
		t.Error("edited comment must not report comment_added")
	}
	if len(m[TaskUpdated]) != 1 || m[TaskUpdated][0].Fields["comments"].New != 1 {
		t.Errorf("want comments field change, got %+v", m[TaskUpdated])
	}
}

func TestCursorPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	p1, err := CursorPath("/a/TODO.md", "claude")
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := CursorPath("/b/TODO.md", "claude")
	if p1 == p2 {
		t.Error("different files must get different cursor dirs")
	}
	if _, err := CursorPath("/a/TODO.md", "../evil"); err == nil {
		t.Error("path traversal in cursor name must be rejected")
	}
}

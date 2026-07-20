package tui

import (
	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

type markKind int

const (
	markNone markKind = iota
	markUpdated
	markNew
)

// unread tracks which tasks changed since this human last saw them in the
// TUI, backed by the same snapshot-cursor mechanism agents use (cursor name
// "tui"). Viewing or touching a card syncs just that task into the
// baseline, so unseen badges on other cards survive — including across
// sessions.
type unread struct {
	path     string
	baseline *task.File
	marks    map[string]markKind
}

func loadUnread(filePath string, cur *task.File) *unread {
	u := &unread{marks: map[string]markKind{}}
	cpath, err := changes.CursorPath(filePath, "tui")
	if err != nil {
		return u // degrade to no badges rather than failing the TUI
	}
	u.path = cpath
	if data, ok, err := changes.LoadCursor(cpath); err == nil && ok {
		if base, perr := markdown.Parse(data); perr == nil {
			u.baseline = base
			u.recompute(cur)
			return u
		}
	}
	// First run (or unreadable snapshot): start tracking from now.
	u.baseline = snapshot(cur)
	_ = changes.SaveCursor(cpath, markdown.Write(cur))
	return u
}

// snapshot deep-copies a file via its canonical serialization.
func snapshot(f *task.File) *task.File {
	c, err := markdown.Parse(markdown.Write(f))
	if err != nil {
		return &task.File{}
	}
	return c
}

// recompute prunes deleted tasks from the baseline and rebuilds the marks.
func (u *unread) recompute(cur *task.File) {
	u.marks = map[string]markKind{}
	if u.baseline == nil {
		return
	}
	alive := map[string]bool{}
	for _, t := range cur.AllTasks() {
		alive[t.ID] = true
	}
	for _, b := range u.baseline.Boards {
		kept := b.Tasks[:0]
		for _, t := range b.Tasks {
			if alive[t.ID] {
				kept = append(kept, t)
			}
		}
		b.Tasks = kept
	}
	for _, e := range changes.Diff(u.baseline, cur) {
		switch e.Type {
		case changes.TaskAdded:
			u.marks[e.Task.ID] = markNew
		case changes.TaskMoved, changes.TaskUpdated, changes.CommentAdded:
			if u.marks[e.Task.ID] != markNew {
				u.marks[e.Task.ID] = markUpdated
			}
		}
	}
}

// markRead syncs one task into the baseline and persists the cursor.
// Called when the user opens a card or mutates it themselves.
func (u *unread) markRead(cur *task.File, id string) {
	if u.baseline == nil || u.marks[id] == markNone {
		return
	}
	var found *task.Task
	var boardName string
	for _, b := range cur.Boards {
		for _, t := range b.Tasks {
			if t.ID == id {
				found, boardName = t, b.Name
			}
		}
	}
	if found == nil {
		return
	}
	for _, b := range u.baseline.Boards {
		kept := b.Tasks[:0]
		for _, t := range b.Tasks {
			if t.ID != id {
				kept = append(kept, t)
			}
		}
		b.Tasks = kept
	}
	var target *task.Board
	for _, b := range u.baseline.Boards {
		if b.Name == boardName {
			target = b
		}
	}
	if target == nil {
		target = &task.Board{Name: boardName}
		u.baseline.Boards = append(u.baseline.Boards, target)
	}
	cp, err := markdown.ParseTask(markdown.WriteTask(found))
	if err != nil {
		return
	}
	target.Tasks = append(target.Tasks, cp)
	delete(u.marks, id)
	if u.path != "" {
		_ = changes.SaveCursor(u.path, markdown.Write(u.baseline))
	}
}

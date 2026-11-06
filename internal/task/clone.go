package task

import "slices"

// Clone deep-copies a task: tags, comments and the due date are never
// shared with the original.
func (t *Task) Clone() *Task {
	c := *t
	c.Tags = slices.Clone(t.Tags)
	c.Comments = slices.Clone(t.Comments)
	if t.Due != nil {
		d := *t.Due
		c.Due = &d
	}
	return &c
}

// Clone deep-copies a file.
func (f *File) Clone() *File {
	c := &File{Title: f.Title, Preamble: f.Preamble, Boards: make([]*Board, len(f.Boards))}
	for i, b := range f.Boards {
		nb := &Board{Name: b.Name, Tasks: make([]*Task, len(b.Tasks))}
		for j, t := range b.Tasks {
			nb.Tasks[j] = t.Clone()
		}
		c.Boards[i] = nb
	}
	return c
}

// SameDue reports whether two due dates are equal (both unset, or the same
// day).
func SameDue(a, b *Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

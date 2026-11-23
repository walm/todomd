package task

import (
	"fmt"
	"slices"
)

// Clone deep-copies a task: tags, links, comments (with their reactions)
// and the due date are never shared with the original.
func (t *Task) Clone() *Task {
	c := *t
	c.Tags = slices.Clone(t.Tags)
	c.Links = slices.Clone(t.Links)
	c.Comments = CloneComments(t.Comments)
	if t.Start != nil {
		s := *t.Start
		c.Start = &s
	}
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

// CheckStartDue is the writers' rule: a start date after the due date is
// refused. Readers keep such a card as it is (see docs/card-start-format.md).
func CheckStartDue(start, due *Date) error {
	if start != nil && due != nil && due.Before(*start) {
		return fmt.Errorf("start %s is after due %s", start, due)
	}
	return nil
}

// SameStart reports whether two start dates are equal.
func SameStart(a, b *Date) bool { return SameDue(a, b) }

// SameDue reports whether two due dates are equal (both unset, or the same
// day).
func SameDue(a, b *Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

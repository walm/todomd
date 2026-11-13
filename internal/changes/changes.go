// Package changes computes semantic differences between two states of a
// todo file and manages per-consumer cursor snapshots. Because it diffs
// states rather than recording intents, it sees every kind of change —
// CLI, TUI, hand edits, formatters, git pulls.
package changes

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/walm/todomd/internal/statedir"
	"github.com/walm/todomd/internal/task"
)

// Event types.
const (
	TaskAdded    = "task_added"
	TaskDeleted  = "task_deleted"
	TaskMoved    = "task_moved"
	TaskUpdated  = "task_updated"
	CommentAdded = "comment_added"
)

// FieldChange is an old/new pair for one changed task field.
type FieldChange struct {
	Old any
	New any
}

// Event is one semantic change. Task points at the current task (the former
// one for TaskDeleted); Board is where it lives now (lived, for deleted).
type Event struct {
	Type    string
	Task    *task.Task
	Board   string
	From    string // TaskMoved only
	To      string // TaskMoved only
	Fields  map[string]FieldChange
	Comment *task.Comment // CommentAdded only
}

type loc struct {
	t *task.Task
	b string
}

func index(f *task.File) map[string]loc {
	m := map[string]loc{}
	for _, b := range f.Boards {
		for _, t := range b.Tasks {
			m[t.ID] = loc{t, b.Name}
		}
	}
	return m
}

// stickerValue is a sticker name, or nil (JSON null) for none.
func stickerValue(name string) any {
	if name == "" {
		return nil
	}
	return name
}

func dueValue(d *task.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// LinkValue is how a link appears in an event's fields.
type LinkValue struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func linkValues(ls []task.Link) []LinkValue {
	out := make([]LinkValue, len(ls))
	for i, l := range ls {
		out[i] = LinkValue{Kind: l.Kind, ID: l.ID}
	}
	return out
}

// ReactionValue is one emoji on one comment in an event's fields.reactions.
// Comment is the comment's 1-based number on the card.
type ReactionValue struct {
	Comment int      `json:"comment"`
	Emoji   string   `json:"emoji"`
	By      []string `json:"by"`
}

// sameText compares two comments without their reactions.
func sameText(a, b task.Comment) bool {
	return a.Author == b.Author && a.Date == b.Date && a.Text == b.Text
}

func commentsEqualPrefix(old, cur []task.Comment) bool {
	if len(old) > len(cur) {
		return false
	}
	return slices.EqualFunc(old, cur[:len(old)], sameText)
}

// reactionChanges lists the reactions, before and after, of each of old's
// comments whose reactions differ in cur. old must be a prefix of cur
// (commentsEqualPrefix); comments cur adds come with their comment_added
// events.
func reactionChanges(old, cur []task.Comment) (before, after []ReactionValue) {
	before, after = []ReactionValue{}, []ReactionValue{}
	for i := range old {
		if task.SameReactions(old[i].Reactions, cur[i].Reactions) {
			continue
		}
		for _, r := range old[i].Reactions {
			before = append(before, ReactionValue{i + 1, r.Emoji, slices.Clone(r.By)})
		}
		for _, r := range cur[i].Reactions {
			after = append(after, ReactionValue{i + 1, r.Emoji, slices.Clone(r.By)})
		}
	}
	return before, after
}

// OnlyReactions reports whether e is a task_updated that changes nothing but
// reactions. People don't count those as unread.
func OnlyReactions(e Event) bool {
	_, ok := e.Fields["reactions"]
	return e.Type == TaskUpdated && ok && len(e.Fields) == 1
}

// Diff returns the semantic events that take old to cur. Reorders within a
// board are deliberately not reported. Events come in current-file order,
// deletions last.
func Diff(old, cur *task.File) []Event {
	om, cm := index(old), index(cur)
	var events []Event
	for _, b := range cur.Boards {
		for _, t := range b.Tasks {
			o, seen := om[t.ID]
			if !seen {
				events = append(events, Event{Type: TaskAdded, Task: t, Board: b.Name})
				continue
			}
			if o.b != b.Name {
				events = append(events, Event{Type: TaskMoved, Task: t, Board: b.Name, From: o.b, To: b.Name})
			}
			fields := map[string]FieldChange{}
			if o.t.Title != t.Title {
				fields["title"] = FieldChange{o.t.Title, t.Title}
			}
			if o.t.Description != t.Description {
				fields["description"] = FieldChange{o.t.Description, t.Description}
			}
			if !slices.Equal(o.t.Tags, t.Tags) {
				fields["tags"] = FieldChange{slices.Clone(o.t.Tags), slices.Clone(t.Tags)}
			}
			if o.t.Priority != t.Priority {
				fields["priority"] = FieldChange{o.t.Priority.String(), t.Priority.String()}
			}
			if dueValue(o.t.Due) != dueValue(t.Due) {
				fields["due"] = FieldChange{dueValue(o.t.Due), dueValue(t.Due)}
			}
			if o.t.Sticker != t.Sticker {
				fields["sticker"] = FieldChange{stickerValue(o.t.Sticker), stickerValue(t.Sticker)}
			}
			if !slices.Equal(o.t.Links, t.Links) {
				fields["links"] = FieldChange{linkValues(o.t.Links), linkValues(t.Links)}
			}
			if commentsEqualPrefix(o.t.Comments, t.Comments) {
				if before, after := reactionChanges(o.t.Comments, t.Comments); len(before)+len(after) > 0 {
					fields["reactions"] = FieldChange{before, after}
				}
				for i := len(o.t.Comments); i < len(t.Comments); i++ {
					c := t.Comments[i]
					events = append(events, Event{Type: CommentAdded, Task: t, Board: b.Name, Comment: &c})
				}
			} else if !task.SameComments(o.t.Comments, t.Comments) {
				// Comments were edited or removed mid-list; report counts and
				// let the consumer `show` the task for detail.
				fields["comments"] = FieldChange{len(o.t.Comments), len(t.Comments)}
			}
			if len(fields) > 0 {
				events = append(events, Event{Type: TaskUpdated, Task: t, Board: b.Name, Fields: fields})
			}
		}
	}
	for _, b := range old.Boards {
		for _, t := range b.Tasks {
			if _, still := cm[t.ID]; !still {
				events = append(events, Event{Type: TaskDeleted, Task: t, Board: b.Name})
			}
		}
	}
	return events
}

var cursorNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// CursorPath returns where the snapshot for (file, cursor name) lives:
// <statedir>/<name>.md (see internal/statedir).
func CursorPath(filePath, name string) (string, error) {
	if !cursorNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid cursor name %q (want [A-Za-z0-9._-]+)", name)
	}
	dir, err := statedir.For(filePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".md"), nil
}

// LoadCursor reads the snapshot; ok is false if none exists yet.
func LoadCursor(path string) (data []byte, ok bool, err error) {
	data, err = os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// SaveCursor stores the snapshot, creating parent directories.
func SaveCursor(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// cloneTask deep-copies a task so a snapshot never aliases the live model.
func cloneTask(t *task.Task) *task.Task {
	c := *t
	c.Tags = slices.Clone(t.Tags)
	c.Links = slices.Clone(t.Links)
	c.Comments = task.CloneComments(t.Comments)
	if t.Due != nil {
		d := *t.Due
		c.Due = &d
	}
	return &c
}

func findTask(f *task.File, id string) (*task.Task, *task.Board) {
	for _, b := range f.Boards {
		for _, t := range b.Tasks {
			if t.ID == id {
				return t, b
			}
		}
	}
	return nil, nil
}

func removeTask(f *task.File, id string) {
	for _, b := range f.Boards {
		for i, t := range b.Tasks {
			if t.ID == id {
				b.Tasks = append(b.Tasks[:i], b.Tasks[i+1:]...)
				return
			}
		}
	}
}

// boardByName finds or appends a board. Board order is irrelevant here:
// snapshots are only ever diffed (by task id and board name), never displayed.
func boardByName(f *task.File, name string) *task.Board {
	for _, b := range f.Boards {
		if b.Name == name {
			return b
		}
	}
	b := &task.Board{Name: name}
	f.Boards = append(f.Boards, b)
	return b
}

// Apply replays events onto f. A writer uses this to fold *its own* delta into
// its own cursor snapshot, so `changes` stops reporting the writer's own
// mutations back to it. Only the fields named by each event are touched, so
// changes made by others — a human's comment on the same task the writer just
// moved, say — stay pending and are still reported.
func Apply(f *task.File, events []Event) {
	for _, e := range events {
		switch e.Type {
		case TaskAdded:
			if t, _ := findTask(f, e.Task.ID); t == nil {
				b := boardByName(f, e.Board)
				b.Tasks = append(b.Tasks, cloneTask(e.Task))
			}
		case TaskDeleted:
			removeTask(f, e.Task.ID)
		case TaskMoved:
			t, _ := findTask(f, e.Task.ID)
			if t == nil {
				continue
			}
			removeTask(f, e.Task.ID)
			to := boardByName(f, e.To)
			to.Tasks = append(to.Tasks, t)
		case TaskUpdated:
			t, _ := findTask(f, e.Task.ID)
			if t == nil {
				continue
			}
			// Copy only the fields this event reports as changed; the event's
			// task carries their post-mutation values.
			for name := range e.Fields {
				switch name {
				case "title":
					t.Title = e.Task.Title
				case "description":
					t.Description = e.Task.Description
				case "tags":
					t.Tags = slices.Clone(e.Task.Tags)
				case "links":
					t.Links = slices.Clone(e.Task.Links)
				case "priority":
					t.Priority = e.Task.Priority
				case "sticker":
					t.Sticker = e.Task.Sticker
				case "due":
					t.Due = nil
					if e.Task.Due != nil {
						d := *e.Task.Due
						t.Due = &d
					}
				case "comments":
					t.Comments = task.CloneComments(e.Task.Comments)
				case "reactions":
					for i := range t.Comments {
						if i < len(e.Task.Comments) && sameText(t.Comments[i], e.Task.Comments[i]) {
							t.Comments[i].Reactions = task.CloneReactions(e.Task.Comments[i].Reactions)
						}
					}
				}
			}
		case CommentAdded:
			t, _ := findTask(f, e.Task.ID)
			if t != nil && e.Comment != nil {
				c := *e.Comment
				c.Reactions = task.CloneReactions(c.Reactions)
				t.Comments = append(t.Comments, c)
			}
		}
	}
}

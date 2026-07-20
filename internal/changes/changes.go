// Package changes computes semantic differences between two states of a
// todo file and manages per-consumer cursor snapshots. Because it diffs
// states rather than recording intents, it sees every kind of change —
// CLI, TUI, hand edits, formatters, git pulls.
package changes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

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

func dueValue(d *task.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func commentsEqualPrefix(old, cur []task.Comment) bool {
	if len(old) > len(cur) {
		return false
	}
	return slices.Equal(old, cur[:len(old)])
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
			if dueValue(o.t.Due) != dueValue(t.Due) {
				fields["due"] = FieldChange{dueValue(o.t.Due), dueValue(t.Due)}
			}
			if commentsEqualPrefix(o.t.Comments, t.Comments) {
				for i := len(o.t.Comments); i < len(t.Comments); i++ {
					c := t.Comments[i]
					events = append(events, Event{Type: CommentAdded, Task: t, Board: b.Name, Comment: &c})
				}
			} else if !slices.Equal(o.t.Comments, t.Comments) {
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
// $XDG_STATE_HOME/todomd/<hash-of-abs-file-path>/<name>.md, defaulting to
// ~/.local/state. The hash keys the *path*, never the content.
func CursorPath(filePath, name string) (string, error) {
	if !cursorNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid cursor name %q (want [A-Za-z0-9._-]+)", name)
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(base, "todomd", hex.EncodeToString(sum[:8]), name+".md"), nil
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

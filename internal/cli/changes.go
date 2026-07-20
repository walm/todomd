package cli

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

type fieldJSON struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type eventJSON struct {
	Type    string               `json:"type"`
	TaskID  string               `json:"task"`
	Title   string               `json:"title"`
	Board   string               `json:"board"`
	From    string               `json:"from,omitempty"`
	To      string               `json:"to,omitempty"`
	Fields  map[string]fieldJSON `json:"fields,omitempty"`
	Comment *commentJSON         `json:"comment,omitempty"`
	Detail  *taskJSON            `json:"detail,omitempty"` // full task, task_added only
}

type changesJSON struct {
	File        string      `json:"file"`
	Cursor      string      `json:"cursor"`
	Initialized bool        `json:"initialized"`
	Events      []eventJSON `json:"events"`
}

var rawIDRe = regexp.MustCompile(`<!--\s*id:\s*([0-9a-z]+)\s*-->`)

// idsPersisted reports whether every parsed task ID literally exists in the
// raw file — false when a hand-added task was assigned a fresh ID at parse
// time, which must be written back before events can carry stable IDs.
func idsPersisted(raw []byte, f *task.File) bool {
	present := map[string]bool{}
	for _, m := range rawIDRe.FindAllSubmatch(raw, -1) {
		present[string(m[1])] = true
	}
	for _, t := range f.AllTasks() {
		if !present[t.ID] {
			return false
		}
	}
	return true
}

func toEventJSON(e changes.Event) eventJSON {
	out := eventJSON{
		Type:   e.Type,
		TaskID: e.Task.ID,
		Title:  e.Task.Title,
		Board:  e.Board,
		From:   e.From,
		To:     e.To,
	}
	if e.Comment != nil {
		out.Comment = &commentJSON{Author: e.Comment.Author, Date: e.Comment.Date, Text: e.Comment.Text}
	}
	if len(e.Fields) > 0 {
		out.Fields = map[string]fieldJSON{}
		for k, v := range e.Fields {
			out.Fields[k] = fieldJSON{Old: v.Old, New: v.New}
		}
	}
	if e.Type == changes.TaskAdded {
		d := toJSON(e.Task, e.Board)
		out.Detail = &d
	}
	return out
}

func newChanges() *cobra.Command {
	var as string
	var peek bool
	var ignoreAuthors []string
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "Show what changed since this cursor last looked",
		Long: `Diffs the file against a per-cursor snapshot (stored under
$XDG_STATE_HOME/todomd, default ~/.local/state/todomd) and reports semantic
events: task_added, task_deleted, task_moved, task_updated, comment_added.
Reading advances the cursor unless --peek is given. The first call
initializes the cursor and reports no events.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(s.Path)
			if err != nil {
				if os.IsNotExist(err) {
					return store.ErrNoFile
				}
				return err
			}
			cur, err := markdown.Parse(raw)
			if err != nil {
				return fmt.Errorf("%s: %w", s.Path, err)
			}
			// Persist parse-assigned IDs (hand-added tasks) so events carry
			// IDs that actually exist in the file.
			if !idsPersisted(raw, cur) {
				if err := s.Mutate(func(*task.File) error { return nil }); err != nil {
					return err
				}
				if raw, err = os.ReadFile(s.Path); err != nil {
					return err
				}
				if cur, err = markdown.Parse(raw); err != nil {
					return err
				}
			}

			cpath, err := changes.CursorPath(s.Path, as)
			if err != nil {
				return err
			}
			prev, ok, err := changes.LoadCursor(cpath)
			if err != nil {
				return err
			}
			if !ok {
				if !peek {
					if err := changes.SaveCursor(cpath, raw); err != nil {
						return err
					}
				}
				if flagJSON {
					return printJSON(changesJSON{File: s.Path, Cursor: as, Initialized: true, Events: []eventJSON{}})
				}
				fmt.Printf("cursor %q initialized; future calls report changes from now\n", as)
				return nil
			}
			old, err := markdown.Parse(prev)
			if err != nil {
				return fmt.Errorf("cursor snapshot %s: %w", cpath, err)
			}

			evs := changes.Diff(old, cur)
			if len(ignoreAuthors) > 0 {
				kept := evs[:0]
				for _, e := range evs {
					if e.Type == changes.CommentAdded && authorIn(e.Comment.Author, ignoreAuthors) {
						continue
					}
					kept = append(kept, e)
				}
				evs = kept
			}
			if !peek {
				if err := changes.SaveCursor(cpath, raw); err != nil {
					return err
				}
			}

			if flagJSON {
				out := changesJSON{File: s.Path, Cursor: as, Events: []eventJSON{}}
				for _, e := range evs {
					out.Events = append(out.Events, toEventJSON(e))
				}
				return printJSON(out)
			}
			if len(evs) == 0 {
				fmt.Println("no changes")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			for _, e := range evs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Type, e.Task.ID, e.Task.Title, eventDetail(e))
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&as, "as", "default", "cursor name (one per consumer, e.g. an agent name)")
	cmd.Flags().BoolVar(&peek, "peek", false, "report without advancing the cursor")
	cmd.Flags().StringArrayVar(&ignoreAuthors, "ignore-author", nil, "drop comment_added events by this author (repeatable)")
	jsonFlag(cmd)
	return cmd
}

func authorIn(author string, list []string) bool {
	for _, a := range list {
		if strings.EqualFold(a, author) {
			return true
		}
	}
	return false
}

func eventDetail(e changes.Event) string {
	switch e.Type {
	case changes.TaskMoved:
		return e.From + " → " + e.To
	case changes.CommentAdded:
		text := e.Comment.Text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[:i] + "…"
		}
		return e.Comment.Author + ": " + text
	case changes.TaskUpdated:
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return "changed: " + strings.Join(keys, ", ")
	case changes.TaskDeleted:
		return "was on " + e.Board
	default:
		return "on " + e.Board
	}
}

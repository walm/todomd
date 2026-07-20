// Package cli wires the todomd command tree. All mutating commands support
// --json (printing the affected task in the pinned schema); errors map to
// exit codes: 1 general, 2 task not found, 3 ambiguous ID prefix.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/tui"
)

type commentJSON struct {
	Author string    `json:"author"`
	Date   task.Date `json:"date"`
	Text   string    `json:"text"`
}

type taskJSON struct {
	ID          string        `json:"id"`
	Board       string        `json:"board"`
	Title       string        `json:"title"`
	Tags        []string      `json:"tags"`
	Due         *task.Date    `json:"due"`
	Description string        `json:"description"`
	Comments    []commentJSON `json:"comments"`
}

func toJSON(t *task.Task, board string) taskJSON {
	out := taskJSON{
		ID:          t.ID,
		Board:       board,
		Title:       t.Title,
		Tags:        append([]string{}, t.Tags...),
		Due:         t.Due,
		Description: t.Description,
		Comments:    []commentJSON{},
	}
	for _, c := range t.Comments {
		out.Comments = append(out.Comments, commentJSON{Author: c.Author, Date: c.Date, Text: c.Text})
	}
	return out
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

var (
	flagFile string
	flagJSON bool
)

func newStore(forInit bool) (*store.Store, error) {
	if forInit && flagFile == "" && os.Getenv("TODOMD_FILE") == "" {
		p, err := filepath.Abs(store.DefaultFileName)
		if err != nil {
			return nil, err
		}
		return &store.Store{Path: p}, nil
	}
	p, err := store.Discover(flagFile)
	if err != nil {
		return nil, err
	}
	return &store.Store{Path: p}, nil
}

// mutate runs fn under the store lock and prints the affected task
// (JSON or a one-line confirmation built by msg).
func mutate(fn func(*task.File) (*task.Task, error), msg func(t *task.Task, board string) string) error {
	s, err := newStore(false)
	if err != nil {
		return err
	}
	var affected *task.Task
	var boardName string
	err = s.Mutate(func(f *task.File) error {
		t, err := fn(f)
		if err != nil {
			return err
		}
		affected = t
		if b := store.BoardOf(f, t); b != nil {
			boardName = b.Name
		}
		return nil
	})
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(toJSON(affected, boardName))
	}
	fmt.Println(msg(affected, boardName))
	return nil
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := newRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "todomd: "+err.Error())
		return classify(err)
	}
	return 0
}

// classify maps an error to the process exit code: 0 none, 2 task not
// found, 3 ambiguous ID prefix, 1 anything else.
func classify(err error) int {
	var nf *store.NotFoundError
	var amb *store.AmbiguousError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &nf):
		return 2
	case errors.As(err, &amb):
		return 3
	default:
		return 1
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "todomd",
		Short:         "Kanban TUI and agent-friendly CLI over a markdown TODO.md",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			return tui.Run(s)
		},
	}
	root.PersistentFlags().StringVarP(&flagFile, "file", "f", "", "path to the todo markdown file (default: TODO.md, searched upward; env TODOMD_FILE)")

	root.AddCommand(newInit(), newList(), newShow(), newAdd(), newUpdate(),
		newMove(), newDone(), newComment(), newDelete(), newBoards(), newChanges())
	return root
}

func jsonFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&flagJSON, "json", false, "print JSON")
}

func newInit() *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new todo file with default boards",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(true)
			if err != nil {
				return err
			}
			if err := store.Init(s.Path, title); err != nil {
				return err
			}
			fmt.Printf("created %s\n", s.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "TODO", "file title")
	return cmd
}

func newList() *cobra.Command {
	var board, tag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			f, err := s.Load()
			if err != nil {
				return err
			}
			match := func(b *task.Board, t *task.Task) bool {
				if board != "" && !strings.EqualFold(b.Name, board) {
					return false
				}
				if tag != "" {
					norm, _ := task.NormalizeTag(tag)
					found := false
					for _, tg := range t.Tags {
						if tg == norm {
							found = true
						}
					}
					if !found {
						return false
					}
				}
				return true
			}
			if flagJSON {
				type boardJSON struct {
					Name  string     `json:"name"`
					Tasks []taskJSON `json:"tasks"`
				}
				out := struct {
					File   string      `json:"file"`
					Boards []boardJSON `json:"boards"`
				}{File: s.Path, Boards: []boardJSON{}}
				for _, b := range f.Boards {
					bj := boardJSON{Name: b.Name, Tasks: []taskJSON{}}
					for _, t := range b.Tasks {
						if match(b, t) {
							bj.Tasks = append(bj.Tasks, toJSON(t, b.Name))
						}
					}
					out.Boards = append(out.Boards, bj)
				}
				return printJSON(out)
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			for _, b := range f.Boards {
				for _, t := range b.Tasks {
					if !match(b, t) {
						continue
					}
					tags := ""
					if len(t.Tags) > 0 {
						tags = "#" + strings.Join(t.Tags, " #")
					}
					due := ""
					if t.Due != nil {
						due = t.Due.String()
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, b.Name, t.Title, tags, due)
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&board, "board", "", "only this board")
	cmd.Flags().StringVar(&tag, "tag", "", "only tasks with this tag")
	jsonFlag(cmd)
	return cmd
}

func newShow() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one task in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			f, err := s.Load()
			if err != nil {
				return err
			}
			b, i, err := store.FindTask(f, args[0])
			if err != nil {
				return err
			}
			t := b.Tasks[i]
			if flagJSON {
				return printJSON(toJSON(t, b.Name))
			}
			fmt.Printf("id:     %s\nboard:  %s\ntitle:  %s\n", t.ID, b.Name, t.Title)
			if len(t.Tags) > 0 {
				fmt.Printf("tags:   #%s\n", strings.Join(t.Tags, " #"))
			}
			if t.Due != nil {
				fmt.Printf("due:    %s\n", t.Due)
			}
			if t.Description != "" {
				fmt.Printf("\n%s\n", t.Description)
			}
			if len(t.Comments) > 0 {
				fmt.Println("\ncomments:")
				for _, c := range t.Comments {
					text := strings.ReplaceAll(c.Text, "\n", "\n    ")
					fmt.Printf("  %s (%s): %s\n", c.Author, c.Date, text)
				}
			}
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}

func newAdd() *cobra.Command {
	var board, desc, due string
	var tags []string
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := &task.Task{Title: args[0], Description: desc, Tags: tags}
			if due != "" {
				d, err := task.ParseDate(due)
				if err != nil {
					return err
				}
				t.Due = &d
			}
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.Add(f, board, t)
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("added %s to %s: %s", t.ID, b, t.Title)
			})
		},
	}
	cmd.Flags().StringVar(&board, "board", "", "target board (default: first board; created if missing)")
	cmd.Flags().StringVar(&desc, "desc", "", "description (markdown)")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "tag (repeatable)")
	cmd.Flags().StringVar(&due, "due", "", "due date YYYY-MM-DD")
	jsonFlag(cmd)
	return cmd
}

func newUpdate() *cobra.Command {
	var opts store.UpdateOpts
	var title, desc, due string
	var tags []string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update task fields (only given flags change)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("title") {
				opts.Title = &title
			}
			if cmd.Flags().Changed("desc") {
				opts.Description = &desc
			}
			if cmd.Flags().Changed("tag") {
				opts.Tags = &tags
			}
			if due != "" {
				d, err := task.ParseDate(due)
				if err != nil {
					return err
				}
				opts.Due = &d
			}
			if opts == (store.UpdateOpts{}) {
				return errors.New("nothing to update: pass at least one flag")
			}
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.Update(f, args[0], opts)
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("updated %s: %s", t.ID, t.Title)
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&desc, "desc", "", "new description")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "replacement tag set (repeatable)")
	cmd.Flags().StringVar(&due, "due", "", "new due date YYYY-MM-DD")
	cmd.Flags().BoolVar(&opts.ClearDue, "clear-due", false, "remove the due date")
	cmd.Flags().BoolVar(&opts.ClearTags, "clear-tags", false, "remove all tags")
	jsonFlag(cmd)
	return cmd
}

func newMove() *cobra.Command {
	var to string
	var pos int
	cmd := &cobra.Command{
		Use:   "move <id>",
		Short: "Move a task to a board and/or position",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" && !cmd.Flags().Changed("pos") {
				return errors.New("pass --to and/or --pos")
			}
			if cmd.Flags().Changed("pos") && pos < 1 {
				return errors.New("--pos must be >= 1")
			}
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.Move(f, args[0], to, pos)
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("moved %s to %s: %s", t.ID, b, t.Title)
			})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "target board (default: current; created if missing)")
	cmd.Flags().IntVar(&pos, "pos", 0, "1-based position in the target (default: append)")
	jsonFlag(cmd)
	return cmd
}

func newDone() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Move a task to the Done board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.Move(f, args[0], "Done", 0)
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("done %s: %s", t.ID, t.Title)
			})
		},
	}
	jsonFlag(cmd)
	return cmd
}

func newComment() *cobra.Command {
	var author string
	cmd := &cobra.Command{
		Use:   "comment <id> <text>",
		Short: "Add a comment to a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.AddComment(f, args[0], author, args[1])
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("commented on %s: %s", t.ID, t.Title)
			})
		},
	}
	cmd.Flags().StringVar(&author, "author", "", "comment author (e.g. ai, or a name)")
	cmd.MarkFlagRequired("author")
	jsonFlag(cmd)
	return cmd
}

func newDelete() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes && term.IsTerminal(int(os.Stdin.Fd())) {
				return errors.New("refusing to delete without --yes on an interactive terminal")
			}
			s, err := newStore(false)
			if err != nil {
				return err
			}
			var deleted *task.Task
			var board string
			err = s.Mutate(func(f *task.File) error {
				deleted, board, err = store.Delete(f, args[0])
				return err
			})
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(toJSON(deleted, board))
			}
			fmt.Printf("deleted %s: %s\n", deleted.ID, deleted.Title)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	jsonFlag(cmd)
	return cmd
}

func newBoards() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boards",
		Short: "List boards with task counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			f, err := s.Load()
			if err != nil {
				return err
			}
			if flagJSON {
				type boardJSON struct {
					Name  string `json:"name"`
					Count int    `json:"count"`
				}
				out := struct {
					Boards []boardJSON `json:"boards"`
				}{Boards: []boardJSON{}}
				for _, b := range f.Boards {
					out.Boards = append(out.Boards, boardJSON{b.Name, len(b.Tasks)})
				}
				return printJSON(out)
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			for _, b := range f.Boards {
				fmt.Fprintf(w, "%s\t%d\n", b.Name, len(b.Tasks))
			}
			return w.Flush()
		},
	}
	jsonFlag(cmd)
	return cmd
}

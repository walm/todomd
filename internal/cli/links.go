package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// cardLinkJSON is one link as a card sees it, in both directions. Title and
// Board are null when the other card isn't on the board (archived or
// deleted); StoredOn is the card whose TODO.md entry holds the line.
type cardLinkJSON struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Title    *string `json:"title"`
	Board    *string `json:"board"`
	StoredOn string  `json:"stored_on"`
}

func linksJSON(f *task.File, t *task.Task) []cardLinkJSON {
	out := []cardLinkJSON{}
	if f == nil {
		for _, l := range t.Links {
			out = append(out, cardLinkJSON{Kind: l.Kind, ID: l.ID, StoredOn: t.ID})
		}
		return out
	}
	for _, v := range store.LinksOf(f, t) {
		j := cardLinkJSON{Kind: v.Kind, ID: v.ID, StoredOn: v.StoredOn}
		if !v.Missing {
			title, board := v.Title, v.Board
			j.Title, j.Board = &title, &board
		}
		out = append(out, j)
	}
	return out
}

// writeLinkRows prints links as aligned rows: kind, id, title, board, each
// row after prefix (the first row's prefix is first).
func writeLinkRows(out io.Writer, views []store.LinkView, first, prefix string) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for i, v := range views {
		p := prefix
		if i == 0 {
			p = first
		}
		if v.Missing {
			fmt.Fprintf(w, "%s%s\t%s\t(not on the board)\t\n", p, v.Kind, v.ID)
		} else {
			fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\n", p, v.Kind, v.ID, v.Title, v.Board)
		}
	}
	w.Flush()
}

func newLinks() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "links <id>",
		Short: "List a card's links, or add and remove them",
		Long: `Lists a card's links in both directions: the ones stored on the card
and the ones other cards store about it, read from this card's side.

Kinds: split from, split into, blocks, blocked by, related, duplicate of,
duplicated by (any case; '-' may stand for the space). A link is stored on
one card only, the one it was added on; the other card shows it inverted.

  todomd links 3f2a
  todomd links add 3f2a blocks 9e41 --as agent
  todomd links rm 3f2a 9e41 [--kind blocks]`,
		Args: cobra.ExactArgs(1),
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
				return printJSON(struct {
					ID    string         `json:"id"`
					Title string         `json:"title"`
					Board string         `json:"board"`
					Links []cardLinkJSON `json:"links"`
				}{t.ID, t.Title, b.Name, linksJSON(f, t)})
			}
			views := store.LinksOf(f, t)
			if len(views) == 0 {
				fmt.Printf("%s has no links\n", t.ID)
				return nil
			}
			writeLinkRows(os.Stdout, views, "", "")
			return nil
		},
	}
	jsonFlag(cmd)
	cmd.AddCommand(newLinksAdd(), newLinksRm())
	return cmd
}

func newLinksAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <id> <kind> <id>",
		Short: "Link two cards (stored on the first)",
		Long: `Adds "<id> <kind> <other id>" to the first card, e.g.
'todomd links add 3f2a blocks 9e41' or 'todomd links add 3f2a blocked-by 9e41'.
The other card must be on the board. A link the board already has, on
either card, is left as it is.`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.Join(args[1:len(args)-1], " ")
			target := args[len(args)-1]
			s, err := newStore(false)
			if err != nil {
				return err
			}
			var t *task.Task
			var l task.Link
			var added bool
			var final *task.File
			before, after, err := s.MutateTracked(func(f *task.File) error {
				t, l, added, err = store.AddLink(f, args[0], kind, target)
				final = f
				return err
			})
			if err != nil {
				return err
			}
			skipOwnChange(s.Path, before, after)
			if flagJSON {
				board := ""
				if b := store.BoardOf(final, t); b != nil {
					board = b.Name
				}
				return printJSON(toJSON(final, t, board))
			}
			title := ""
			if b, i, err := store.FindTask(final, l.ID); err == nil {
				title = fmt.Sprintf(" %q", b.Tasks[i].Title)
			}
			verb := "linked"
			if !added {
				verb = "already linked:"
			}
			fmt.Printf("%s %s %s %s%s\n", verb, t.ID, l.Kind, l.ID, title)
			return nil
		},
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func newLinksRm() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "rm <id> <id>",
		Short: "Remove the links between two cards, wherever they are stored",
		Long: `Removes the links between two cards, from whichever card stores them.
--kind removes only that relation, read from the first card's side
('rm 3f2a 9e41 --kind blocks' removes "3f2a blocks 9e41" and "9e41 blocked by
3f2a"). One of the two may be a card that is no longer on the board.
Exits 2 when there is no such link.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			var removed []store.RemovedLink
			before, after, err := s.MutateTracked(func(f *task.File) error {
				removed, err = store.RemoveLinks(f, args[0], args[1], kind)
				return err
			})
			if err != nil {
				return err
			}
			skipOwnChange(s.Path, before, after)
			if flagJSON {
				type removedJSON struct {
					StoredOn string `json:"stored_on"`
					Kind     string `json:"kind"`
					ID       string `json:"id"`
				}
				out := struct {
					Removed []removedJSON `json:"removed"`
				}{}
				for _, r := range removed {
					out.Removed = append(out.Removed, removedJSON{r.StoredOn, r.Kind, r.ID})
				}
				return printJSON(out)
			}
			for _, r := range removed {
				fmt.Printf("unlinked %s %s %s\n", r.StoredOn, r.Kind, r.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "only this relation, from the first card's side (e.g. blocks)")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func newSplit() *cobra.Command {
	var board string
	cmd := &cobra.Command{
		Use:   "split <id> <title>...",
		Short: "Split a card into new cards linked 'split from' it",
		Long: `Creates one card per title, each linked "split from <id>". They go on the
original card's board (or --board) with its tags. The original card isn't
changed: add a comment there saying why it was split.

  todomd split 3f2a "Parse the header" "Parse the body" --as agent`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			var created []*task.Task
			var parent *task.Task
			var final *task.File
			before, after, err := s.MutateTracked(func(f *task.File) error {
				final = f
				pb, i, err := store.FindTask(f, args[0])
				if err != nil {
					return err
				}
				parent = pb.Tasks[i]
				to := board
				if to == "" {
					to = pb.Name
				}
				for _, title := range args[1:] {
					t := &task.Task{
						Title: title,
						Tags:  append([]string(nil), parent.Tags...),
						Links: []task.Link{{Kind: task.LinkSplitFrom, ID: parent.ID}},
					}
					if _, err := store.Add(f, to, t); err != nil {
						return err
					}
					created = append(created, t)
				}
				return nil
			})
			if err != nil {
				return err
			}
			skipOwnChange(s.Path, before, after)
			boardOf := func(t *task.Task) string {
				if b := store.BoardOf(final, t); b != nil {
					return b.Name
				}
				return ""
			}
			if flagJSON {
				out := []taskJSON{}
				for _, t := range created {
					out = append(out, toJSON(final, t, boardOf(t)))
				}
				return printJSON(out)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, t := range created {
				fmt.Fprintf(w, "added %s %s\t%s · split from %s\n", t.ID, t.Title, boardOf(t), parent.ID)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&board, "board", "", "board for the new cards (default: the original's; created if missing)")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

// addLinks resolves `add --link kind:id` values against f for the new card t.
func addLinks(f *task.File, t *task.Task, specs []string) error {
	for _, spec := range specs {
		kind, ref, err := store.ParseLinkSpec(spec)
		if err != nil {
			return err
		}
		id, err := store.LinkTarget(f, nil, ref)
		if err != nil {
			return err
		}
		l := task.Link{Kind: kind, ID: id}
		if !slices.Contains(t.Links, l) {
			t.Links = append(t.Links, l)
		}
	}
	if len(t.Links) > task.MaxLinks {
		return fmt.Errorf("a card can have at most %d links", task.MaxLinks)
	}
	return nil
}

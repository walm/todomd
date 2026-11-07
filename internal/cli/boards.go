package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// boardResultJSON is what `boards add|rename|move --json` print: the board
// as it is now, its 1-based position, and the full board order after the
// change. From is the old name, for rename only.
type boardResultJSON struct {
	Board    string   `json:"board"`
	From     string   `json:"from,omitempty"`
	Position int      `json:"position"`
	Count    int      `json:"count"`
	Boards   []string `json:"boards"`
}

// mutateBoard runs fn under the store lock, folds the write into the --as
// cursor, and prints the result (JSON, or the line msg builds).
func mutateBoard(fn func(*task.File) (b *task.Board, from string, err error), msg func(r boardResultJSON) string) error {
	s, err := newStore(false)
	if err != nil {
		return err
	}
	var res boardResultJSON
	before, after, err := s.MutateTracked(func(f *task.File) error {
		b, from, err := fn(f)
		if err != nil {
			return err
		}
		res = boardResultJSON{Board: b.Name, From: from, Position: store.BoardPosition(f, b), Count: len(b.Tasks), Boards: []string{}}
		for _, x := range f.Boards {
			res.Boards = append(res.Boards, x.Name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	skipOwnChange(s.Path, before, after)
	if flagJSON {
		return printJSON(res)
	}
	fmt.Println(msg(res))
	return nil
}

// placeFlags adds --after/--before (and --to when withPos, which then makes
// one of them required) and reads them back as a store.BoardPlace.
func placeFlags(cmd *cobra.Command, withPos bool) func() (store.BoardPlace, error) {
	var p store.BoardPlace
	cmd.Flags().StringVar(&p.After, "after", "", "put it directly after this board")
	cmd.Flags().StringVar(&p.Before, "before", "", "put it directly before this board")
	names := []string{"after", "before"}
	if withPos {
		cmd.Flags().IntVar(&p.Pos, "to", 0, "1-based position among the boards (past the end means last)")
		names = []string{"to", "after", "before"}
	}
	list := "--" + strings.Join(names, ", --")
	return func() (store.BoardPlace, error) {
		set := 0
		for _, n := range names {
			if cmd.Flags().Changed(n) {
				set++
			}
		}
		switch {
		case set > 1:
			return p, fmt.Errorf("pass only one of %s", list)
		case set == 0 && withPos:
			return p, fmt.Errorf("pass one of %s", list)
		case withPos && cmd.Flags().Changed("to") && p.Pos < 1:
			return p, errors.New("--to must be >= 1")
		case cmd.Flags().Changed("after") && strings.TrimSpace(p.After) == "",
			cmd.Flags().Changed("before") && strings.TrimSpace(p.Before) == "":
			return p, errors.New("--after and --before need a board name")
		}
		return p, nil
	}
}

func newBoardsAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an empty board (before Done by default)",
		Long: `Add an empty board. By default it goes before "Done" when there is one,
otherwise last; --after or --before put it next to another board.

Board names ignore case, so a name another board already has (in any case)
is refused with exit code 8.`,
		Args: cobra.ExactArgs(1),
	}
	place := placeFlags(cmd, false)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		at, err := place()
		if err != nil {
			return err
		}
		return mutateBoard(func(f *task.File) (*task.Board, string, error) {
			b, err := store.AddBoard(f, args[0], at)
			return b, "", err
		}, func(r boardResultJSON) string {
			return fmt.Sprintf("added board %s at position %d of %d", r.Board, r.Position, len(r.Boards))
		})
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func newBoardsRename() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <board> <new name>",
		Short: "Rename a board, keeping its tasks and position",
		Long: `Rename a board. Its tasks and position stay; a change of case only is fine.

Board names ignore case, so a name another board already has (in any case)
is refused with exit code 8. In 'todomd changes', each task on a renamed
board shows up as task_moved from the old name to the new one.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateBoard(func(f *task.File) (*task.Board, string, error) {
				return store.RenameBoard(f, args[0], args[1])
			}, func(r boardResultJSON) string {
				if r.From == r.Board {
					return fmt.Sprintf("board %s already has that name", r.Board)
				}
				return fmt.Sprintf("renamed board %s to %s", r.From, r.Board)
			})
		},
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func newBoardsMove() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <board> (--to <pos> | --after <board> | --before <board>)",
		Short: "Move a board to another position",
		Long: `Move a board, with its tasks, to another position among the boards:
--to is 1-based (past the end means last), --after and --before name
another board.`,
		Args: cobra.ExactArgs(1),
	}
	place := placeFlags(cmd, true)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		at, err := place()
		if err != nil {
			return err
		}
		return mutateBoard(func(f *task.File) (*task.Board, string, error) {
			b, err := store.MoveBoard(f, args[0], at)
			return b, "", err
		}, func(r boardResultJSON) string {
			return fmt.Sprintf("moved board %s to position %d of %d", r.Board, r.Position, len(r.Boards))
		})
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

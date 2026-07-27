package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/vcs"
)

func newArchive() *cobra.Command {
	var board, to string
	var yes, force, dryRun bool
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Clear finished tasks off the board",
		Long: `Remove every task from a board (Done by default), keeping the board itself.

This is bulk and destructive, so it asks before doing anything, and it checks
whether the file is committed to git first — a committed file means everything
it removes stays recoverable from history, which is how todomd expects you to
keep an archive. Pass --to to move the tasks into another markdown file
instead of dropping them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			f, err := s.Load()
			if err != nil {
				return err
			}
			b := store.FindBoard(f, board)
			if b == nil {
				return fmt.Errorf("no board named %q", board)
			}
			doomed := append([]*task.Task{}, b.Tasks...)
			if len(doomed) == 0 {
				if flagJSON {
					return printJSON(archiveResult{Board: b.Name, Count: 0, Tasks: []taskJSON{}})
				}
				fmt.Printf("nothing to archive: %s is empty\n", b.Name)
				return nil
			}

			state := vcs.FileState(s.Path)
			if dryRun {
				return reportArchive(b.Name, doomed, state, to, true)
			}

			// Refuse by default when history wouldn't hold what we remove.
			if !state.Recoverable() && to == "" && !force {
				return fmt.Errorf("%s is %s, so the %s would not be recoverable from git history: "+
					"commit it first, use --to FILE to keep the tasks, or pass --force",
					s.Path, state, plural(len(doomed), "task"))
			}
			if !yes {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("refusing to archive %s from %s without --yes",
						plural(len(doomed), "task"), b.Name)
				}
				if err := reportArchive(b.Name, doomed, state, to, false); err != nil {
					return err
				}
				if !confirm(fmt.Sprintf("archive %s?", plural(len(doomed), "task"))) {
					fmt.Println("cancelled")
					return nil
				}
			}

			// Write the destination first: if the second step fails the tasks
			// exist twice, which beats them existing nowhere.
			if to != "" {
				if err := appendTasks(to, b.Name, doomed); err != nil {
					return fmt.Errorf("writing %s: %w", to, err)
				}
			}
			ids := make([]string, len(doomed))
			for i, t := range doomed {
				ids[i] = t.ID
			}
			before, after, err := s.MutateTracked(func(f *task.File) error {
				for _, id := range ids {
					if _, _, err := store.Delete(f, id); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			skipOwnChange(s.Path, before, after)

			if flagJSON {
				res := archiveResult{Board: b.Name, Count: len(doomed), To: to, Tasks: []taskJSON{}}
				for _, t := range doomed {
					res.Tasks = append(res.Tasks, toJSON(t, b.Name))
				}
				return printJSON(res)
			}
			where := "removed"
			if to != "" {
				where = "moved to " + to
			}
			fmt.Printf("archived %s from %s (%s)\n", plural(len(doomed), "task"), b.Name, where)
			return nil
		},
	}
	cmd.Flags().StringVar(&board, "board", "Done", "board to clear")
	cmd.Flags().StringVar(&to, "to", "", "append the tasks to this markdown file instead of dropping them")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "archive even when the file isn't committed to git")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be archived and stop")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

type archiveResult struct {
	Board    string     `json:"board"`
	Count    int        `json:"count"`
	To       string     `json:"to,omitempty"`
	DryRun   bool       `json:"dry_run,omitempty"`
	GitState string     `json:"git_state,omitempty"`
	Recovers bool       `json:"recoverable_from_git,omitempty"`
	Tasks    []taskJSON `json:"tasks"`
}

// reportArchive lists what is about to go, and how recoverable it would be.
func reportArchive(board string, doomed []*task.Task, state vcs.State, to string, dry bool) error {
	if flagJSON {
		res := archiveResult{
			Board: board, Count: len(doomed), To: to, DryRun: dry,
			GitState: state.String(), Recovers: state.Recoverable(), Tasks: []taskJSON{},
		}
		for _, t := range doomed {
			res.Tasks = append(res.Tasks, toJSON(t, board))
		}
		return printJSON(res)
	}
	verb := "would archive"
	if !dry {
		verb = "about to archive"
	}
	fmt.Printf("%s %s from %s:\n", verb, plural(len(doomed), "task"), board)
	const show = 10
	for i, t := range doomed {
		if i == show {
			fmt.Printf("  … and %d more\n", len(doomed)-show)
			break
		}
		fmt.Printf("  %s  %s\n", t.ID, t.Title)
	}
	if to != "" {
		fmt.Printf("they will be appended to %s\n", to)
	} else if state.Recoverable() {
		fmt.Printf("%s is committed, so these stay in git history (git log -p)\n", "the file")
	} else {
		fmt.Printf("warning: the file is %s — removing these would be permanent\n", state)
	}
	return nil
}

// plural renders "1 task" / "3 tasks".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(sc.Text()))
	return answer == "y" || answer == "yes"
}

// appendTasks adds tasks to a board of the same name in another todo file,
// creating the file if it doesn't exist yet. The destination stays a valid
// TODO.md, so it can be opened with todomd like any other board.
func appendTasks(path, board string, tasks []*task.Task) error {
	dest := &store.Store{Path: path}
	f, err := dest.Load()
	if err != nil {
		if !os.IsNotExist(err) && err != store.ErrNoFile {
			return err
		}
		f = &task.File{Title: "Archive"}
	}
	b, err := store.EnsureBoard(f, board)
	if err != nil {
		return err
	}
	taken := map[string]bool{}
	for _, t := range f.AllTasks() {
		taken[t.ID] = true
	}
	for _, t := range tasks {
		cp, err := markdown.ParseTask(markdown.WriteTask(t))
		if err != nil {
			return err
		}
		if taken[cp.ID] { // keep ids unique in the destination
			cp.ID = task.NewID(taken)
		}
		taken[cp.ID] = true
		b.Tasks = append(b.Tasks, cp)
	}
	return dest.Save(f)
}

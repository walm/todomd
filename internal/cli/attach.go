package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newAttach() *cobra.Command {
	var alt, comment, author string
	cmd := &cobra.Command{
		Use:   "attach <id> <file>",
		Short: "Attach a file or image to a task or comment (synced with todomd.online)",
		Long: `Copy a file into todomd's attachment folder for the linked project
($XDG_STATE_HOME/todomd/attachments, default ~/.local/state) and link it from
the task. The next sync uploads it, and the board online shows it. The link in
TODO.md stays a local path, so agents and the TUI can open the file like any
other.

An image (PNG, JPEG, GIF or WebP, up to 5 MB) is shown inline: ![name](path).
Any other file the board takes (a Markdown plan, a PDF, a CSV, a zip, … up to
25 MB) becomes a plain link, [name](path), which the board shows as a download.

By default the link goes at the end of the description. With --comment "<text>"
it goes into a new comment instead (the text first, the link after it), and
--author says who wrote it.

Needs a linked file ('todomd link'), because attachments are stored per
project. The size limits are checked before anything is copied.`,
		Example: `  todomd attach 3f2a ~/Desktop/screenshot.png
  todomd attach 3f2a plan.md
  todomd attach 3f2a plan.md --comment "Plan for review" --author ai`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			commenting := cmd.Flags().Changed("comment")
			if commenting && author == "" {
				return errors.New("--comment needs --author (e.g. --author ai)")
			}
			if !commenting && author != "" {
				return errors.New("--author goes with --comment")
			}
			st, err := syncStore()
			if err != nil {
				return err
			}
			l, err := cloudsync.LoadLink(st.Path)
			if errors.Is(err, cloudsync.ErrNotLinked) {
				return fmt.Errorf("attaching a file needs a linked board: %w", err)
			}
			if err != nil {
				return err
			}
			fi, err := os.Stat(args[1])
			if err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("%s: not a regular file", args[1])
			}
			if fi.Size() > cloud.MaxFileBytes {
				return fmt.Errorf("%s: files can be at most %d MB (it is %.1f MB)", args[1], cloud.MaxFileBytes>>20, float64(fi.Size())/(1<<20))
			}
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			att, name, link, image, err := cloudsync.StoreFile(l, data, filepath.Base(args[1]))
			if err != nil {
				return fmt.Errorf("%s: %w", args[1], err)
			}
			text := alt
			if text == "" {
				text = name
				if image {
					text = strings.TrimSuffix(name, filepath.Ext(name))
				}
			}
			snippet := "[" + strings.NewReplacer("[", "", "]", "", "\n", " ").Replace(text) + "](" + link + ")"
			if image {
				snippet = "!" + snippet
			}
			where := "to"
			return mutate(func(f *task.File) (*task.Task, error) {
				if commenting {
					where = "to a comment on"
					body := strings.TrimRight(comment, "\n")
					if body != "" {
						body += "\n\n"
					}
					return store.AddComment(f, args[0], author, body+snippet)
				}
				b, i, err := store.FindTask(f, args[0])
				if err != nil {
					return nil, err
				}
				t := b.Tasks[i]
				desc := strings.TrimRight(t.Description, "\n")
				if desc != "" {
					desc += "\n\n"
				}
				desc += snippet
				return store.Update(f, t.ID, store.UpdateOpts{Description: &desc})
			}, func(t *task.Task, b string) string {
				return fmt.Sprintf("attached %s %s %s: %s (%s)", name, where, t.ID, t.Title, att)
			})
		},
	}
	cmd.Flags().StringVar(&alt, "alt", "", "link text (default: the file name)")
	cmd.Flags().StringVar(&comment, "comment", "", "add a new comment with this text and the file, instead of changing the description")
	cmd.Flags().StringVar(&author, "author", "", "comment author with --comment (e.g. ai, or a name)")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

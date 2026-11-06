package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newAttach() *cobra.Command {
	var alt string
	cmd := &cobra.Command{
		Use:   "attach <id> <image>",
		Short: "Attach an image to a task (synced with todomd.online)",
		Long: `Copy an image into todomd's attachment folder for the linked project
($XDG_STATE_HOME/todomd/attachments, default ~/.local/state) and add a link to
it at the end of the task's description. The next sync uploads it, and the
board online shows it. The link in TODO.md stays a local path, so agents and
the TUI can open the image like any file.

PNG, JPEG, GIF and WebP images up to 5 MB. Needs a linked file
('todomd link'), because images are stored per project.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := syncStore()
			if err != nil {
				return err
			}
			l, err := cloudsync.LoadLink(st.Path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			att, name, link, err := cloudsync.StoreImage(l, data, filepath.Base(args[1]))
			if err != nil {
				return fmt.Errorf("%s: %w", args[1], err)
			}
			text := alt
			if text == "" {
				text = strings.TrimSuffix(name, filepath.Ext(name))
			}
			snippet := "![" + strings.NewReplacer("[", "", "]", "", "\n", " ").Replace(text) + "](" + link + ")"
			return mutate(func(f *task.File) (*task.Task, error) {
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
				return fmt.Sprintf("attached %s to %s: %s (%s)", name, t.ID, t.Title, att)
			})
		},
	}
	cmd.Flags().StringVar(&alt, "alt", "", "alt text for the image (default: its file name)")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

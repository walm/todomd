package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newSticker() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sticker [<id> [<sticker>|none]]",
		Short: "Set or remove a card's sticker",
		Long: `A card can carry one sticker so it stands out on the board: an emoji from a
fixed set, or a coloured ribbon. Give its name or the emoji itself; "none"
removes it. With only an id, prints the card's sticker; with no arguments,
lists the stickers.

Emoji:   fire 🔥  star ⭐  eyes 👀  wip 🚧  check ✅  target 🎯
         zap ⚡  idea 💡  bug 🐛  ship 🚀  question ❓  heart ❤️
Ribbons: red  amber  green  blue  violet`,
		Example: `  todomd sticker 3f2a fire
  todomd sticker 3f2a 🚀
  todomd sticker 3f2a red
  todomd sticker 3f2a none`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch len(args) {
			case 0:
				return listStickers()
			case 1:
				return showSticker(args[0])
			}
			name, err := task.ParseSticker(args[1])
			if err != nil {
				return err
			}
			return mutate(func(f *task.File) (*task.Task, error) {
				return store.Update(f, args[0], store.UpdateOpts{Sticker: &name})
			}, func(t *task.Task, b string) string {
				if name == "" {
					return fmt.Sprintf("removed the sticker from %s: %s", t.ID, t.Title)
				}
				return fmt.Sprintf("sticker %s on %s: %s", task.StickerLabel(name), t.ID, t.Title)
			})
		},
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func listStickers() error {
	if flagJSON {
		type stickerJSON struct {
			Name  string `json:"name"`
			Emoji string `json:"emoji,omitempty"`
			Kind  string `json:"kind"`
		}
		out := []stickerJSON{}
		for _, s := range task.Stickers {
			kind := "emoji"
			if s.Emoji == "" {
				kind = "ribbon"
			}
			out = append(out, stickerJSON{s.Name, s.Emoji, kind})
		}
		return printJSON(out)
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	for _, s := range task.Stickers {
		fmt.Fprintf(w, "%s\t%s\n", s.Name, stickerMark(s.Name))
	}
	return w.Flush()
}

func showSticker(ref string) error {
	s, err := newStore(false)
	if err != nil {
		return err
	}
	f, err := s.Load()
	if err != nil {
		return err
	}
	b, i, err := store.FindTask(f, ref)
	if err != nil {
		return err
	}
	t := b.Tasks[i]
	if flagJSON {
		return printJSON(toJSON(f, t, b.Name))
	}
	if t.Sticker == "" {
		fmt.Println("none")
	} else {
		fmt.Println(task.StickerLabel(t.Sticker))
	}
	return nil
}

// stickerJSON is a sticker in task JSON: its name, or null.
func stickerJSON(name string) *string {
	if name == "" {
		return nil
	}
	return &name
}

// stickerMark is a sticker in a list row: the emoji, or the ribbon colour
// after a corner mark.
func stickerMark(name string) string {
	switch {
	case name == "":
		return ""
	case task.IsRibbon(name):
		return "◥ " + name
	default:
		return task.StickerEmoji(name)
	}
}

// stickerFilter reads list --sticker: a sticker, "any" or "none"; "" keeps
// every card.
func stickerFilter(s string) (func(*task.Task) bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return func(*task.Task) bool { return true }, nil
	case "any":
		return func(t *task.Task) bool { return t.Sticker != "" }, nil
	case "none":
		return func(t *task.Task) bool { return t.Sticker == "" }, nil
	}
	name, err := task.ParseSticker(s)
	if err != nil {
		return nil, err
	}
	return func(t *task.Task) bool { return t.Sticker == name }, nil
}

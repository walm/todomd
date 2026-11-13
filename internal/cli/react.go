package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newReact() *cobra.Command {
	var author, match string
	var remove bool
	cmd := &cobra.Command{
		Use:   "react <id> <comment> [<emoji>]",
		Short: "React to a comment with an emoji",
		Long: `Adds your emoji reaction to one of a card's comments, or with --remove takes
it away. Name the comment by its number as 'todomd show' prints it, or
"last". --match makes sure it is the comment you mean: its text must
contain that text (case and spacing aside), or nothing changes.

Any emoji works; the quick set is 👍 ❤️ 🎉 👀 ✅ 🙏. Without an emoji, prints
the comment's reactions.`,
		Example: `  todomd react 3f2a 2 👍 --author walm
  todomd react 3f2a last 👀 --author agent --match "can you check"
  todomd react 3f2a 2 👍 --author walm --remove
  todomd react 3f2a 2`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 {
				return showReactions(args[0], args[1], match)
			}
			emoji, err := task.ParseEmoji(args[2])
			if err != nil {
				return err
			}
			if author == "" {
				return errors.New(`required flag "author" not set (who reacts, e.g. walm or agent)`)
			}
			name, err := task.ParseReactor(author)
			if err != nil {
				return err
			}
			var k int
			var changed bool
			return mutate(func(f *task.File) (*task.Task, error) {
				t, i, ch, err := store.React(f, args[0], args[1], match, emoji, name, remove)
				k, changed = i, ch
				return t, err
			}, func(t *task.Task, b string) string {
				where := fmt.Sprintf("comment %d on %s: %s", k+1, t.ID, t.Title)
				switch {
				case remove && changed:
					return fmt.Sprintf("removed %s's %s from %s", name, emoji, where)
				case remove:
					return fmt.Sprintf("%s hadn't reacted %s to %s", name, emoji, where)
				case changed:
					return fmt.Sprintf("%s reacted %s to %s", name, emoji, where)
				default:
					return fmt.Sprintf("%s already reacted %s to %s", name, emoji, where)
				}
			})
		},
	}
	cmd.Flags().StringVar(&author, "author", "", "who reacts (e.g. walm, or agent); lowercased")
	cmd.Flags().StringVar(&match, "match", "", "only if the comment's text contains this")
	cmd.Flags().BoolVar(&remove, "remove", false, "take the reaction away")
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func showReactions(ref, which, match string) error {
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
	k, err := store.FindComment(t, which, match)
	if err != nil {
		return err
	}
	rs := t.Comments[k].Reactions
	if flagJSON {
		return printJSON(reactionsJSON(rs))
	}
	if len(rs) == 0 {
		fmt.Println("none")
	}
	for _, r := range rs {
		fmt.Printf("%s %s\n", r.Emoji, strings.Join(r.By, ", "))
	}
	return nil
}

type reactionJSON struct {
	Emoji string   `json:"emoji"`
	By    []string `json:"by"`
}

// reactionsJSON is a comment's reactions in JSON: always an array.
func reactionsJSON(rs []task.Reaction) []reactionJSON {
	out := []reactionJSON{}
	for _, r := range rs {
		out = append(out, reactionJSON{r.Emoji, append([]string{}, r.By...)})
	}
	return out
}

// reactionsLine is a comment's reactions on one line for people:
// "👍 walm, anna · 👀 agent"; "" for none.
func reactionsLine(rs []task.Reaction) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.Emoji + " " + strings.Join(r.By, ", ")
	}
	return strings.Join(parts, " · ")
}

func toCommentJSON(c task.Comment) commentJSON {
	return commentJSON{Author: c.Author, Date: c.Date, Text: c.Text, Reactions: reactionsJSON(c.Reactions)}
}

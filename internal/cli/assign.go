package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newAssign() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <id> <@handle|me>",
		Short: "Assign a card to someone (one assignee per card)",
		Long: `A card has at most one assignee: a handle (lowercase letters, digits and
inner hyphens, up to 39 characters; the @ is optional). It doesn't have to be
a todomd.online project member: an agent's --as name works too. Assigning
replaces any assignee the card had.

"me" is you: the --as name (or $TODOMD_CURSOR), else your todomd.online
handle. Write @me for someone whose handle is literally "me".

todomd doesn't act on assignees by itself. An agent uses them only if its
rules say so; see 'todomd list --assignee' and 'watch --ignore-assigned-to-others'.`,
		Example: `  todomd assign 3f2a @anna
  todomd assign 3f2a me --as agent
  todomd unassign 3f2a`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := assigneeArg(args[1])
			if err != nil {
				return err
			}
			was := ""
			return mutate(func(f *task.File) (*task.Task, error) {
				if b, i, err := store.FindTask(f, args[0]); err == nil {
					was = b.Tasks[i].Assignee
				}
				return store.Update(f, args[0], store.UpdateOpts{Assignee: &h})
			}, func(t *task.Task, b string) string {
				msg := fmt.Sprintf("assigned %s to @%s: %s", t.ID, h, t.Title)
				switch was {
				case "":
				case h:
					msg = fmt.Sprintf("%s is already assigned to @%s: %s", t.ID, h, t.Title)
				default:
					msg += " (was @" + was + ")"
				}
				return msg
			})
		},
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

func newUnassign() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unassign <id> [@handle|me]",
		Short: "Remove a card's assignee",
		Long: `Removes the card's assignee. With a handle (or "me"), only when the card is
assigned to that handle; otherwise the card is left as it is, so an agent
letting go of its card can't clear someone else's.`,
		Example: `  todomd unassign 3f2a
  todomd unassign 3f2a me --as agent`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			only := ""
			if len(args) == 2 {
				h, err := assigneeArg(args[1])
				if err != nil {
					return err
				}
				only = h
			}
			was, kept := "", false
			return mutate(func(f *task.File) (*task.Task, error) {
				b, i, err := store.FindTask(f, args[0])
				if err != nil {
					return nil, err
				}
				was = b.Tasks[i].Assignee
				if only != "" && was != only {
					kept = true
					return b.Tasks[i], nil
				}
				none := ""
				return store.Update(f, args[0], store.UpdateOpts{Assignee: &none})
			}, func(t *task.Task, b string) string {
				switch {
				case kept && was == "":
					return fmt.Sprintf("%s isn't assigned to anyone, so not to @%s; left as is: %s", t.ID, only, t.Title)
				case kept:
					return fmt.Sprintf("%s is assigned to @%s, not @%s; left as is: %s", t.ID, was, only, t.Title)
				case was == "":
					return fmt.Sprintf("%s wasn't assigned: %s", t.ID, t.Title)
				}
				return fmt.Sprintf("unassigned %s (was @%s): %s", t.ID, was, t.Title)
			})
		},
	}
	asFlag(cmd)
	jsonFlag(cmd)
	return cmd
}

// assigneeArg reads a handle argument: "me" is you (see resolveMe), any
// other spelling a handle, the @ optional.
func assigneeArg(s string) (string, error) {
	if strings.EqualFold(strings.TrimSpace(s), "me") {
		return resolveMe()
	}
	return task.ParseHandle(s)
}

// resolveMe is who "me" is: the --as name (or $TODOMD_CURSOR), else the
// handle this machine is logged in to todomd.online as (the linked board's
// host, else the default one).
func resolveMe() (string, error) {
	if as := selfCursor(); as != "" {
		h, err := task.ParseHandle(as)
		if err != nil {
			return "", fmt.Errorf("'me' is your --as name %q, which isn't a valid handle (lowercase letters, digits and inner hyphens, up to %d characters)", as, task.MaxHandle)
		}
		return h, nil
	}
	host := ""
	if s, err := newStore(false); err == nil {
		if l, err := cloudsync.LoadLink(s.Path); err == nil {
			host = l.Host
		}
	}
	if host == "" {
		h, err := cloud.ResolveHost("")
		if err != nil {
			return "", err
		}
		host = h
	}
	if hosts, err := cloud.LoadHosts(); err == nil {
		if c := hosts.Hosts[host]; c != nil && c.User.Handle != "" {
			return c.User.Handle, nil
		}
	}
	return "", errors.New("who is 'me'? pass --as <name> (an agent's name), or log in with 'todomd login' and pick a handle on todomd.online")
}

// assigneeFilter reads list --assignee: a handle, "me", "any" or "none"; ""
// keeps every card.
func assigneeFilter(s string) (func(*task.Task) bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return func(*task.Task) bool { return true }, nil
	case "any":
		return func(t *task.Task) bool { return t.Assignee != "" }, nil
	case "none":
		return func(t *task.Task) bool { return t.Assignee == "" }, nil
	}
	h, err := assigneeArg(s)
	if err != nil {
		return nil, err
	}
	return func(t *task.Task) bool { return t.Assignee == h }, nil
}

// updateAssigneeArg reads add/update --assignee: a handle, "me", or "none"
// (update only) to unassign.
func updateAssigneeArg(s string, allowNone bool) (string, error) {
	if allowNone && strings.EqualFold(strings.TrimSpace(s), "none") {
		return "", nil
	}
	return assigneeArg(s)
}

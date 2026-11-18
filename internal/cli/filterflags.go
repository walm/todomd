package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/filter"
	"github.com/walm/todomd/internal/task"
)

// filterFlags are --filter and its short forms on watch and changes.
type filterFlags struct {
	query     string
	tags      []string
	assignees []string
	save      bool
	show      bool
	clear     bool
}

func (f *filterFlags) add(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.query, "filter", "", "report only events about cards matching this query (the web filter's syntax; see changes --help; \"\" for none this run)")
	fl.StringArrayVar(&f.tags, "tag", nil, "short for --filter 'tag:<tag>' (repeatable; all of --filter, --tag, --assignee are OR-ed)")
	fl.StringArrayVar(&f.assignees, "assignee", nil, "short for --filter 'assignee:<handle>' (repeatable; me, none and any work too)")
	fl.BoolVar(&f.save, "save", false, "remember the filter for this --as name, for later runs")
	fl.BoolVar(&f.show, "show-filter", false, "print this --as name's saved filter and exit")
	fl.BoolVar(&f.clear, "clear-filter", false, "forget this --as name's saved filter and exit")
}

// filterPath is where the cursor's saved filter lives, next to its snapshot.
func filterPath(cpath string) string { return strings.TrimSuffix(cpath, ".md") + ".filter" }

func loadFilter(cpath string) (string, error) {
	b, err := os.ReadFile(filterPath(cpath))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// expression is the filter the flags give: --filter, --tag and --assignee
// as alternatives.
func (f *filterFlags) expression() (string, error) {
	var parts []string
	if strings.TrimSpace(f.query) != "" {
		parts = append(parts, strings.TrimSpace(f.query))
	}
	for _, t := range f.tags {
		n, err := task.NormalizeTag(t)
		if err != nil {
			return "", err
		}
		parts = append(parts, "tag:"+n)
	}
	for _, a := range f.assignees {
		v := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "@"))
		if v != "me" && v != "none" && v != "any" {
			h, err := task.ParseHandle(a)
			if err != nil {
				return "", err
			}
			v = h
		}
		parts = append(parts, "assignee:"+v)
	}
	return strings.Join(parts, " OR "), nil
}

// resolve returns the filter in force: the flags', else the one saved for
// this cursor, else nil. done is true when --show-filter or --clear-filter
// has said its piece and the command should stop.
func (f *filterFlags) resolve(cmd *cobra.Command, cpath, as string) (q *filter.Query, done bool, err error) {
	given := cmd.Flags().Changed("filter") || len(f.tags) > 0 || len(f.assignees) > 0
	switch {
	case f.show:
		saved, err := loadFilter(cpath)
		if err != nil {
			return nil, true, err
		}
		if saved == "" {
			fmt.Printf("cursor %q has no saved filter\n", as)
		} else {
			fmt.Println(saved)
		}
		return nil, true, nil
	case f.clear:
		if err := os.Remove(filterPath(cpath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, true, err
		}
		fmt.Printf("cursor %q: saved filter cleared\n", as)
		return nil, true, nil
	case f.save && !given:
		return nil, true, errors.New("--save needs a filter: --filter, --tag or --assignee")
	}
	var expr string
	if given {
		if expr, err = f.expression(); err != nil {
			return nil, false, err
		}
	} else if expr, err = loadFilter(cpath); err != nil {
		return nil, false, err
	}
	if expr == "" {
		if f.save {
			return nil, true, errors.New("--save needs a filter, not an empty one; --clear-filter forgets it")
		}
		return nil, false, nil // --filter "": none for this run
	}
	if q, err = filter.Parse(expr); err != nil {
		return nil, false, err
	}
	if f.save {
		if err := os.MkdirAll(dirOf(cpath), 0o755); err != nil {
			return nil, false, err
		}
		if err := os.WriteFile(filterPath(cpath), []byte(q.String()+"\n"), 0o644); err != nil {
			return nil, false, err
		}
		fmt.Fprintf(os.Stderr, "todomd: filter saved for cursor %q: %s\n", as, q)
	}
	return q, false, nil
}

func dirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

// passesFilter reports whether the filter lets e through: the card matches
// now or matched before the change (it left the filter, or was deleted),
// or the change @mentions one of me (unless the filter turned that off).
func passesFilter(q *filter.Query, e changes.Event, old map[string]oldCard, me []string) bool {
	if q.Match(e.Task, e.Board, me) {
		return true
	}
	if o, ok := old[e.Task.ID]; ok && q.Match(o.t, o.board, me) {
		return true
	}
	return q.MentionsPass() && mentionsMe(e, me)
}

// mentionsMe reports whether e is a comment that @mentions one of me, or a
// card or description change that newly does.
func mentionsMe(e changes.Event, me []string) bool {
	switch e.Type {
	case changes.CommentAdded:
		return e.Comment != nil && filter.Mentions(&task.Task{Description: e.Comment.Text}, me)
	case changes.TaskAdded:
		return filter.Mentions(&task.Task{Description: e.Task.Description}, me)
	case changes.TaskUpdated:
		fc, ok := e.Fields["description"]
		if !ok {
			return false
		}
		oldD, _ := fc.Old.(string)
		newD, _ := fc.New.(string)
		return filter.Mentions(&task.Task{Description: newD}, me) && !filter.Mentions(&task.Task{Description: oldD}, me)
	}
	return false
}

type oldCard struct {
	t     *task.Task
	board string
}

func indexOld(f *task.File) map[string]oldCard {
	m := map[string]oldCard{}
	for _, b := range f.Boards {
		for _, t := range b.Tasks {
			m[t.ID] = oldCard{t, b.Name}
		}
	}
	return m
}

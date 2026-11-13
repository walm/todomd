package store

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// FindComment picks one of t's comments the way `todomd react` names it:
// its 1-based number as `show` prints it, or "last". A non-empty match
// guards against the list having changed since the caller looked: the
// comment's text must contain it, ignoring case and runs of white space.
// It returns the comment's index.
func FindComment(t *task.Task, which, match string) (int, error) {
	n := len(t.Comments)
	if n == 0 {
		return 0, fmt.Errorf("task %s has no comments", t.ID)
	}
	var k int
	switch w := strings.ToLower(strings.TrimSpace(which)); w {
	case "last":
		k = n - 1
	default:
		num, err := strconv.Atoi(strings.TrimPrefix(w, "#"))
		if err != nil {
			return 0, fmt.Errorf("comment %q: want its number as `show` prints it (1-%d) or last", which, n)
		}
		if num < 1 || num > n {
			return 0, fmt.Errorf("task %s has no comment %d (it has %d)", t.ID, num, n)
		}
		k = num - 1
	}
	if m := fold(match); m != "" && !strings.Contains(fold(t.Comments[k].Text), m) {
		c := t.Comments[k]
		return 0, fmt.Errorf("comment %d on %s is %s's %q, which doesn't contain %q; run `todomd show %s` and try again",
			k+1, t.ID, c.Author, excerpt(c.Text, 60), match, t.ID)
	}
	return k, nil
}

// fold lowercases s and collapses its white space, for a forgiving match.
func fold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func excerpt(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// React adds name's reaction emoji to one comment on the task matching ref
// (see FindComment), or with remove takes it away. It returns the task, the
// comment's index and whether anything changed.
func React(f *task.File, ref, which, match, emoji, name string, remove bool) (*task.Task, int, bool, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, 0, false, err
	}
	t := b.Tasks[i]
	k, err := FindComment(t, which, match)
	if err != nil {
		return nil, 0, false, err
	}
	changed, err := t.Comments[k].React(emoji, name, remove)
	if err != nil {
		return nil, 0, false, err
	}
	return t, k, changed, nil
}

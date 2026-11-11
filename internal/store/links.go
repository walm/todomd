package store

import (
	"fmt"
	"slices"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// LinkView is one link as a card sees it: stored on the card itself, or on
// the other card and read from this side (its kind inverted). Title and
// Board are empty when the other card isn't on the board.
type LinkView struct {
	Kind     string
	ID       string // the other card
	Title    string
	Board    string
	StoredOn string // the card whose TODO.md entry holds the link
	Missing  bool   // the other card isn't on the board
}

// LinksOf lists t's links in both directions: the ones t stores, and the
// ones other cards store that point at t. An edge stored on both ends (A
// "blocks B" on A and B "blocked by A" on B) shows once, as stored on t.
// They are ordered by kind (task.LinkKinds), then t's own in file order,
// then the others in board order.
func LinksOf(f *task.File, t *task.Task) []LinkView {
	where := map[string]*task.Board{}
	byID := map[string]*task.Task{}
	for _, b := range f.Boards {
		for _, x := range b.Tasks {
			where[x.ID], byID[x.ID] = b, x
		}
	}
	view := func(kind, id, storedOn string) LinkView {
		v := LinkView{Kind: kind, ID: id, StoredOn: storedOn}
		if x := byID[id]; x != nil {
			v.Title, v.Board = x.Title, where[id].Name
		} else {
			v.Missing = true
		}
		return v
	}
	var out []LinkView
	seen := map[task.Link]bool{}
	for _, l := range t.Links {
		if !seen[l] {
			seen[l] = true
			out = append(out, view(l.Kind, l.ID, t.ID))
		}
	}
	for _, x := range f.AllTasks() {
		if x.ID == t.ID {
			continue
		}
		for _, l := range x.Links {
			if l.ID != t.ID {
				continue
			}
			back := task.Link{Kind: task.InverseLinkKind(l.Kind), ID: x.ID}
			if back.Kind != "" && !seen[back] {
				seen[back] = true
				out = append(out, view(back.Kind, back.ID, x.ID))
			}
		}
	}
	slices.SortStableFunc(out, func(a, b LinkView) int {
		return slices.Index(task.LinkKinds, a.Kind) - slices.Index(task.LinkKinds, b.Kind)
	})
	return out
}

// ParseLinkSpec reads a `kind:id` link (`split-from:mpbv`, `blocks:3f2a`).
func ParseLinkSpec(s string) (kind, ref string, err error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", fmt.Errorf("invalid link %q (want kind:id, e.g. split-from:3f2a)", s)
	}
	kind, err = task.ParseLinkKind(s[:i])
	if err != nil {
		return "", "", err
	}
	return kind, strings.TrimSpace(s[i+1:]), nil
}

// LinkTarget resolves the card a new link points at: it must be on the
// board (a unique id prefix works) and isn't t itself.
func LinkTarget(f *task.File, t *task.Task, ref string) (string, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return "", err
	}
	id := b.Tasks[i].ID
	if t != nil && id == t.ID {
		return "", fmt.Errorf("a card can't link to itself (%s)", id)
	}
	return id, nil
}

// HasEdge reports whether the link "from kind to" is already on the board,
// stored on either card.
func HasEdge(f *task.File, from string, l task.Link) bool {
	for _, x := range f.AllTasks() {
		switch x.ID {
		case from:
			if slices.Contains(x.Links, l) {
				return true
			}
		case l.ID:
			if slices.Contains(x.Links, task.Link{Kind: task.InverseLinkKind(l.Kind), ID: from}) {
				return true
			}
		}
	}
	return false
}

// AddLink stores "ref kind target" on the card ref. added is false when the
// board already has that link, stored on either card; nothing changes then.
func AddLink(f *task.File, ref, kind, target string) (t *task.Task, l task.Link, added bool, err error) {
	if kind, err = task.ParseLinkKind(kind); err != nil {
		return nil, l, false, err
	}
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, l, false, err
	}
	t = b.Tasks[i]
	id, err := LinkTarget(f, t, target)
	if err != nil {
		return nil, l, false, err
	}
	l = task.Link{Kind: kind, ID: id}
	if HasEdge(f, t.ID, l) {
		return t, l, false, nil
	}
	if len(t.Links) >= task.MaxLinks {
		return nil, l, false, fmt.Errorf("%s already has %d links, the most a card can have", t.ID, task.MaxLinks)
	}
	t.Links = append(t.Links, l)
	return t, l, true, nil
}

// LinkNotFoundError: no link between two cards (of the given kind).
type LinkNotFoundError struct{ A, B, Kind string }

func (e *LinkNotFoundError) Error() string {
	if e.Kind != "" {
		return fmt.Sprintf("no %q link between %s and %s", e.Kind, e.A, e.B)
	}
	return fmt.Sprintf("no link between %s and %s", e.A, e.B)
}

// RemovedLink is a link RemoveLinks took off a card.
type RemovedLink struct {
	StoredOn string
	task.Link
}

// RemoveLinks removes the links between a and b, wherever they are stored.
// With kind, only that relation is removed, read from a's side ("a blocks
// b": `blocks` on a, or `blocked by` on b). Either card may be off the board
// (a link to an archived card), but not both.
func RemoveLinks(f *task.File, a, b, kind string) ([]RemovedLink, error) {
	if kind != "" {
		var err error
		if kind, err = task.ParseLinkKind(kind); err != nil {
			return nil, err
		}
	}
	aID, aErr := linkEnd(f, a)
	bID, bErr := linkEnd(f, b)
	switch {
	case aErr != nil && bErr != nil:
		return nil, aErr
	case aErr != nil && !task.ValidID(a):
		return nil, aErr
	case bErr != nil && !task.ValidID(b):
		return nil, bErr
	}
	if aErr != nil {
		aID = a
	}
	if bErr != nil {
		bID = b
	}
	var out []RemovedLink
	drop := func(on string, to string, kind string) {
		for _, x := range f.AllTasks() {
			if x.ID != on {
				continue
			}
			x.Links = slices.DeleteFunc(x.Links, func(l task.Link) bool {
				if l.ID == to && (kind == "" || l.Kind == kind) {
					out = append(out, RemovedLink{StoredOn: on, Link: l})
					return true
				}
				return false
			})
			if len(x.Links) == 0 {
				x.Links = nil
			}
		}
	}
	inv := ""
	if kind != "" {
		inv = task.InverseLinkKind(kind)
	}
	drop(aID, bID, kind)
	drop(bID, aID, inv)
	if len(out) == 0 {
		return nil, &LinkNotFoundError{A: aID, B: bID, Kind: kind}
	}
	return out, nil
}

// linkEnd resolves a card on the board by id or unique prefix.
func linkEnd(f *task.File, ref string) (string, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return "", err
	}
	return b.Tasks[i].ID, nil
}

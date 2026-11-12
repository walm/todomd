package merge

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

// ValidationError says why a card or the file frame can't be represented in
// a TODO.md exactly as it is.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Msg }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

func validDate(d task.Date) bool {
	p, err := task.ParseDate(d.String())
	return err == nil && p == d
}

// SameTask reports whether two tasks are equal field for field.
func SameTask(a, b *task.Task) bool {
	return a.ID == b.ID && sameContent(a, b)
}

// Normalize brings a model to exactly the form Parse(Write(f)) returns, so
// two models can be compared directly.
func Normalize(f *task.File) (*task.File, error) {
	return markdown.Parse(markdown.Write(f))
}

// CheckTask fails unless the card reads back exactly as it is: a well-formed
// id, a single-line title, valid fields, and text that can't be mistaken for
// structure or for git conflict markers.
func CheckTask(t *task.Task) error {
	if !task.ValidID(t.ID) {
		return invalid("id", "malformed task id %q", t.ID)
	}
	texts := append([]string{t.Title, t.Description}, t.Tags...)
	for _, c := range t.Comments {
		texts = append(texts, c.Author, c.Text)
	}
	for _, s := range texts {
		if !utf8.ValidString(s) {
			return invalid("text", "text is not valid UTF-8")
		}
	}
	if t.Due != nil && !validDate(*t.Due) {
		return invalid("due", "invalid date %q", t.Due.String())
	}
	for _, c := range t.Comments {
		if !validDate(c.Date) {
			return invalid("comments", "invalid comment date %q", c.Date.String())
		}
	}
	if t.Sticker != "" && !task.ValidSticker(t.Sticker) {
		return invalid("sticker", "unknown sticker %q", t.Sticker)
	}
	if err := checkLinks(t); err != nil {
		return err
	}
	// Alone in a fragment an open fence reads back fine; in a file it would
	// swallow every card after it.
	if markdown.UnclosedFence(t.Description) {
		return invalid("description", "description contains an unclosed code fence")
	}
	back, err := markdown.ParseTaskStrict(markdown.WriteTask(t))
	if err != nil {
		return invalid("task", "task %q can't be represented in TODO.md", t.Title)
	}
	if !SameTask(back, t) {
		return invalid("task", "task %q would read back differently; check its title, tags and text", t.Title)
	}
	if markdown.ConflictMarkerLine(t.Title) > 0 {
		return invalid("title", "title contains git conflict markers")
	}
	if markdown.ConflictMarkerLine(t.Description) > 0 {
		return invalid("description", "description contains git conflict markers")
	}
	for _, c := range t.Comments {
		if markdown.ConflictMarkerLine(c.Text) > 0 {
			return invalid("comments", "comment contains git conflict markers")
		}
	}
	return nil
}

// checkLinks: known kinds, well-formed ids, no link to the card itself, no
// duplicates, at most task.MaxLinks. The target needn't exist: links to
// archived or deleted cards are kept.
func checkLinks(t *task.Task) error {
	if len(t.Links) > task.MaxLinks {
		return invalid("links", "a card can have at most %d links (task %q has %d)", task.MaxLinks, t.ID, len(t.Links))
	}
	seen := map[task.Link]bool{}
	for _, l := range t.Links {
		switch {
		case !task.ValidLinkKind(l.Kind):
			return invalid("links", "unknown link kind %q", l.Kind)
		case !task.ValidID(l.ID):
			return invalid("links", "malformed task id %q in a link", l.ID)
		case l.ID == t.ID:
			return invalid("links", "task %q links to itself", t.ID)
		case seen[l]:
			return invalid("links", "task %q has the link %q twice", t.ID, l.String())
		}
		seen[l] = true
	}
	return nil
}

// CheckFrame checks the file-level fields: title, preamble and board names.
func CheckFrame(title, preamble string, boards []string) error {
	for _, s := range append([]string{title, preamble}, boards...) {
		if !utf8.ValidString(s) {
			return invalid("text", "text is not valid UTF-8")
		}
	}
	// Like a description: with no boards after it an open fence reads back
	// fine, but it would swallow every board that follows.
	if markdown.UnclosedFence(preamble) {
		return invalid("preamble", "preamble contains an unclosed code fence")
	}
	frame := &task.File{Title: title, Preamble: preamble}
	for _, n := range boards {
		frame.Boards = append(frame.Boards, &task.Board{Name: n})
	}
	back, err := markdown.ParseStrict(markdown.Write(frame))
	if err != nil {
		return invalid("boards", "%s", err.Error())
	}
	// An empty title is the same as "TODO".
	want := title
	if want == "" {
		want = "TODO"
	}
	if back.Title != want {
		return invalid("title", "title would read back differently")
	}
	if back.Preamble != preamble {
		return invalid("preamble", "preamble would read back differently")
	}
	names := make([]string, len(back.Boards))
	for i, b := range back.Boards {
		names[i] = b.Name
	}
	if !slices.Equal(names, boards) {
		return invalid("boards", "board names would read back differently")
	}
	if markdown.ConflictMarkerLine(preamble) > 0 {
		return invalid("preamble", "preamble contains git conflict markers")
	}
	return nil
}

// CheckFile runs CheckFrame on f and CheckTask on every task.
func CheckFile(f *task.File) error {
	names := make([]string, len(f.Boards))
	for i, b := range f.Boards {
		names[i] = b.Name
	}
	if err := CheckFrame(f.Title, f.Preamble, names); err != nil {
		return err
	}
	for _, t := range f.AllTasks() {
		if err := CheckTask(t); err != nil {
			return err
		}
	}
	return nil
}

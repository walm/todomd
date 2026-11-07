// Package store is the single mutation API over a TODO.md file, shared by
// the CLI and TUI. Every mutation runs as: advisory flock → load → apply →
// atomic write → unlock, so concurrent writers serialize instead of losing
// updates.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/statedir"
	"github.com/walm/todomd/internal/task"
)

// DefaultFileName is the file searched for when none is specified.
const DefaultFileName = "TODO.md"

// ErrNoFile is returned when no TODO.md can be located.
var ErrNoFile = errors.New("no TODO.md found (run 'todomd init' to create one, or pass --file)")

// NotFoundError: no task matches the given ID or prefix.
type NotFoundError struct{ Ref string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("no task with id %q", e.Ref) }

// AmbiguousError: an ID prefix matches more than one task.
type AmbiguousError struct {
	Ref     string
	Matches []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("ambiguous id %q matches: %s", e.Ref, strings.Join(e.Matches, ", "))
}

// Discover resolves the file path: explicit flag > TODOMD_FILE env > walk
// from cwd upward looking for TODO.md, stopping at (and including) the first
// directory containing .git.
func Discover(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if env := os.Getenv("TODOMD_FILE"); env != "" {
		return filepath.Abs(env)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, DefaultFileName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "", ErrNoFile
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoFile
		}
		dir = parent
	}
}

// Store reads and mutates one TODO.md.
type Store struct {
	Path string
}

// Load parses the file.
func (s *Store) Load() (*task.File, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoFile
		}
		return nil, err
	}
	f, err := markdown.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Path, err)
	}
	return f, nil
}

// Save writes the file atomically (temp file + rename in the same
// directory), preserving existing permissions.
func (s *Store) Save(f *task.File) error {
	perm := os.FileMode(0o644)
	if st, err := os.Stat(s.Path); err == nil {
		perm = st.Mode().Perm()
	}
	dir := filepath.Dir(s.Path)
	tmp, err := os.CreateTemp(dir, ".todomd-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(markdown.Write(f)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Mutate locks the file, loads it fresh, applies fn, and writes the result.
// fn must express its change in terms of task IDs / board names, never
// pointers into a previously loaded model.
//
// The flock target lives in the per-file state dir, not next to the todo
// file — flock is same-machine anyway, and this keeps repositories free of
// .lock sidecars. The lock file is intentionally never deleted (unlink
// after unlock races against concurrent lockers).
func (s *Store) Mutate(fn func(*task.File) error) error {
	return s.withLock(func() error {
		f, err := s.Load()
		if err != nil {
			return err
		}
		if err := fn(f); err != nil {
			return err
		}
		return s.Save(f)
	})
}

// MutateTracked is Mutate plus the pre-mutation state, so a caller can compute
// the delta it just wrote (see internal/changes.Diff).
func (s *Store) MutateTracked(fn func(*task.File) error) (before, after *task.File, err error) {
	err = s.withLock(func() error {
		f, err := s.Load()
		if err != nil {
			return err
		}
		// Copy via a canonical round-trip: Write emits every task's id, so the
		// copy is stable. (Loading the file twice could mint different ids for
		// tasks that don't have one yet, which would diff as delete+add.)
		if before, err = markdown.Parse(markdown.Write(f)); err != nil {
			return err
		}
		if err := fn(f); err != nil {
			return err
		}
		after = f
		return s.Save(f)
	})
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// withLock runs fn holding the exclusive advisory lock for this file.
func (s *Store) withLock(fn func() error) error {
	dir, err := statedir.For(s.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", s.Path, err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	return fn()
}

// Init creates a new file with the default boards. Fails if it exists.
func Init(path, title string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if title == "" {
		title = "TODO"
	}
	f := &task.File{
		Title: title,
		Boards: []*task.Board{
			{Name: "Backlog"}, {Name: "In Progress"}, {Name: "Done"},
		},
	}
	s := &Store{Path: path}
	return s.Save(f)
}

// FindTask resolves an exact ID or unique prefix to its board and index.
func FindTask(f *task.File, ref string) (*task.Board, int, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return nil, 0, &NotFoundError{Ref: ref}
	}
	type hit struct {
		b *task.Board
		i int
	}
	var hits []hit
	for _, b := range f.Boards {
		for i, t := range b.Tasks {
			if t.ID == ref {
				return b, i, nil
			}
			if strings.HasPrefix(t.ID, ref) {
				hits = append(hits, hit{b, i})
			}
		}
	}
	switch len(hits) {
	case 0:
		return nil, 0, &NotFoundError{Ref: ref}
	case 1:
		return hits[0].b, hits[0].i, nil
	default:
		ids := make([]string, len(hits))
		for i, h := range hits {
			ids[i] = h.b.Tasks[h.i].ID
		}
		return nil, 0, &AmbiguousError{Ref: ref, Matches: ids}
	}
}

// FindBoard matches a board name case-insensitively.
func FindBoard(f *task.File, name string) *task.Board {
	for _, b := range f.Boards {
		if strings.EqualFold(b.Name, strings.TrimSpace(name)) {
			return b
		}
	}
	return nil
}

// EnsureBoard finds or creates a board. New boards are inserted before
// "Done" when present, otherwise appended.
func EnsureBoard(f *task.File, name string) (*task.Board, error) {
	if b := FindBoard(f, name); b != nil {
		return b, nil
	}
	name, err := task.ValidateBoardName(name)
	if err != nil {
		return nil, err
	}
	nb := &task.Board{Name: name}
	insertBoard(f, defaultBoardIndex(f), nb)
	return nb, nil
}

// defaultBoardIndex is where a new board goes when nothing else is asked
// for: before "Done" when present, otherwise last (as todomd.online does).
func defaultBoardIndex(f *task.File) int {
	for i, b := range f.Boards {
		if task.SameBoard(b.Name, "Done") {
			return i
		}
	}
	return len(f.Boards)
}

func insertBoard(f *task.File, i int, b *task.Board) {
	f.Boards = append(f.Boards[:i], append([]*task.Board{b}, f.Boards[i:]...)...)
}

func boardIndex(f *task.File, b *task.Board) int {
	for i, x := range f.Boards {
		if x == b {
			return i
		}
	}
	return -1
}

// BoardOf returns the board containing t, or nil.
func BoardOf(f *task.File, t *task.Task) *task.Board {
	for _, b := range f.Boards {
		for _, bt := range b.Tasks {
			if bt == t {
				return b
			}
		}
	}
	return nil
}

func validateMultiline(field, s string) (string, error) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Trim(s, "\n")
	if markdown.UnclosedFence(s) {
		return "", fmt.Errorf("%s contains an unclosed code fence", field)
	}
	return s, nil
}

// Add validates and appends a new task to the named board (first board when
// empty), assigning it a fresh ID. Returns the created task.
func Add(f *task.File, boardName string, t *task.Task) (*task.Task, error) {
	var err error
	if t.Title, err = task.ValidateTitle(t.Title); err != nil {
		return nil, err
	}
	if t.Description, err = validateMultiline("description", t.Description); err != nil {
		return nil, err
	}
	for i, tag := range t.Tags {
		if t.Tags[i], err = task.NormalizeTag(tag); err != nil {
			return nil, err
		}
	}
	var b *task.Board
	if boardName == "" {
		if len(f.Boards) == 0 {
			return nil, errors.New("file has no boards; pass --board to create one")
		}
		b = f.Boards[0]
	} else if b, err = EnsureBoard(f, boardName); err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, x := range f.AllTasks() {
		taken[x.ID] = true
	}
	t.ID = task.NewID(taken)
	b.Tasks = append(b.Tasks, t)
	return t, nil
}

// UpdateOpts carries optional field changes; nil pointers mean "unchanged".
type UpdateOpts struct {
	Title       *string
	Description *string
	Tags        *[]string // replaces the whole set
	Priority    *task.Priority
	Due         *task.Date
	ClearDue    bool
	ClearTags   bool
}

// Update applies opts to the task matching ref.
func Update(f *task.File, ref string, opts UpdateOpts) (*task.Task, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, err
	}
	t := b.Tasks[i]
	if opts.Title != nil {
		if t.Title, err = task.ValidateTitle(*opts.Title); err != nil {
			return nil, err
		}
	}
	if opts.Description != nil {
		if t.Description, err = validateMultiline("description", *opts.Description); err != nil {
			return nil, err
		}
	}
	if opts.ClearTags {
		t.Tags = nil
	}
	if opts.Tags != nil {
		tags := make([]string, len(*opts.Tags))
		for i, tag := range *opts.Tags {
			if tags[i], err = task.NormalizeTag(tag); err != nil {
				return nil, err
			}
		}
		t.Tags = tags
	}
	if opts.Priority != nil {
		t.Priority = *opts.Priority
	}
	if opts.ClearDue {
		t.Due = nil
	}
	if opts.Due != nil {
		d := *opts.Due
		t.Due = &d
	}
	return t, nil
}

// Move relocates the task matching ref. to == "" keeps its current board;
// pos is a 1-based position in the target after removal, 0 (or > len)
// appends; pos < 0 is an error (checked by the caller via flag parsing).
func Move(f *task.File, ref, to string, pos int) (*task.Task, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, err
	}
	t := b.Tasks[i]
	target := b
	if to != "" {
		if target, err = EnsureBoard(f, to); err != nil {
			return nil, err
		}
	}
	b.Tasks = append(b.Tasks[:i], b.Tasks[i+1:]...)
	idx := len(target.Tasks)
	if pos > 0 && pos <= len(target.Tasks) {
		idx = pos - 1
	}
	target.Tasks = append(target.Tasks[:idx], append([]*task.Task{t}, target.Tasks[idx:]...)...)
	return t, nil
}

// Delete removes the task matching ref, returning it and the board it was on.
func Delete(f *task.File, ref string) (*task.Task, string, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, "", err
	}
	t := b.Tasks[i]
	b.Tasks = append(b.Tasks[:i], b.Tasks[i+1:]...)
	return t, b.Name, nil
}

// BoardNotEmptyError: the board still holds tasks and force wasn't given.
type BoardNotEmptyError struct {
	Name  string
	Tasks int
}

func (e *BoardNotEmptyError) Error() string {
	return fmt.Sprintf("board %q still holds %d task(s), which would be deleted with it", e.Name, e.Tasks)
}

// DeleteBoard removes a board and returns it. A board with tasks needs force,
// because its tasks go with it.
func DeleteBoard(f *task.File, name string, force bool) (*task.Board, error) {
	b := FindBoard(f, name)
	if b == nil {
		return nil, &BoardNotFoundError{Name: name}
	}
	if len(b.Tasks) > 0 && !force {
		return nil, &BoardNotEmptyError{Name: b.Name, Tasks: len(b.Tasks)}
	}
	i := boardIndex(f, b)
	f.Boards = append(f.Boards[:i], f.Boards[i+1:]...)
	return b, nil
}

// BoardNotFoundError: no board has the given name (compared ignoring case).
type BoardNotFoundError struct{ Name string }

func (e *BoardNotFoundError) Error() string { return fmt.Sprintf("no board named %q", e.Name) }

// BoardExistsError: the name is already taken by another board. Board names
// are compared ignoring case (task.BoardKey), so "review" clashes with
// "Review".
type BoardExistsError struct {
	Name     string // the name asked for
	Existing string // the board that already has it, as spelled in the file
}

func (e *BoardExistsError) Error() string {
	if e.Name == e.Existing {
		return fmt.Sprintf("a board named %q already exists", e.Existing)
	}
	return fmt.Sprintf("board name %q is taken by %q (board names ignore case)", e.Name, e.Existing)
}

// BoardPlace says where a board goes. At most one field is set; the zero
// value means "the default place" (see AddBoard and MoveBoard).
type BoardPlace struct {
	Pos    int    // 1-based position among the boards after the change; > count means last
	After  string // directly after this board
	Before string // directly before this board
}

func (p BoardPlace) isZero() bool { return p == BoardPlace{} }

// index resolves p to an insertion index into f.Boards, which must not
// contain the board being placed. self names that board, so placing it
// relative to itself is refused.
func (p BoardPlace) index(f *task.File, self string) (int, error) {
	set := 0
	for _, on := range []bool{p.Pos != 0, p.After != "", p.Before != ""} {
		if on {
			set++
		}
	}
	if set > 1 {
		return 0, errors.New("give only one of a position, after or before")
	}
	rel := func(name string, offset int) (int, error) {
		if task.SameBoard(strings.TrimSpace(name), self) {
			return 0, fmt.Errorf("can't place board %q relative to itself", self)
		}
		b := FindBoard(f, name)
		if b == nil {
			return 0, &BoardNotFoundError{Name: name}
		}
		return boardIndex(f, b) + offset, nil
	}
	switch {
	case p.After != "":
		return rel(p.After, 1)
	case p.Before != "":
		return rel(p.Before, 0)
	case p.Pos < 0:
		return 0, errors.New("position must be >= 1")
	case p.Pos > 0:
		return min(p.Pos-1, len(f.Boards)), nil
	}
	return defaultBoardIndex(f), nil
}

// AddBoard creates a board at the given place; the zero place means before
// "Done" when present, otherwise last. A name that is already taken,
// ignoring case, is a *BoardExistsError.
func AddBoard(f *task.File, name string, at BoardPlace) (*task.Board, error) {
	name, err := task.ValidateBoardName(name)
	if err != nil {
		return nil, err
	}
	if b := FindBoard(f, name); b != nil {
		return nil, &BoardExistsError{Name: name, Existing: b.Name}
	}
	i, err := at.index(f, name)
	if err != nil {
		return nil, err
	}
	nb := &task.Board{Name: name}
	insertBoard(f, i, nb)
	return nb, nil
}

// RenameBoard renames a board, keeping its tasks and position, and returns
// it with its old name. A change of case only is allowed; a name taken by
// another board, ignoring case, is a *BoardExistsError. Renaming to the
// exact current name is a no-op.
func RenameBoard(f *task.File, name, newName string) (b *task.Board, oldName string, err error) {
	b = FindBoard(f, name)
	if b == nil {
		return nil, "", &BoardNotFoundError{Name: name}
	}
	if newName, err = task.ValidateBoardName(newName); err != nil {
		return nil, "", err
	}
	for _, x := range f.Boards {
		if x != b && task.SameBoard(x.Name, newName) {
			return nil, "", &BoardExistsError{Name: newName, Existing: x.Name}
		}
	}
	oldName = b.Name
	b.Name = newName
	return b, oldName, nil
}

// MoveBoard moves a board, with its tasks, to another place among the
// boards. at must name a place.
func MoveBoard(f *task.File, name string, at BoardPlace) (*task.Board, error) {
	b := FindBoard(f, name)
	if b == nil {
		return nil, &BoardNotFoundError{Name: name}
	}
	if at.isZero() {
		return nil, errors.New("say where to move the board: a position, after or before")
	}
	from := boardIndex(f, b)
	rest := &task.File{Boards: append(append([]*task.Board{}, f.Boards[:from]...), f.Boards[from+1:]...)}
	i, err := at.index(rest, b.Name)
	if err != nil {
		return nil, err
	}
	insertBoard(rest, i, b)
	f.Boards = rest.Boards
	return b, nil
}

// BoardPosition is b's 1-based position in f, or 0 when it isn't there.
func BoardPosition(f *task.File, b *task.Board) int { return boardIndex(f, b) + 1 }

// AddComment appends a comment dated today to the task matching ref.
func AddComment(f *task.File, ref, author, text string) (*task.Task, error) {
	b, i, err := FindTask(f, ref)
	if err != nil {
		return nil, err
	}
	if author, err = task.ValidateAuthor(author); err != nil {
		return nil, err
	}
	if text, err = validateMultiline("comment text", text); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errors.New("comment text must not be empty")
	}
	t := b.Tasks[i]
	t.Comments = append(t.Comments, task.Comment{Author: author, Date: task.Today(), Text: text})
	return t, nil
}

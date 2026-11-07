package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/walm/todomd/internal/task"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "TODO.md")
	if err := Init(path, "Test"); err != nil {
		t.Fatal(err)
	}
	return &Store{Path: path}
}

func addTask(t *testing.T, s *Store, board, title string) string {
	t.Helper()
	var id string
	err := s.Mutate(func(f *task.File) error {
		added, err := Add(f, board, &task.Task{Title: title})
		if err == nil {
			id = added.ID
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestInitAndLoad(t *testing.T) {
	s := newTestStore(t)
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Boards) != 3 || f.Boards[0].Name != "Backlog" || f.Boards[2].Name != "Done" {
		t.Fatalf("boards = %+v", f.Boards)
	}
	if err := Init(s.Path, "X"); err == nil {
		t.Error("second init should fail")
	}
}

func TestAddFindMoveDelete(t *testing.T) {
	s := newTestStore(t)
	id := addTask(t, s, "", "First task")

	f, _ := s.Load()
	b, i, err := FindTask(f, id)
	if err != nil || b.Name != "Backlog" || b.Tasks[i].Title != "First task" {
		t.Fatalf("find: %v %v", b, err)
	}
	// Prefix match.
	if _, _, err := FindTask(f, id[:2]); err != nil {
		t.Errorf("prefix find: %v", err)
	}
	if _, _, err := FindTask(f, "zzzz"); err == nil {
		t.Error("want NotFoundError")
	}

	// Move to a new board (created before Done), then delete.
	err = s.Mutate(func(f *task.File) error {
		_, err := Move(f, id, "Review", 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ = s.Load()
	names := make([]string, len(f.Boards))
	for i, b := range f.Boards {
		names[i] = b.Name
	}
	if strings.Join(names, ",") != "Backlog,In Progress,Review,Done" {
		t.Errorf("boards = %v", names)
	}
	err = s.Mutate(func(f *task.File) error {
		_, _, err := Delete(f, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ = s.Load()
	if _, _, err := FindTask(f, id); err == nil {
		t.Error("task should be gone")
	}
	// Empty boards persist.
	if len(f.Boards) != 4 {
		t.Errorf("boards after delete = %d", len(f.Boards))
	}
}

func TestMovePositions(t *testing.T) {
	s := newTestStore(t)
	a := addTask(t, s, "", "A")
	b := addTask(t, s, "", "B")
	c := addTask(t, s, "", "C")
	_ = b
	// Move C to position 1.
	err := s.Mutate(func(f *task.File) error {
		_, err := Move(f, c, "", 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Load()
	titles := []string{}
	for _, tk := range f.Boards[0].Tasks {
		titles = append(titles, tk.Title)
	}
	if strings.Join(titles, "") != "CAB" {
		t.Errorf("order = %v", titles)
	}
	// pos beyond len appends.
	err = s.Mutate(func(f *task.File) error {
		_, err := Move(f, a, "", 99)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ = s.Load()
	last := f.Boards[0].Tasks[len(f.Boards[0].Tasks)-1]
	if last.Title != "A" {
		t.Errorf("last = %q", last.Title)
	}
}

func TestBoardCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	id := addTask(t, s, "", "X")
	err := s.Mutate(func(f *task.File) error {
		_, err := Move(f, id, "in progress", 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Load()
	if len(f.Boards) != 3 {
		t.Errorf("case-insensitive match failed, boards = %d", len(f.Boards))
	}
	if len(f.Boards[1].Tasks) != 1 {
		t.Errorf("task not on In Progress")
	}
}

func TestValidationRejects(t *testing.T) {
	s := newTestStore(t)
	cases := []*task.Task{
		{Title: "multi\nline"},
		{Title: ""},
		{Title: "ok", Description: "```\nunclosed fence"},
		{Title: "ok", Tags: []string{"Bad Tag!"}},
	}
	for _, tk := range cases {
		err := s.Mutate(func(f *task.File) error {
			_, err := Add(f, "", tk)
			return err
		})
		if err == nil {
			t.Errorf("expected rejection for %+v", tk)
		}
	}
}

func TestAmbiguousPrefix(t *testing.T) {
	s := newTestStore(t)
	f, _ := s.Load()
	f.Boards[0].Tasks = []*task.Task{
		{ID: "ab11", Title: "one"},
		{ID: "ab22", Title: "two"},
	}
	_, _, err := FindTask(f, "ab")
	amb, ok := err.(*AmbiguousError)
	if !ok || len(amb.Matches) != 2 {
		t.Fatalf("want AmbiguousError with 2 matches, got %v", err)
	}
	// Exact match wins over prefix ambiguity.
	f.Boards[0].Tasks = append(f.Boards[0].Tasks, &task.Task{ID: "ab", Title: "exact"})
	// "ab" is not a valid generated id but FindTask should still match exactly.
	if _, i, err := FindTask(f, "ab"); err != nil || f.Boards[0].Tasks[i].Title != "exact" {
		t.Errorf("exact match should win: %v", err)
	}
}

func TestConcurrentWriters(t *testing.T) {
	s := newTestStore(t)
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.Mutate(func(f *task.File) error {
				_, err := Add(f, "", &task.Task{Title: strings.Repeat("x", i+1)})
				return err
			})
			if err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	f, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(f.Boards[0].Tasks); got != n {
		t.Errorf("lost writes: %d/%d tasks survived", got, n)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	todo := filepath.Join(dir, "TODO.md")
	if err := Init(todo, ""); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	got, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks (macOS /tmp → /private/tmp) before comparing.
	want, _ := filepath.EvalSymlinks(todo)
	gotR, _ := filepath.EvalSymlinks(got)
	if gotR != want {
		t.Errorf("got %q want %q", gotR, want)
	}

	// .git stops the walk.
	if err := os.Mkdir(filepath.Join(sub, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(""); err == nil {
		t.Error("walk should stop at .git and fail")
	}
}

func TestDeleteBoard(t *testing.T) {
	s := newTestStore(t)

	// An empty board goes without ceremony.
	err := s.Mutate(func(f *task.File) error {
		b, err := DeleteBoard(f, "In Progress", false)
		if err == nil && b.Name != "In Progress" {
			t.Errorf("returned board = %q", b.Name)
		}
		return err
	})
	if err != nil {
		t.Fatalf("empty board should delete without force: %v", err)
	}
	f, _ := s.Load()
	if FindBoard(f, "In Progress") != nil {
		t.Error("board still present")
	}

	// One with tasks refuses, and says how many would go with it.
	id := addTask(t, s, "Backlog", "keep me")
	err = s.Mutate(func(f *task.File) error {
		_, err := DeleteBoard(f, "Backlog", false)
		return err
	})
	var notEmpty *BoardNotEmptyError
	if !errors.As(err, &notEmpty) || notEmpty.Tasks != 1 {
		t.Fatalf("want BoardNotEmptyError with 1 task, got %v", err)
	}
	f, _ = s.Load()
	if _, _, err := FindTask(f, id); err != nil {
		t.Error("a refused delete must leave the tasks alone")
	}

	// With force, the board and its tasks go.
	if err := s.Mutate(func(f *task.File) error {
		_, err := DeleteBoard(f, "backlog", true) // name match is case-insensitive
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f, _ = s.Load()
	if FindBoard(f, "Backlog") != nil {
		t.Error("forced board still present")
	}
	if _, _, err := FindTask(f, id); err == nil {
		t.Error("the board's tasks should have gone with it")
	}

	// Unknown board is an error, not a silent no-op.
	if err := s.Mutate(func(f *task.File) error {
		_, err := DeleteBoard(f, "Nope", true)
		return err
	}); err == nil {
		t.Error("unknown board should error")
	}

	// Deleting the last board is allowed; the file just has none.
	if err := s.Mutate(func(f *task.File) error {
		_, err := DeleteBoard(f, "Done", false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f, _ = s.Load()
	if len(f.Boards) != 0 {
		t.Errorf("boards left = %d", len(f.Boards))
	}
}

func boardNames(f *task.File) string {
	names := make([]string, len(f.Boards))
	for i, b := range f.Boards {
		names[i] = b.Name
	}
	return strings.Join(names, ",")
}

func TestAddBoard(t *testing.T) {
	f := &task.File{Boards: []*task.Board{{Name: "Backlog"}, {Name: "In Progress"}, {Name: "Done"}}}
	steps := []struct {
		name string
		at   BoardPlace
		want string
	}{
		{"Review", BoardPlace{}, "Backlog,In Progress,Review,Done"},                        // default: before Done
		{" Ideas ", BoardPlace{After: "backlog"}, "Backlog,Ideas,In Progress,Review,Done"}, // trimmed, case-insensitive ref
		{"Inbox", BoardPlace{Before: "Backlog"}, "Inbox,Backlog,Ideas,In Progress,Review,Done"},
		{"Later", BoardPlace{After: "Done"}, "Inbox,Backlog,Ideas,In Progress,Review,Done,Later"},
		{"Second", BoardPlace{Pos: 2}, "Inbox,Second,Backlog,Ideas,In Progress,Review,Done,Later"},
		{"Last", BoardPlace{Pos: 99}, "Inbox,Second,Backlog,Ideas,In Progress,Review,Done,Later,Last"},
	}
	for _, st := range steps {
		b, err := AddBoard(f, st.name, st.at)
		if err != nil {
			t.Fatalf("AddBoard(%q, %+v): %v", st.name, st.at, err)
		}
		if b.Name != strings.TrimSpace(st.name) || len(b.Tasks) != 0 {
			t.Errorf("returned board %+v", b)
		}
		if got := boardNames(f); got != st.want {
			t.Errorf("after adding %q: %s, want %s", st.name, got, st.want)
		}
	}

	// Without a Done board, the default is last.
	g := &task.File{Boards: []*task.Board{{Name: "A"}}}
	if _, err := AddBoard(g, "B", BoardPlace{}); err != nil || boardNames(g) != "A,B" {
		t.Errorf("no Done: %s %v", boardNames(g), err)
	}
	// An empty file gets its first board.
	e := &task.File{}
	if _, err := AddBoard(e, "Only", BoardPlace{}); err != nil || boardNames(e) != "Only" {
		t.Errorf("empty file: %s %v", boardNames(e), err)
	}

	before := boardNames(f)
	var exists *BoardExistsError
	if _, err := AddBoard(f, "REVIEW", BoardPlace{}); !errors.As(err, &exists) || exists.Existing != "Review" {
		t.Errorf("duplicate ignoring case: want BoardExistsError naming Review, got %v", err)
	}
	var nf *BoardNotFoundError
	if _, err := AddBoard(f, "X", BoardPlace{After: "Nope"}); !errors.As(err, &nf) {
		t.Errorf("unknown --after: want BoardNotFoundError, got %v", err)
	}
	for _, bad := range []string{"", "  ", "two\nlines"} {
		if _, err := AddBoard(f, bad, BoardPlace{}); err == nil {
			t.Errorf("name %q should be refused", bad)
		}
	}
	if _, err := AddBoard(f, "X", BoardPlace{After: "Done", Before: "Inbox"}); err == nil {
		t.Error("two places at once should be refused")
	}
	if _, err := AddBoard(f, "X", BoardPlace{Pos: -1}); err == nil {
		t.Error("negative position should be refused")
	}
	if got := boardNames(f); got != before {
		t.Errorf("refused adds changed the boards: %s", got)
	}
}

func TestRenameBoard(t *testing.T) {
	s := newTestStore(t)
	id := addTask(t, s, "Backlog", "keep me")

	err := s.Mutate(func(f *task.File) error {
		b, old, err := RenameBoard(f, "backlog", "Todo")
		if err == nil && (b.Name != "Todo" || old != "Backlog" || len(b.Tasks) != 1) {
			t.Errorf("returned %q (was %q) with %d tasks", b.Name, old, len(b.Tasks))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.Load()
	if got := boardNames(f); got != "Todo,In Progress,Done" {
		t.Errorf("boards = %s (position must stay)", got)
	}
	if b, _, err := FindTask(f, id); err != nil || b.Name != "Todo" {
		t.Errorf("task should stay on the renamed board: %v", err)
	}

	// Case only, and the exact current name (a no-op), are fine.
	for _, name := range []string{"TODO", "TODO"} {
		if err := s.Mutate(func(f *task.File) error {
			_, _, err := RenameBoard(f, "todo", name)
			return err
		}); err != nil {
			t.Fatalf("rename to %q: %v", name, err)
		}
	}
	f, _ = s.Load()
	if got := boardNames(f); got != "TODO,In Progress,Done" {
		t.Errorf("boards = %s", got)
	}

	var exists *BoardExistsError
	var nf *BoardNotFoundError
	cases := []struct {
		from, to string
		check    func(error) bool
	}{
		{"TODO", "done", func(err error) bool { return errors.As(err, &exists) && exists.Existing == "Done" }},
		{"TODO", "In progress", func(err error) bool { return errors.As(err, &exists) }},
		{"Nope", "X", func(err error) bool { return errors.As(err, &nf) }},
		{"TODO", "", func(err error) bool { return err != nil }},
		{"TODO", " \t", func(err error) bool { return err != nil }},
		{"TODO", "a\nb", func(err error) bool { return err != nil }},
	}
	for _, c := range cases {
		err := s.Mutate(func(f *task.File) error {
			_, _, err := RenameBoard(f, c.from, c.to)
			return err
		})
		if !c.check(err) {
			t.Errorf("rename %q → %q: unexpected error %v", c.from, c.to, err)
		}
	}
	f, _ = s.Load()
	if got := boardNames(f); got != "TODO,In Progress,Done" {
		t.Errorf("refused renames changed the file: %s", got)
	}
}

func TestMoveBoard(t *testing.T) {
	newFile := func() *task.File {
		return &task.File{Boards: []*task.Board{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D", Tasks: []*task.Task{{ID: "t1", Title: "x"}}}}}
	}
	cases := []struct {
		board string
		at    BoardPlace
		want  string
	}{
		{"A", BoardPlace{Pos: 1}, "A,B,C,D"},
		{"A", BoardPlace{Pos: 3}, "B,C,A,D"},
		{"a", BoardPlace{Pos: 4}, "B,C,D,A"},
		{"A", BoardPlace{Pos: 99}, "B,C,D,A"},
		{"D", BoardPlace{Pos: 1}, "D,A,B,C"},
		{"A", BoardPlace{After: "c"}, "B,C,A,D"},
		{"A", BoardPlace{After: "D"}, "B,C,D,A"},
		{"D", BoardPlace{Before: "A"}, "D,A,B,C"},
		{"D", BoardPlace{Before: "C"}, "A,B,D,C"},
		{"B", BoardPlace{After: "A"}, "A,B,C,D"}, // already there
	}
	for _, c := range cases {
		f := newFile()
		b, err := MoveBoard(f, c.board, c.at)
		if err != nil {
			t.Errorf("MoveBoard(%q, %+v): %v", c.board, c.at, err)
			continue
		}
		if got := boardNames(f); got != c.want {
			t.Errorf("MoveBoard(%q, %+v) = %s, want %s", c.board, c.at, got, c.want)
		}
		if !strings.EqualFold(b.Name, c.board) {
			t.Errorf("returned %q", b.Name)
		}
	}

	// Tasks travel with the board.
	f := newFile()
	if _, err := MoveBoard(f, "D", BoardPlace{Pos: 1}); err != nil || len(f.Boards[0].Tasks) != 1 {
		t.Errorf("tasks should move with their board: %v", err)
	}

	var nf *BoardNotFoundError
	bad := []struct {
		board string
		at    BoardPlace
		check func(error) bool
	}{
		{"Nope", BoardPlace{Pos: 1}, func(err error) bool { return errors.As(err, &nf) }},
		{"A", BoardPlace{After: "Nope"}, func(err error) bool { return errors.As(err, &nf) }},
		{"A", BoardPlace{}, func(err error) bool { return err != nil }},
		{"A", BoardPlace{After: "a"}, func(err error) bool { return err != nil && strings.Contains(err.Error(), "itself") }},
		{"A", BoardPlace{Pos: 1, Before: "B"}, func(err error) bool { return err != nil }},
		{"A", BoardPlace{Pos: -2}, func(err error) bool { return err != nil }},
	}
	for _, c := range bad {
		f := newFile()
		_, err := MoveBoard(f, c.board, c.at)
		if !c.check(err) {
			t.Errorf("MoveBoard(%q, %+v): unexpected error %v", c.board, c.at, err)
		}
		if got := boardNames(f); got != "A,B,C,D" {
			t.Errorf("a refused move changed the boards: %s", got)
		}
	}
}

func TestDeleteBoardNotFoundIsTyped(t *testing.T) {
	f := &task.File{Boards: []*task.Board{{Name: "A"}}}
	var nf *BoardNotFoundError
	if _, err := DeleteBoard(f, "Nope", true); !errors.As(err, &nf) {
		t.Errorf("want BoardNotFoundError, got %v", err)
	}
}

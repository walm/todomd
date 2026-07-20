package store

import (
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

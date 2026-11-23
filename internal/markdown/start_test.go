package markdown

import (
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

func TestStartRoundTrip(t *testing.T) {
	in := "# B\n\n## Todo\n\n### A\n<!-- id:aa01 -->\n`#infra` **priority:** high **start:** 2026-11-10 **due:** 2026-11-14\n\nbody\n"
	f, err := ParseStrict([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	tk := f.AllTasks()[0]
	if tk.Start == nil || *tk.Start != (task.Date{Year: 2026, Month: 11, Day: 10}) || tk.Due == nil || tk.Description != "body" {
		t.Fatalf("parsed %+v", tk)
	}
	if got := string(Write(f)); got != in {
		t.Errorf("round trip:\n%s\nwant\n%s", got, in)
	}
}

// A description that would read as a metadata line with a start date is
// escaped by the writer when the card has no metadata of its own.
func TestStartLookalikeDescriptionEscaped(t *testing.T) {
	f := &task.File{Boards: []*task.Board{{Name: "Todo", Tasks: []*task.Task{
		{ID: "aa01", Title: "A", Description: "**start:** 2026-11-10\nmore"},
	}}}}
	out := Write(f)
	if !strings.Contains(string(out), "\\**start:** 2026-11-10") {
		t.Fatalf("not escaped:\n%s", out)
	}
	g, err := ParseStrict(out)
	if err != nil {
		t.Fatal(err)
	}
	if tk := g.AllTasks()[0]; tk.Start != nil || tk.Description != "**start:** 2026-11-10\nmore" {
		t.Errorf("round trip lost it: %+v", tk)
	}
}

// start > due is kept by the reader exactly as written.
func TestStartAfterDueKept(t *testing.T) {
	in := "# B\n\n## Todo\n\n### A\n<!-- id:aa01 -->\n**start:** 2026-12-01 **due:** 2026-11-14\n"
	f, err := ParseStrict([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(Write(f)); got != in {
		t.Errorf("got\n%s", got)
	}
}

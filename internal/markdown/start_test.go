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

// todomd.online hands clients below format 6 a start date as a hidden first
// description line; UpgradeStart makes it the field again.
func TestUpgradeStart(t *testing.T) {
	in := "# B\n\n## Todo\n\n" +
		"### Downgraded\n<!-- id:aa01 -->\n`#x` **due:** 2026-11-14\n\n<!-- assignee: anna -->\n\n<!-- start: 2026-11-10 -->\n\nbody\n\n" +
		"### Only the line\n<!-- id:aa02 -->\n\n<!-- start: 2026-11-11 -->\n\n" +
		"### Has a start already\n<!-- id:aa03 -->\n**start:** 2026-11-01\n\n<!-- start: 2026-11-11 -->\n\n" +
		"### Escaped stays text\n<!-- id:aa04 -->\n\n\\<!-- start: 2026-11-11 -->\n\n" +
		"### Bad date stays text\n<!-- id:aa05 -->\n\n<!-- start: 2026-13-11 -->\n"
	f, err := ParseStrict([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if !UpgradeStart(f) {
		t.Fatal("nothing upgraded")
	}
	var got []string
	for _, tk := range f.AllTasks() {
		s := "-"
		if tk.Start != nil {
			s = tk.Start.String()
		}
		got = append(got, tk.ID+" "+s+" "+strings.ReplaceAll(tk.Description, "\n", "|"))
	}
	want := []string{
		"aa01 2026-11-10 body",
		"aa02 2026-11-11 ",
		"aa03 2026-11-01 <!-- start: 2026-11-11 -->",
		"aa04 - <!-- start: 2026-11-11 -->",
		"aa05 - <!-- start: 2026-13-11 -->",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	out := string(Write(f))
	if !strings.Contains(out, "`#x` **start:** 2026-11-10 **due:** 2026-11-14") || UpgradeStart(f) {
		t.Errorf("written:\n%s", out)
	}
}

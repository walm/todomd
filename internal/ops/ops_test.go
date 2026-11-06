package ops_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/todogen"
)

func doc(t *testing.T, s string) *task.File {
	t.Helper()
	f, err := markdown.ParseStrict([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const baseDoc = "# TODO\n\n## Backlog\n\n### One\n<!-- id:aaaa -->\n\n### Two\n<!-- id:bbbb -->\n`#x`\n\nDesc.\n\n#### Comments\n\n- **ai** (2026-01-01): hi\n\n### Three\n<!-- id:cccc -->\n\n## Done\n"

// roundTrip checks the invariant and that the ops survive JSON and the
// strict reader unchanged, and returns the ops as JSON for comparison.
func roundTrip(t *testing.T, a, b *task.File) string {
	t.Helper()
	list := ops.Delta(a, b)
	got, err := ops.Apply(a, list)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if string(markdown.Write(got)) != string(markdown.Write(b)) {
		t.Fatalf("apply(a, delta(a, b)) != b\nops: %s\ngot:\n%s\nwant:\n%s", js(t, list), markdown.Write(got), markdown.Write(b))
	}
	data := js(t, list)
	back, err := ops.Decode([]byte(data))
	if err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	if again := js(t, back); again != data {
		t.Fatalf("JSON round trip changed ops:\n%s\n%s", data, again)
	}
	return data
}

func js(t *testing.T, list []ops.Op) string {
	t.Helper()
	if list == nil {
		list = []ops.Op{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func edit(t *testing.T, f *task.File, fn func(f *task.File)) *task.File {
	t.Helper()
	c := f.Clone()
	fn(c)
	return c
}

func TestDeltaCases(t *testing.T) {
	base := doc(t, baseDoc)
	cases := []struct {
		name string
		fn   func(f *task.File)
		want string
	}{
		{"equal", func(f *task.File) {}, `[]`},
		{"move one card", func(f *task.File) {
			tk := f.Boards[0].Tasks[0]
			f.Boards[0].Tasks = f.Boards[0].Tasks[1:]
			f.Boards[1].Tasks = append(f.Boards[1].Tasks, tk)
		}, `[{"op":"task.move","id":"aaaa","board":"Done","after":null}]`},
		{"reorder within a board", func(f *task.File) {
			ts := f.Boards[0].Tasks
			f.Boards[0].Tasks = []*task.Task{ts[2], ts[0], ts[1]}
		}, `[{"op":"task.move","id":"cccc","board":"Backlog","after":null}]`},
		{"one field", func(f *task.File) { f.Boards[0].Tasks[1].Priority = task.PriorityHigh },
			`[{"op":"task.update","id":"bbbb","set":{"priority":"high"}}]`},
		{"append comment", func(f *task.File) {
			tk := f.Boards[0].Tasks[1]
			tk.Comments = append(tk.Comments, task.Comment{Author: "me", Date: task.Date{Year: 2026, Month: 2, Day: 2}, Text: "more"})
		}, `[{"op":"comment.add","task":"bbbb","comment":{"author":"me","date":"2026-02-02","text":"more"}}]`},
		{"edit comment", func(f *task.File) { f.Boards[0].Tasks[1].Comments[0].Text = "edited" },
			`[{"op":"comment.set","task":"bbbb","comments":[{"author":"ai","date":"2026-01-01","text":"edited"}]}]`},
		{"case-only board rename", func(f *task.File) { f.Boards[1].Name = "DONE" },
			`[{"op":"board.spelling","board":"Done","name":"DONE"}]`},
		{"board order", func(f *task.File) { f.Boards[0], f.Boards[1] = f.Boards[1], f.Boards[0] },
			`[{"op":"board.order","boards":["Done","Backlog"]}]`},
		{"title and preamble", func(f *task.File) { f.Title = "Project"; f.Preamble = "Hello\n\nworld" },
			`[{"op":"doc.title","title":"Project"},{"op":"doc.preamble","preamble":"Hello\n\nworld"}]`},
		{"clear due and set tags", func(f *task.File) {
			f.Boards[0].Tasks[0].Tags = []string{"a"}
		}, `[{"op":"task.update","id":"aaaa","set":{"tags":["a"]}}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roundTrip(t, base, edit(t, base, c.fn)); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}

	t.Run("board delete comes after its tasks", func(t *testing.T) {
		b := edit(t, base, func(f *task.File) { f.Boards = f.Boards[1:] })
		list := ops.Delta(base, b)
		roundTrip(t, base, b)
		last := list[len(list)-1]
		if last.Op != ops.BoardDelete || last.Board != "Backlog" {
			t.Errorf("last op = %+v", last)
		}
		n := 0
		for _, o := range list {
			if o.Op == ops.TaskDelete {
				n++
			}
		}
		if n != 3 {
			t.Errorf("task deletes = %d", n)
		}
	})

	t.Run("due cleared", func(t *testing.T) {
		withDue := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Due = &task.Date{Year: 2026, Month: 1, Day: 1} })
		got := roundTrip(t, withDue, base)
		if got != `[{"op":"task.update","id":"aaaa","set":{"due":null}}]` {
			t.Errorf("got %s", got)
		}
	})
}

func TestDeltaRandomEditScripts(t *testing.T) {
	for seed := uint32(1); seed <= 2000; seed++ {
		r := todogen.New(seed)
		a := todogen.File(r, 1)
		b := todogen.Mutate(r, a, todogen.EditOptions{Steps: 1 + r.Int(12)})
		roundTrip(t, a, b)
	}
}

func TestDeltaUnrelatedModels(t *testing.T) {
	for seed := uint32(1); seed <= 400; seed++ {
		r := todogen.New(seed * 7919)
		roundTrip(t, todogen.File(r, 1), todogen.File(r, 2))
	}
}

func TestDeltaKeepsSingleEditsSmall(t *testing.T) {
	for seed := uint32(1); seed <= 400; seed++ {
		r := todogen.New(seed + 100000)
		a := todogen.File(r, 2)
		b := todogen.Mutate(r, a, todogen.EditOptions{Steps: 1})
		list := ops.Delta(a, b)
		roundTrip(t, a, b)
		moved, most := 0, 1
		for _, o := range list {
			if o.Op == ops.TaskMove {
				moved++
			}
		}
		for _, bd := range a.Boards {
			most = max(most, len(bd.Tasks))
		}
		if moved > most {
			t.Fatalf("seed %d: %d moves for one edit", seed, moved)
		}
	}
}

func TestApplyRejects(t *testing.T) {
	base := doc(t, baseDoc)
	s := func(v string) *string { return &v }
	bad := map[string]ops.Op{
		"unknown task":           {Op: ops.TaskDelete, ID: "zzzz"},
		"unknown board":          {Op: ops.TaskMove, ID: "aaaa", Board: "Nope"},
		"duplicate id":           {Op: ops.TaskAdd, Board: "Done", Task: &task.Task{ID: "aaaa", Title: "x"}},
		"malformed id":           {Op: ops.TaskAdd, Board: "Done", Task: &task.Task{ID: "AAAA", Title: "x"}},
		"after on another board": {Op: ops.TaskMove, ID: "aaaa", Board: "Done", After: s("bbbb")},
		"after itself":           {Op: ops.TaskMove, ID: "aaaa", Board: "Backlog", After: s("aaaa")},
		"existing board":         {Op: ops.BoardAdd, Board: "DONE"},
		"non-empty board delete": {Op: ops.BoardDelete, Board: "Backlog"},
		"incomplete order":       {Op: ops.BoardOrder, Boards: []string{"Done"}},
		"duplicate order":        {Op: ops.BoardOrder, Boards: []string{"Done", "done"}},
		"spelling not case":      {Op: ops.BoardSpelling, Board: "Done", Name: "Finished"},
		"comment unknown task":   {Op: ops.CommentAdd, TaskID: "zzzz", Comment: task.Comment{Author: "a", Date: task.Date{Year: 2026, Month: 1, Day: 1}}},
	}
	for name, op := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := ops.Apply(base, []ops.Op{{Op: ops.DocTitle, Title: "x"}, op})
			var oe *ops.OpError
			if !errors.As(err, &oe) || oe.Index != 1 {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestApplyLeavesInputAlone(t *testing.T) {
	base := doc(t, baseDoc)
	before := string(markdown.Write(base))
	title := "changed"
	tags := []string{"a"}
	_, err := ops.Apply(base, []ops.Op{
		{Op: ops.TaskUpdate, ID: "aaaa", Set: ops.Fields{Title: &title, Tags: &tags}},
		{Op: ops.CommentAdd, TaskID: "bbbb", Comment: task.Comment{Author: "a", Date: task.Date{Year: 2026, Month: 1, Day: 1}, Text: "x"}},
		{Op: ops.BoardSpelling, Board: "Done", Name: "DONE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(markdown.Write(base)) != before {
		t.Error("Apply modified its input")
	}
}

func TestApplyMatchesBoardsCaseInsensitively(t *testing.T) {
	base := doc(t, baseDoc)
	f, err := ops.Apply(base, []ops.Op{{Op: ops.TaskMove, ID: "aaaa", Board: "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Boards[1].Name != "Done" || len(f.Boards[1].Tasks) != 1 || f.Boards[1].Tasks[0].ID != "aaaa" {
		t.Errorf("got %+v", f.Boards[1])
	}
}

func TestDecode(t *testing.T) {
	ok := `[{"op":"task.update","id":"aaaa","set":{"due":null,"tags":["x"]}},{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":"2026-01-01","description":"","comments":[]}}]`
	list, err := ops.Decode([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if !list[0].Set.HasDue || list[0].Set.Due != nil || !reflect.DeepEqual(*list[0].Set.Tags, []string{"x"}) {
		t.Errorf("set = %+v", list[0].Set)
	}

	bad := []string{
		`null`, `{}`, `[null]`, `[[]]`,
		`[{"op":"task.nuke","id":"aaaa"}]`,
		`[{"op":"task.delete"}]`,
		`[{"op":"task.delete","id":"aaaa","extra":1}]`,
		`[{"op":"task.delete","id":"AAAA"}]`,
		`[{"op":"task.delete","id":5}]`,
		`[{"op":"task.delete","id":null}]`,
		`[{"op":"task.update","id":"aaaa","set":{"comments":[]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"priority":"urgent"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"priority":"h"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"due":"2026-02-30"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"tags":"x"}}]`,
		`[{"op":"task.move","id":"aaaa","board":"B"}]`,
		`[{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t"}}]`,
		`[{"op":"comment.add","task":"aaaa","comment":{"author":"a","date":"2026-01-01","text":"x","mood":"happy"}}]`,
		`[{"op":"board.order","boards":["a",1]}]`,
		`[{"op":"doc.title","title":"x","__proto__":{"polluted":true}}]`,
	}
	for _, raw := range bad {
		if _, err := ops.Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		} else if !strings.HasPrefix(err.Error(), "op ") {
			t.Errorf("%s: not an OpError: %v", raw, err)
		}
	}
}

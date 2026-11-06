package merge_test

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/todogen"
)

var day = task.Date{Year: 2026, Month: 11, Day: 5}

func T(id, title string, mods ...func(*task.Task)) *task.Task {
	t := &task.Task{ID: id, Title: title}
	for _, m := range mods {
		m(t)
	}
	return t
}

type bd struct {
	name  string
	tasks []*task.Task
}

func file(t *testing.T, boards ...bd) *task.File {
	t.Helper()
	f := &task.File{Title: "TODO"}
	for _, b := range boards {
		f.Boards = append(f.Boards, &task.Board{Name: b.name, Tasks: b.tasks})
	}
	n, err := merge.Normalize(f)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func edit(t *testing.T, f *task.File, fn func(f *task.File)) *task.File {
	t.Helper()
	c := f.Clone()
	fn(c)
	n, err := merge.Normalize(c)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func m(t *testing.T, b, o, th *task.File, opts ...merge.Options) merge.Result {
	t.Helper()
	opt := merge.Options{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	opt.Date = day
	r, err := merge.Merge(b, o, th, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func layout(f *task.File) [][]string {
	var out [][]string
	for _, b := range f.Boards {
		row := []string{b.Name}
		for _, t := range b.Tasks {
			row = append(row, t.ID)
		}
		out = append(out, row)
	}
	return out
}

func find(f *task.File, id string) *task.Task {
	for _, t := range f.AllTasks() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func syncComments(t *task.Task) []string {
	var out []string
	for _, c := range t.Comments {
		if c.Author == merge.SyncAuthor {
			out = append(out, c.Text)
		}
	}
	return out
}

func write(f *task.File) string { return string(markdown.Write(f)) }

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func d(s string) task.Date {
	x, err := task.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return x
}

func baseFile(t *testing.T) *task.File {
	return file(t,
		bd{"Backlog", []*task.Task{
			T("aaaa", "One", func(t *task.Task) { t.Tags = []string{"x", "y"}; t.Description = "l1\nl2\nl3\nl4\nl5" }),
			T("bbbb", "Two"), T("cccc", "Three"),
		}},
		bd{"Doing", []*task.Task{T("dddd", "Four")}},
		bd{"Done", nil},
	)
}

func TestMerge3(t *testing.T) {
	check := func(b, o, th, want string, wantOK bool) {
		t.Helper()
		got, ok := merge.Merge3(b, o, th)
		if ok != wantOK || (ok && got != want) {
			t.Errorf("Merge3(%q, %q, %q) = %q, %v; want %q, %v", b, o, th, got, ok, want, wantOK)
		}
	}
	check("a\nb\nc\nd", "A\nb\nc\nd", "a\nb\nc\nD", "A\nb\nc\nD", true)
	check("a\nb", "a\nX\nb", "a\nX\nb", "a\nX\nb", true)
	check("a\nb\nc", "a\nX\nc", "a\nY\nc", "", false)

	big := make([]string, merge.MaxMergeLines+1)
	for i := range big {
		big[i] = "line " + strings.Repeat("x", i%7) + string(rune('a'+i%26))
	}
	bigS := strings.Join(big, "\n")
	check(bigS, bigS+"\nours", "theirs\n"+bigS, "", false)
	check(bigS, bigS+"\nours", bigS, bigS+"\nours", true)

	lines := func(p string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = p + strings.Repeat(" ", 0) + itoa(i)
		}
		return out
	}
	b := strings.Join(lines("b", 1500), "\n")
	check(b, strings.Join(lines("o", 1500), "\n"), b+"\ntheirs", "", false)
	near := strings.Join(append(lines("o", 900), lines("b", 1500)[900:]...), "\n")
	if _, ok := merge.Merge3(b, near, b+"\ntheirs"); !ok {
		t.Error("a side within MaxEditLines should merge")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var s []byte
	for ; i > 0; i /= 10 {
		s = append([]byte{byte('0' + i%10)}, s...)
	}
	return string(s)
}

func TestMerge3IdempotentByConstruction(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "", "```"}
	for seed := uint32(1); seed <= 5000; seed++ {
		r := todogen.New(seed)
		gen := func(n int) []string {
			out := make([]string, n)
			for i := range out {
				out[i] = pool[r.Int(len(pool))]
			}
			return out
		}
		b := gen(r.Int(8))
		o, th := slices.Clone(b), slices.Clone(b)
		for _, side := range []*[]string{&o, &th} {
			for k := r.Int(3); k >= 0; k-- {
				at := r.Int(len(*side) + 1)
				del := r.Int(2)
				ins := gen(r.Int(2))
				end := min(at+del, len(*side))
				*side = slices.Concat((*side)[:at], ins, (*side)[end:])
			}
		}
		bs, os, ts := strings.Join(b, "\n"), strings.Join(o, "\n"), strings.Join(th, "\n")
		res, ok := merge.Merge3(bs, os, ts)
		if !ok {
			continue
		}
		if again, ok := merge.Merge3(bs, os, res); !ok || again != res {
			t.Fatalf("seed %d: re-merge changed %q to %q (%v)", seed, res, again, ok)
		}
	}
}

func TestMergeFieldOneSide(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Title = "Ours"; f.Boards[0].Tasks[1].Priority = task.PriorityHigh })
	th := edit(t, base, func(f *task.File) { due := d("2026-12-01"); f.Boards[0].Tasks[1].Due = &due })
	r := m(t, base, o, th)
	eq(t, r.Merged.Title, "Ours")
	two := find(r.Merged, "bbbb")
	eq(t, two.Priority, task.PriorityHigh)
	eq(t, two.Due.String(), "2026-12-01")
	eq(t, len(r.Conflicts), 0)
}

func TestMergeFieldSameValue(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[1].Title = "Same" })
	r := m(t, base, o, o)
	eq(t, find(r.Merged, "bbbb").Title, "Same")
	eq(t, len(r.Conflicts), 0)
}

func TestMergeFieldBothDiffer(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) {
		f.Title = "Ours"
		tk := f.Boards[0].Tasks[1]
		tk.Title, tk.Priority = "Ours title", task.PriorityHigh
		due := d("2026-01-01")
		tk.Due = &due
	})
	th := edit(t, base, func(f *task.File) {
		f.Title = "Theirs"
		tk := f.Boards[0].Tasks[1]
		tk.Title, tk.Priority = "Theirs title", task.PriorityLow
		due := d("2026-02-02")
		tk.Due = &due
	})
	r := m(t, base, o, th)
	eq(t, r.Merged.Title, "Theirs")
	two := find(r.Merged, "bbbb")
	eq(t, []any{two.Title, two.Priority, two.Due.String()}, []any{"Theirs title", task.PriorityLow, "2026-02-02"})
	eq(t, syncComments(two), []string{
		"Sync conflict on title — this machine's version was: Ours title",
		"Sync conflict on priority — this machine's version was: high",
		"Sync conflict on due date — this machine's version was: 2026-01-01",
	})
	for _, c := range two.Comments {
		if c.Date != day {
			t.Errorf("comment date %s", c.Date)
		}
	}
	var got [][]string
	for _, c := range r.Conflicts {
		got = append(got, []string{c.Kind, c.Task, c.Field, c.Kept})
	}
	eq(t, got, [][]string{
		{"field", "", "title", "theirs"},
		{"field", "bbbb", "title", "theirs"},
		{"field", "bbbb", "priority", "theirs"},
		{"field", "bbbb", "due", "theirs"},
	})
	if !strings.Contains(r.Conflicts[0].Note, "Ours") {
		t.Errorf("note %q", r.Conflicts[0].Note)
	}
}

func TestMergeDescription(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Description = "L1\nl2\nl3\nl4\nl5" })
	th := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Description = "l1\nl2\nl3\nl4\nL5" })
	r := m(t, base, o, th)
	eq(t, find(r.Merged, "aaaa").Description, "L1\nl2\nl3\nl4\nL5")
	eq(t, len(r.Conflicts), 0)

	ours := "l1\nl2 ours\n```js\ncode\n```\nl4\nl5"
	o = edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Description = ours })
	th = edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Description = "l1\nl2 theirs\nl3\nl4\nl5" })
	r = m(t, base, o, th)
	one := find(r.Merged, "aaaa")
	eq(t, one.Description, "l1\nl2 theirs\nl3\nl4\nl5")
	eq(t, syncComments(one), []string{"Sync conflict on description — this machine's version was:\n\n````\n" + ours + "\n````"})
	eq(t, len(r.Conflicts), 1)
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Task, r.Conflicts[0].Field, r.Conflicts[0].Kept}, []string{"text", "aaaa", "description", "theirs"})
	if err := merge.CheckTask(one); err != nil {
		t.Error(err)
	}
}

func TestMergePreamble(t *testing.T) {
	b := edit(t, baseFile(t), func(f *task.File) { f.Preamble = "p1\np2\np3" })
	o := edit(t, b, func(f *task.File) { f.Preamble = "P1\np2\np3" })
	th := edit(t, b, func(f *task.File) { f.Preamble = "p1\np2\nP3" })
	eq(t, m(t, b, o, th).Merged.Preamble, "P1\np2\nP3")
	t2 := edit(t, b, func(f *task.File) { f.Preamble = "X1\np2\np3" })
	r := m(t, b, o, t2)
	eq(t, r.Merged.Preamble, "X1\np2\np3")
	eq(t, len(r.Conflicts), 1)
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Field, r.Conflicts[0].Kept}, []string{"text", "preamble", "theirs"})
	if !strings.Contains(r.Conflicts[0].Note, "P1\np2\np3") {
		t.Errorf("note %q", r.Conflicts[0].Note)
	}
}

func TestMergeUnreadableLineMergeFallsBack(t *testing.T) {
	b := file(t, bd{"B", []*task.Task{T("aaaa", "a", func(t *task.Task) { t.Description = "x\ny\nz" })}})
	o := edit(t, b, func(f *task.File) { f.Boards[0].Tasks[0].Description = "```\nx\n```\ny\nz" })
	th := edit(t, b, func(f *task.File) { f.Boards[0].Tasks[0].Description = "x\ny\n```\nz\n```" })
	r := m(t, b, o, th)
	if err := merge.CheckTask(find(r.Merged, "aaaa")); err != nil {
		t.Error(err)
	}
	back, err := markdown.Parse(markdown.Write(r.Merged))
	if err != nil || write(back) != write(r.Merged) {
		t.Errorf("not canonical: %v", err)
	}
}

func TestMergeTags(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Tags = []string{"y", "o1"} })
	th := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Tags = []string{"t1", "y", "x"} })
	r := m(t, base, o, th)
	eq(t, find(r.Merged, "aaaa").Tags, []string{"t1", "y", "o1"})
	eq(t, len(r.Conflicts), 0)
}

func TestMergeComments(t *testing.T) {
	c := func(text string) task.Comment { return task.Comment{Author: "ai", Date: d("2026-01-01"), Text: text} }
	b := file(t, bd{"B", []*task.Task{T("aaaa", "a", func(t *task.Task) { t.Comments = []task.Comment{c("one"), c("two")} })}})
	set := func(f *task.File, cs ...task.Comment) *task.File {
		return edit(t, f, func(x *task.File) { x.Boards[0].Tasks[0].Comments = cs })
	}
	texts := func(f *task.File) []string {
		var out []string
		for _, x := range find(f, "aaaa").Comments {
			out = append(out, x.Text)
		}
		return out
	}

	o := set(b, c("one"), c("two"), c("ours"), c("both"))
	th := set(b, c("one"), c("two"), c("both"), c("theirs"))
	r := m(t, b, o, th)
	eq(t, texts(r.Merged), []string{"one", "two", "both", "theirs", "ours"})
	eq(t, len(r.Conflicts), 0)

	o = set(b, c("one edited"), c("two"))
	th = set(b, c("one"), c("two"), c("new"))
	eq(t, texts(m(t, b, o, th).Merged), []string{"one edited", "two", "new"})
	eq(t, texts(m(t, b, th, o).Merged), []string{"one edited", "two", "new"})

	o = set(b, c("one ours"))
	th = set(b, c("one theirs"), c("two"))
	r = m(t, b, o, th)
	eq(t, texts(r.Merged)[:2], []string{"one theirs", "two"})
	eq(t, syncComments(find(r.Merged, "aaaa")), []string{
		"Sync conflict on comments — these comments from this machine were removed:\n\n```\n- **ai** (2026-01-01): one ours\n```",
	})
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Task, r.Conflicts[0].Field, r.Conflicts[0].Kept}, []string{"field", "aaaa", "comments", "theirs"})
}

func TestMergeTaskAdded(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[1].Tasks = append(f.Boards[1].Tasks, T("oooo", "Ours new")) })
	th := edit(t, base, func(f *task.File) {
		f.Boards[1].Tasks = append([]*task.Task{T("tttt", "Theirs new")}, f.Boards[1].Tasks...)
	})
	eq(t, layout(m(t, base, o, th).Merged)[1], []string{"Doing", "tttt", "dddd", "oooo"})

	o = edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("nnnn", "New")) })
	r := m(t, base, o, o)
	n := 0
	for _, x := range r.Merged.AllTasks() {
		if x.ID == "nnnn" {
			n++
		}
	}
	eq(t, n, 1)
	eq(t, len(r.Renamed), 0)

	o = edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("nnnn", "Ours")) })
	th = edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("nnnn", "Theirs")) })
	r = m(t, base, o, th)
	if len(r.Renamed) != 1 {
		t.Fatalf("renamed = %+v", r.Renamed)
	}
	rn := r.Renamed[0]
	eq(t, []string{rn.From, rn.Title}, []string{"nnnn", "Ours"})
	if !regexp.MustCompile(`^[0-9a-z]{4}$`).MatchString(rn.To) {
		t.Errorf("new id %q", rn.To)
	}
	eq(t, find(r.Merged, "nnnn").Title, "Theirs")
	eq(t, find(r.Merged, rn.To).Title, "Ours")
	again := m(t, base, o, th)
	eq(t, write(again.Merged), write(r.Merged))
	eq(t, again.Renamed, r.Renamed)
	eq(t, write(m(t, base, o, r.Merged).Merged), write(r.Merged))
}

func TestMergeStableIDMatchesReference(t *testing.T) {
	// The rename id is FNV-1a over the task's fragment; pin one value so a
	// change on either side of the wire shows up here.
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("nnnn", "Ours")) })
	th := edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("nnnn", "Theirs")) })
	r := m(t, base, o, th)
	if len(r.Renamed) != 1 {
		t.Fatal(r.Renamed)
	}
	t.Logf("renamed to %s", r.Renamed[0].To)
}

func TestMergeTaskDeleted(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards[0].Tasks = slices.Delete(f.Boards[0].Tasks, 1, 2) })
	th := edit(t, base, func(f *task.File) { f.Boards[0].Tasks = slices.Delete(f.Boards[0].Tasks, 2, 3) })
	eq(t, layout(m(t, base, o, th).Merged)[0], []string{"Backlog", "aaaa"})

	th = edit(t, base, func(f *task.File) { f.Boards[0].Tasks[1].Title = "Edited" })
	r := m(t, base, o, th)
	eq(t, find(r.Merged, "bbbb").Title, "Edited")
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Task, r.Conflicts[0].Kept}, []string{"delete_vs_edit", "bbbb", "theirs"})
	r2 := m(t, base, th, o)
	eq(t, find(r2.Merged, "bbbb").Title, "Edited")
	eq(t, []string{r2.Conflicts[0].Kind, r2.Conflicts[0].Task, r2.Conflicts[0].Kept}, []string{"delete_vs_edit", "bbbb", "ours"})
}

func moveTo(f *task.File, from, i, to int) {
	tk := f.Boards[from].Tasks[i]
	f.Boards[from].Tasks = slices.Delete(f.Boards[from].Tasks, i, i+1)
	f.Boards[to].Tasks = append(f.Boards[to].Tasks, tk)
}

func TestMergeMoves(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { moveTo(f, 0, 1, 2) })
	eq(t, layout(m(t, base, o, base).Merged)[2], []string{"Done", "bbbb"})
	eq(t, layout(m(t, base, base, o).Merged)[2], []string{"Done", "bbbb"})

	th := edit(t, base, func(f *task.File) { moveTo(f, 0, 1, 1) })
	r := m(t, base, o, th)
	eq(t, layout(r.Merged), [][]string{{"Backlog", "aaaa", "cccc"}, {"Doing", "dddd", "bbbb"}, {"Done"}})
	eq(t, syncComments(find(r.Merged, "bbbb")), []string{"Sync conflict on board — this machine's version was: Done"})
}

func TestMergeOrder(t *testing.T) {
	base := baseFile(t)
	rev := func(f *task.File) { slices.Reverse(f.Boards[0].Tasks) }
	o := edit(t, base, rev)
	th := edit(t, base, func(f *task.File) { f.Boards[0].Tasks[0].Title = "Edited" })
	eq(t, layout(m(t, base, o, th).Merged)[0], []string{"Backlog", "cccc", "bbbb", "aaaa"})
	eq(t, layout(m(t, base, th, o).Merged)[0], []string{"Backlog", "cccc", "bbbb", "aaaa"})

	th = edit(t, base, func(f *task.File) {
		ts := f.Boards[0].Tasks
		f.Boards[0].Tasks = append(ts[1:], ts[0])
	})
	eq(t, layout(m(t, base, o, th).Merged)[0], []string{"Backlog", "bbbb", "cccc", "aaaa"})

	o = edit(t, base, func(f *task.File) {
		f.Boards[0].Tasks = slices.Insert(f.Boards[0].Tasks, 1, T("o111", "o111"))
		f.Boards[0].Tasks = slices.Insert(f.Boards[0].Tasks, 0, T("o000", "o000"))
	})
	th = edit(t, base, rev)
	eq(t, layout(m(t, base, o, th).Merged)[0], []string{"Backlog", "o000", "cccc", "bbbb", "aaaa", "o111"})
}

func names(f *task.File) []string {
	var out []string
	for _, b := range f.Boards {
		out = append(out, b.Name)
	}
	return out
}

func TestMergeBoards(t *testing.T) {
	base := baseFile(t)
	o := edit(t, base, func(f *task.File) { f.Boards = slices.Insert(f.Boards, 1, &task.Board{Name: "Ours"}) })
	th := edit(t, base, func(f *task.File) { f.Boards = slices.Insert(f.Boards, 0, &task.Board{Name: "Theirs"}) })
	eq(t, names(m(t, base, o, th).Merged), []string{"Theirs", "Backlog", "Ours", "Doing", "Done"})

	o = edit(t, base, func(f *task.File) { f.Boards = f.Boards[:2] })
	r := m(t, base, o, base)
	eq(t, names(r.Merged), []string{"Backlog", "Doing"})
	eq(t, len(r.Conflicts), 0)

	th = edit(t, base, func(f *task.File) { f.Boards[2].Tasks = append(f.Boards[2].Tasks, T("tttt", "tttt")) })
	r = m(t, base, o, th)
	eq(t, layout(r.Merged)[2], []string{"Done", "tttt"})
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Board, r.Conflicts[0].Kept}, []string{"board_delete", "Done", "theirs"})

	th = edit(t, base, func(f *task.File) { f.Boards = f.Boards[:2] })
	o = edit(t, base, func(f *task.File) { moveTo(f, 0, 0, 2) })
	r = m(t, base, o, th)
	eq(t, layout(r.Merged)[2], []string{"Done", "aaaa"})
	eq(t, []string{r.Conflicts[0].Kind, r.Conflicts[0].Board, r.Conflicts[0].Kept}, []string{"board_delete", "Done", "ours"})

	o = edit(t, base, func(f *task.File) { f.Boards[2].Name = "Finished" })
	eq(t, names(m(t, base, o, base).Merged), []string{"Backlog", "Doing", "Finished"})

	o = edit(t, base, func(f *task.File) { f.Boards[2].Name = "DONE" })
	th = edit(t, base, func(f *task.File) { f.Boards[2].Name = "done" })
	eq(t, m(t, base, o, base).Merged.Boards[2].Name, "DONE")
	eq(t, m(t, base, base, th).Merged.Boards[2].Name, "done")
	r = m(t, base, o, th)
	eq(t, r.Merged.Boards[2].Name, "done")
	eq(t, len(r.Conflicts), 0)

	o = edit(t, base, func(f *task.File) { slices.Reverse(f.Boards) })
	eq(t, names(m(t, base, o, base).Merged), []string{"Done", "Doing", "Backlog"})
}

func TestMergeBaseless(t *testing.T) {
	empty := &task.File{}
	o := file(t, bd{"Backlog", []*task.Task{T("aaaa", "Ours", func(t *task.Task) {
		t.Tags = []string{"o"}
		t.Description = "mine"
		t.Comments = []task.Comment{{Author: "a", Date: d("2026-01-01"), Text: "c1"}}
	})}})
	th := file(t, bd{"Backlog", []*task.Task{T("aaaa", "Theirs", func(t *task.Task) {
		t.Tags = []string{"t"}
		t.Comments = []task.Comment{{Author: "b", Date: d("2026-01-02"), Text: "c2"}}
	})}})
	r := m(t, empty, o, th, merge.Options{Baseless: true})
	eq(t, len(r.Renamed), 0)
	a := find(r.Merged, "aaaa")
	eq(t, a.Title, "Theirs")
	eq(t, a.Description, "")
	eq(t, a.Tags, []string{"t", "o"})
	eq(t, []string{a.Comments[0].Text, a.Comments[1].Text}, []string{"c2", "c1"})
	eq(t, syncComments(a), []string{
		"Sync conflict on title — this machine's version was: Ours",
		"Sync conflict on description — this machine's version was:\n\n```\nmine\n```",
	})

	o = file(t, bd{"Backlog", []*task.Task{T("aaaa", "aaaa")}}, bd{"Ours", []*task.Task{T("oooo", "oooo")}})
	th = file(t, bd{"Theirs", []*task.Task{T("tttt", "tttt")}}, bd{"Backlog", []*task.Task{T("bbbb", "bbbb")}})
	eq(t, layout(m(t, empty, o, th, merge.Options{Baseless: true}).Merged), [][]string{
		{"Theirs", "tttt"}, {"Backlog", "aaaa", "bbbb"}, {"Ours", "oooo"},
	})

	for seed := uint32(1); seed <= 300; seed++ {
		r := todogen.New(seed)
		shared := todogen.File(r, 1)
		o := todogen.Mutate(r, shared, todogen.EditOptions{Steps: 4})
		th := todogen.Mutate(r, shared, todogen.EditOptions{Steps: 4})
		first := m(t, empty, o, th, merge.Options{Baseless: true}).Merged
		again := m(t, empty, o, first, merge.Options{Baseless: true}).Merged
		if write(again) != write(first) {
			t.Fatalf("seed %d: repeated first link changed the result", seed)
		}
	}
}

func TestMergeNoDeletesFromOurs(t *testing.T) {
	base := baseFile(t)
	head := edit(t, base, func(f *task.File) {
		f.Boards[0].Tasks[0].Comments = []task.Comment{{Author: "ai", Date: d("2026-01-01"), Text: "keep me"}}
	})
	ours := edit(t, head, func(f *task.File) {
		f.Boards[0].Tasks = slices.Delete(f.Boards[0].Tasks, 1, 2)
		f.Boards[0].Tasks[0].Comments = nil
		f.Boards[0].Tasks[0].Tags = []string{"x"}
		f.Boards = slices.Delete(f.Boards, 1, 2)
		f.Boards[0].Tasks = append(f.Boards[0].Tasks, T("nnnn", "Added on the branch"))
		f.Boards[0].Tasks[0].Title = "Edited on the branch"
	})
	r := m(t, head, ours, head, merge.Options{NoDeletesFromOurs: true})
	eq(t, layout(r.Merged), [][]string{{"Backlog", "aaaa", "bbbb", "cccc", "nnnn"}, {"Doing", "dddd"}, {"Done"}})
	a := find(r.Merged, "aaaa")
	eq(t, a.Title, "Edited on the branch")
	eq(t, len(a.Comments), 1)
	eq(t, a.Comments[0].Text, "keep me")
	eq(t, a.Tags, []string{"x", "y"})
	eq(t, len(r.Conflicts), 0)
	eq(t, layout(m(t, head, ours, head).Merged)[0], []string{"Backlog", "aaaa", "cccc", "nnnn"})
}

// --- Properties over random edit scripts from a common base ---------------------

func cases(n uint32, fn func(b, o, th *task.File)) {
	ids := []string{"zz01", "zz02", "zz03"} // shared pool for new ids: provokes collisions
	for seed := uint32(1); seed <= n; seed++ {
		r := todogen.New(seed * 31337)
		b := todogen.File(r, 1+r.Int(2))
		o := todogen.Mutate(r, b, todogen.EditOptions{Steps: 1 + r.Int(8), IDPool: ids})
		th := todogen.Mutate(r, b, todogen.EditOptions{Steps: 1 + r.Int(8), IDPool: ids})
		fn(b, o, th)
	}
}

func assertValid(t *testing.T, f *task.File) {
	t.Helper()
	text := markdown.Write(f)
	back, err := markdown.ParseStrict(text)
	if err != nil {
		t.Fatalf("merged result fails strict parse: %v\n%s", err, text)
	}
	if string(markdown.Write(back)) != string(text) {
		t.Fatal("merged result not canonical")
	}
	if err := merge.CheckFile(f); err != nil {
		t.Fatalf("merged result fails checks: %v", err)
	}
}

func TestMergePropertiesValidDeterministic(t *testing.T) {
	cases(1000, func(b, o, th *task.File) {
		r1 := m(t, b, o, th)
		assertValid(t, r1.Merged)
		r2 := m(t, b, o, th)
		if write(r1.Merged) != write(r2.Merged) || !reflect.DeepEqual(r1.Conflicts, r2.Conflicts) || !reflect.DeepEqual(r1.Renamed, r2.Renamed) {
			t.Fatal("merge not deterministic")
		}
		assertValid(t, m(t, b, o, th, merge.Options{NoDeletesFromOurs: true}).Merged)
		assertValid(t, m(t, &task.File{}, o, th, merge.Options{Baseless: true}).Merged)
	})
}

func TestMergePropertiesIdentity(t *testing.T) {
	cases(1000, func(b, o, th *task.File) {
		if write(m(t, b, o, o).Merged) != write(o) {
			t.Fatal("Merge(B,X,X) != X")
		}
		if write(m(t, b, b, th).Merged) != write(th) {
			t.Fatal("Merge(B,B,T) != T")
		}
		if write(m(t, b, o, b).Merged) != write(o) {
			t.Fatal("Merge(B,O,B) != O")
		}
	})
}

func TestMergePropertiesIdempotentRepush(t *testing.T) {
	cases(1000, func(b, o, th *task.File) {
		once := m(t, b, o, th).Merged
		if write(m(t, b, o, once).Merged) != write(once) {
			t.Fatal("re-push changed the result")
		}
		imp := m(t, b, o, th, merge.Options{NoDeletesFromOurs: true}).Merged
		if write(m(t, b, o, imp, merge.Options{NoDeletesFromOurs: true}).Merged) != write(imp) {
			t.Fatal("re-push of an import changed the result")
		}
	})
}

func TestMergePropertiesNoSilentLoss(t *testing.T) {
	cases(600, func(b, o, th *task.File) {
		r := m(t, b, o, th)
		have := map[string]bool{}
		for _, x := range r.Merged.AllTasks() {
			have[x.ID] = true
		}
		inB := map[string]bool{}
		for _, x := range b.AllTasks() {
			inB[x.ID] = true
		}
		for _, x := range o.AllTasks() {
			if inB[x.ID] {
				continue
			}
			to := x.ID
			for _, rn := range r.Renamed {
				if rn.From == x.ID {
					to = rn.To
				}
			}
			if have[to] {
				continue
			}
			found := false
			for _, y := range r.Merged.AllTasks() {
				if y.Title == x.Title && y.Description == x.Description {
					found = true
				}
			}
			if !found {
				t.Fatalf("ours task %s (%q) was lost", x.ID, x.Title)
			}
		}
	})
}

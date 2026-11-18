package merge_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/task"
)

func assignee(h string) func(*task.Task) {
	return func(t *task.Task) { t.Assignee = h }
}

func setAssignee(id, h string) func(f *task.File) {
	return func(f *task.File) {
		for _, x := range f.AllTasks() {
			if x.ID == id {
				x.Assignee = h
			}
		}
	}
}

// TestMergeAssignees also writes testdata/assignees/merge.json, which
// todomd.online's merge.ts checks against.
func TestMergeAssignees(t *testing.T) {
	base := file(t,
		bd{"Backlog", []*task.Task{
			T("aaaa", "One", assignee("anna")),
			T("bbbb", "Two"),
			T("cccc", "Three", assignee("bob"), sticker("fire")),
		}},
		bd{"Done", nil},
	)
	var golden []mergeCase
	run := func(name string, b, o, th *task.File, opts merge.Options, want map[string]string, conflicts int, note string) {
		t.Run(name, func(t *testing.T) {
			r := m(t, b, o, th, opts)
			assertValid(t, r.Merged)
			for id, h := range want {
				x := find(r.Merged, id)
				if x == nil {
					t.Fatalf("%s missing", id)
				}
				eq(t, x.Assignee, h)
			}
			eq(t, len(r.Conflicts), conflicts)
			if note != "" {
				found := false
				for _, x := range r.Merged.AllTasks() {
					for _, c := range x.Comments {
						found = found || (c.Author == merge.SyncAuthor && c.Text == note)
					}
				}
				if !found {
					t.Errorf("no conflict comment %q", note)
				}
			}
			if write(m(t, b, o, r.Merged, opts).Merged) != write(r.Merged) {
				t.Error("re-push changed the result")
			}
			c := mergeCase{Name: name, Base: write(b), Ours: write(o), Theirs: write(th), Merged: write(r.Merged),
				Conflicts: r.Conflicts, Renamed: r.Renamed}
			if c.Conflicts == nil {
				c.Conflicts = []merge.Conflict{}
			}
			if c.Renamed == nil {
				c.Renamed = []merge.Rename{}
			}
			if opts.Baseless {
				c.Options = map[string]bool{"baseless": true}
			}
			if opts.NoDeletesFromOurs {
				c.Options = map[string]bool{"no_deletes_from_ours": true}
			}
			golden = append(golden, c)
		})
	}
	ed := func(fn ...func(f *task.File)) *task.File {
		return edit(t, base, func(f *task.File) {
			for _, g := range fn {
				g(f)
			}
		})
	}
	none := merge.Options{}
	conflictNote := "Sync conflict on assignee — this machine's version was: @carl"

	run("ours assigns, theirs unchanged", base, ed(setAssignee("bbbb", "carl")), base, none,
		map[string]string{"bbbb": "carl"}, 0, "")
	run("theirs assigns, ours unchanged", base, base, ed(setAssignee("bbbb", "dora")), none,
		map[string]string{"bbbb": "dora"}, 0, "")
	run("ours unassigns, theirs unchanged", base, ed(setAssignee("aaaa", "")), base, none,
		map[string]string{"aaaa": ""}, 0, "")
	run("theirs unassigns, ours unchanged", base, base, ed(setAssignee("aaaa", "")), none,
		map[string]string{"aaaa": ""}, 0, "")
	run("both assign different people: theirs, with a conflict", base, ed(setAssignee("bbbb", "carl")), ed(setAssignee("bbbb", "dora")), none,
		map[string]string{"bbbb": "dora"}, 1, conflictNote)
	run("both reassign differently: theirs, with a conflict", base, ed(setAssignee("aaaa", "carl")), ed(setAssignee("aaaa", "dora")), none,
		map[string]string{"aaaa": "dora"}, 1, conflictNote)
	run("both assign the same person", base, ed(setAssignee("bbbb", "carl")), ed(setAssignee("bbbb", "carl")), none,
		map[string]string{"bbbb": "carl"}, 0, "")
	run("both unassign", base, ed(setAssignee("aaaa", "")), ed(setAssignee("aaaa", "")), none,
		map[string]string{"aaaa": ""}, 0, "")
	run("ours reassigns, theirs unassigns: the assign wins", base, ed(setAssignee("cccc", "carl")), ed(setAssignee("cccc", "")), none,
		map[string]string{"cccc": "carl"}, 0, "")
	run("ours unassigns, theirs reassigns: the assign wins", base, ed(setAssignee("cccc", "")), ed(setAssignee("cccc", "dora")), none,
		map[string]string{"cccc": "dora"}, 0, "")
	run("different cards", base, ed(setAssignee("aaaa", "carl")), ed(setAssignee("cccc", "dora")), none,
		map[string]string{"aaaa": "carl", "cccc": "dora"}, 0, "")
	run("assignee and sticker change on one card", base, ed(setAssignee("cccc", "carl")), ed(setSticker("cccc", "star")), none,
		map[string]string{"cccc": "carl"}, 0, "")
	run("no deletes from ours: ours' unassign doesn't apply", base, ed(setAssignee("aaaa", "")), base,
		merge.Options{NoDeletesFromOurs: true}, map[string]string{"aaaa": "anna"}, 0, "")
	run("no deletes from ours: theirs' unassign still applies", base, base, ed(setAssignee("aaaa", "")),
		merge.Options{NoDeletesFromOurs: true}, map[string]string{"aaaa": ""}, 0, "")
	run("no deletes from ours: ours' reassign applies", base, ed(setAssignee("aaaa", "carl")), base,
		merge.Options{NoDeletesFromOurs: true}, map[string]string{"aaaa": "carl"}, 0, "")
	run("baseless: theirs when set", &task.File{}, ed(setAssignee("aaaa", "carl")), base,
		merge.Options{Baseless: true}, map[string]string{"aaaa": "anna"}, 1, conflictNote)
	run("baseless: ours when theirs has none", &task.File{}, ed(setAssignee("bbbb", "carl")), base,
		merge.Options{Baseless: true}, map[string]string{"bbbb": "carl"}, 0, "")
	run("baseless: theirs' assignee over none", &task.File{}, ed(setAssignee("aaaa", "")), base,
		merge.Options{Baseless: true}, map[string]string{"aaaa": "anna"}, 0, "")
	run("an assignee change keeps a card the other side deleted", base, ed(setAssignee("bbbb", "carl")),
		ed(func(f *task.File) { f.Boards[0].Tasks = []*task.Task{f.Boards[0].Tasks[0], f.Boards[0].Tasks[2]} }), none,
		map[string]string{"bbbb": "carl"}, 1, "")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(golden); err != nil {
		t.Fatal(err)
	}
	path := "../../testdata/assignees/merge.json"
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("merge.json differs; run with -update and review:\n%s", buf.Bytes())
	}
}

func TestCheckTaskAssignee(t *testing.T) {
	for _, h := range []string{"", "anna", "a", "x-1", strings.Repeat("a", 39)} {
		if err := merge.CheckTask(T("aaaa", "x", assignee(h))); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	for _, h := range []string{"Anna", "@anna", "a_b", "-a", "a-", " anna", strings.Repeat("a", 40), "anna,bob"} {
		err := merge.CheckTask(T("aaaa", "x", assignee(h)))
		var ve *merge.ValidationError
		if err == nil || !asValidation(err, &ve) || ve.Field != "assignee" {
			t.Errorf("%q: %v", h, err)
		}
	}
}

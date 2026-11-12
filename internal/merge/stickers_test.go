package merge_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/task"
)

func sticker(name string) func(*task.Task) {
	return func(t *task.Task) { t.Sticker = name }
}

func setSticker(id, name string) func(f *task.File) {
	return func(f *task.File) {
		for _, x := range f.AllTasks() {
			if x.ID == id {
				x.Sticker = name
			}
		}
	}
}

// TestMergeStickers also writes testdata/stickers/merge.json, which
// todomd.online's merge.ts checks against.
func TestMergeStickers(t *testing.T) {
	base := file(t,
		bd{"Backlog", []*task.Task{
			T("aaaa", "One", sticker("fire")),
			T("bbbb", "Two"),
			T("cccc", "Three", sticker("red")),
		}},
		bd{"Done", nil},
	)
	var golden []mergeCase
	run := func(name string, b, o, th *task.File, opts merge.Options, want map[string]string, conflicts int) {
		t.Run(name, func(t *testing.T) {
			r := m(t, b, o, th, opts)
			assertValid(t, r.Merged)
			for id, s := range want {
				x := find(r.Merged, id)
				if x == nil {
					t.Fatalf("%s missing", id)
				}
				eq(t, x.Sticker, s)
			}
			eq(t, len(r.Conflicts), conflicts)
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

	run("ours sets, theirs unchanged", base, ed(setSticker("bbbb", "star")), base, none,
		map[string]string{"bbbb": "star"}, 0)
	run("theirs sets, ours unchanged", base, base, ed(setSticker("bbbb", "zap")), none,
		map[string]string{"bbbb": "zap"}, 0)
	run("ours removes, theirs unchanged", base, ed(setSticker("aaaa", "")), base, none,
		map[string]string{"aaaa": ""}, 0)
	run("both change: theirs wins, no conflict", base, ed(setSticker("aaaa", "star")), ed(setSticker("aaaa", "blue")), none,
		map[string]string{"aaaa": "blue"}, 0)
	run("ours changes, theirs removes: theirs wins", base, ed(setSticker("cccc", "green")), ed(setSticker("cccc", "")), none,
		map[string]string{"cccc": ""}, 0)
	run("ours removes, theirs changes: theirs wins", base, ed(setSticker("cccc", "")), ed(setSticker("cccc", "amber")), none,
		map[string]string{"cccc": "amber"}, 0)
	run("both set the same", base, ed(setSticker("bbbb", "bug")), ed(setSticker("bbbb", "bug")), none,
		map[string]string{"bbbb": "bug"}, 0)
	run("different cards", base, ed(setSticker("aaaa", "ship")), ed(setSticker("cccc", "violet")), none,
		map[string]string{"aaaa": "ship", "cccc": "violet"}, 0)
	run("no deletes from ours: theirs' removal still wins", base, base, ed(setSticker("aaaa", "")),
		merge.Options{NoDeletesFromOurs: true}, map[string]string{"aaaa": ""}, 0)
	run("baseless: theirs wins", &task.File{}, ed(setSticker("aaaa", "idea"), setSticker("bbbb", "eyes")), base,
		merge.Options{Baseless: true}, map[string]string{"aaaa": "fire", "bbbb": ""}, 0)
	run("a sticker change keeps a card the other side deleted", base, ed(setSticker("bbbb", "question")),
		ed(func(f *task.File) { f.Boards[0].Tasks = []*task.Task{f.Boards[0].Tasks[0], f.Boards[0].Tasks[2]} }), none,
		map[string]string{"bbbb": "question"}, 1)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(golden); err != nil {
		t.Fatal(err)
	}
	path := "../../testdata/stickers/merge.json"
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

func TestCheckTaskSticker(t *testing.T) {
	for _, s := range []string{"", "fire", "heart", "violet"} {
		if err := merge.CheckTask(T("aaaa", "x", sticker(s))); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"Fire", "🔥", "banana", "none", " fire"} {
		err := merge.CheckTask(T("aaaa", "x", sticker(s)))
		var ve *merge.ValidationError
		if err == nil || !asValidation(err, &ve) || ve.Field != "sticker" {
			t.Errorf("%q: %v", s, err)
		}
	}
}

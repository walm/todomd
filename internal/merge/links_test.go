package merge_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/task"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func L(kind, id string) task.Link { return task.Link{Kind: kind, ID: id} }

func links(ls ...task.Link) func(*task.Task) {
	return func(t *task.Task) { t.Links = ls }
}

func linkBase(t *testing.T) *task.File {
	return file(t,
		bd{"Backlog", []*task.Task{
			T("aaaa", "One", links(L("blocks", "bbbb"), L("related", "cccc"))),
			T("bbbb", "Two"),
			T("cccc", "Three", links(L("split from", "zzzz"))),
		}},
		bd{"Done", nil},
	)
}

func setLinks(id string, ls ...task.Link) func(f *task.File) {
	return func(f *task.File) {
		for _, x := range f.AllTasks() {
			if x.ID == id {
				x.Links = ls
			}
		}
	}
}

// mergeCase is one entry of testdata/links/merge.json, which
// todomd.online's merge.ts checks against too.
type mergeCase struct {
	Name      string           `json:"name"`
	Options   map[string]bool  `json:"options,omitempty"`
	Base      string           `json:"base"`
	Ours      string           `json:"ours"`
	Theirs    string           `json:"theirs"`
	Merged    string           `json:"merged"`
	Conflicts []merge.Conflict `json:"conflicts"`
	Renamed   []merge.Rename   `json:"renamed"`
}

func TestMergeLinks(t *testing.T) {
	base := linkBase(t)
	var golden []mergeCase
	run := func(name string, b, o, th *task.File, opts merge.Options, check func(r merge.Result)) {
		t.Run(name, func(t *testing.T) {
			r := m(t, b, o, th, opts)
			assertValid(t, r.Merged)
			check(r)
			// A re-push of ours onto the result changes nothing.
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
	ed := func(fn func(f *task.File)) *task.File { return edit(t, base, fn) }
	none := merge.Options{}

	run("both add: theirs' order, ours' new appended", base,
		ed(setLinks("aaaa", L("blocks", "bbbb"), L("related", "cccc"), L("duplicate of", "o001"))),
		ed(setLinks("aaaa", L("blocked by", "t001"), L("blocks", "bbbb"), L("related", "cccc"))),
		none, func(r merge.Result) {
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("blocked by", "t001"), L("blocks", "bbbb"), L("related", "cccc"), L("duplicate of", "o001")})
			eq(t, len(r.Conflicts), 0)
		})
	run("ours removes, theirs adds", base,
		ed(setLinks("aaaa", L("related", "cccc"))),
		ed(setLinks("aaaa", L("blocks", "bbbb"), L("related", "cccc"), L("related", "t001"))),
		none, func(r merge.Result) {
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("related", "cccc"), L("related", "t001")})
		})
	run("both add the same link once", base,
		ed(setLinks("bbbb", L("split from", "aaaa"))),
		ed(setLinks("bbbb", L("split from", "aaaa"), L("related", "cccc"))),
		none, func(r merge.Result) {
			eq(t, find(r.Merged, "bbbb").Links, []task.Link{L("split from", "aaaa"), L("related", "cccc")})
		})
	run("same target, another kind: both kept", base,
		ed(setLinks("aaaa", L("blocked by", "bbbb"), L("related", "cccc"))),
		ed(setLinks("aaaa", L("blocks", "bbbb"), L("related", "cccc"), L("related", "bbbb"))),
		none, func(r merge.Result) {
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("related", "cccc"), L("related", "bbbb"), L("blocked by", "bbbb")})
		})
	run("both remove everything", base,
		ed(setLinks("cccc")), ed(setLinks("cccc")),
		none, func(r merge.Result) { eq(t, len(find(r.Merged, "cccc").Links), 0) })
	run("no deletes from ours keeps removed links", base,
		ed(setLinks("aaaa", L("related", "cccc"), L("related", "o001"))), base,
		merge.Options{NoDeletesFromOurs: true}, func(r merge.Result) {
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("blocks", "bbbb"), L("related", "cccc"), L("related", "o001")})
		})
	run("baseless: union, theirs first", &task.File{},
		ed(setLinks("aaaa", L("related", "o001"), L("blocks", "bbbb"))),
		base, merge.Options{Baseless: true}, func(r merge.Result) {
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("blocks", "bbbb"), L("related", "cccc"), L("related", "o001")})
		})
	run("a link change keeps a card the other side deleted", base,
		ed(setLinks("cccc", L("split from", "zzzz"), L("related", "aaaa"))),
		ed(func(f *task.File) { f.Boards[0].Tasks = f.Boards[0].Tasks[:2] }),
		none, func(r merge.Result) {
			if find(r.Merged, "cccc") == nil {
				t.Fatal("cccc was deleted")
			}
			eq(t, len(r.Conflicts), 1)
			eq(t, r.Conflicts[0].Kind, "delete_vs_edit")
		})
	run("a link to a card the other side deleted stays", base,
		ed(setLinks("aaaa", L("blocks", "bbbb"), L("related", "cccc"), L("blocked by", "bbbb"))),
		ed(func(f *task.File) { f.Boards[0].Tasks = f.Boards[0].Tasks[:1] }),
		none, func(r merge.Result) {
			eq(t, layout(r.Merged)[0], []string{"Backlog", "aaaa"})
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("blocks", "bbbb"), L("related", "cccc"), L("blocked by", "bbbb")})
		})

	// Both sides add a card nnnn: ours' is renamed, and the links ours added
	// to it follow; links that were in the base, and theirs' links, don't.
	collBase := edit(t, base, setLinks("bbbb", L("related", "nnnn")))
	run("id collision remaps the links ours added", collBase,
		edit(t, collBase, func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{T("nnnn", "Ours", links(L("split from", "aaaa")))}
			setLinks("aaaa", L("blocks", "bbbb"), L("related", "cccc"), L("blocks", "nnnn"))(f)
			setLinks("bbbb", L("related", "nnnn"), L("split into", "nnnn"))(f)
		}),
		edit(t, collBase, func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{T("nnnn", "Theirs")}
			setLinks("cccc", L("split from", "zzzz"), L("related", "nnnn"))(f)
		}),
		none, func(r merge.Result) {
			if len(r.Renamed) != 1 {
				t.Fatalf("renamed = %+v", r.Renamed)
			}
			to := r.Renamed[0].To
			eq(t, find(r.Merged, to).Title, "Ours")
			eq(t, find(r.Merged, to).Links, []task.Link{L("split from", "aaaa")})
			eq(t, find(r.Merged, "aaaa").Links, []task.Link{L("blocks", "bbbb"), L("related", "cccc"), L("blocks", to)})
			eq(t, find(r.Merged, "bbbb").Links, []task.Link{L("related", "nnnn"), L("split into", to)})
			eq(t, find(r.Merged, "cccc").Links, []task.Link{L("split from", "zzzz"), L("related", "nnnn")})
		})

	path := "../../testdata/links/merge.json"
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(golden); err != nil {
		t.Fatal(err)
	}
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

func TestCheckTaskLinks(t *testing.T) {
	ok := T("aaaa", "x", links(L("blocks", "bbbb"), L("related", "bbbb"), L("split from", "zzzz")))
	if err := merge.CheckTask(ok); err != nil {
		t.Fatal(err)
	}
	many := T("aaaa", "x")
	for i := 0; i < task.MaxLinks; i++ {
		many.Links = append(many.Links, L("related", strings.Repeat(string("0123456789abcdefghijklmnopqrstuvwxyz"[i%36]), 3)+string("01"[i/36])))
	}
	if err := merge.CheckTask(many); err != nil {
		t.Fatalf("%d links: %v", task.MaxLinks, err)
	}
	for name, links := range map[string][]task.Link{
		"self":        {L("related", "aaaa")},
		"duplicate":   {L("blocks", "bbbb"), L("blocks", "bbbb")},
		"kind":        {L("blocked-by", "bbbb")},
		"kind case":   {L("Blocks", "bbbb")},
		"id":          {L("blocks", "BBBB")},
		"short id":    {L("blocks", "bbb")},
		"over limit":  append(append([]task.Link{}, many.Links...), L("blocks", "zzzz")),
		"empty kind":  {L("", "bbbb")},
		"empty id":    {L("related", "")},
		"spaced kind": {L("blocked  by", "bbbb")},
	} {
		x := T("aaaa", "x", func(t *task.Task) { t.Links = links })
		err := merge.CheckTask(x)
		var ve *merge.ValidationError
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !asValidation(err, &ve) || ve.Field != "links" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func asValidation(err error, ve **merge.ValidationError) bool {
	v, ok := err.(*merge.ValidationError)
	*ve = v
	return ok
}

// Every link ours added (not in the base card) is in the merged card, with
// its target remapped if ours renamed that card; the same for theirs.
func TestMergePropertiesNoLostLinks(t *testing.T) {
	n := 0
	cases(1000, func(b, o, th *task.File) {
		r := m(t, b, o, th)
		moved := map[string]string{}
		for _, rn := range r.Renamed {
			moved[rn.From] = rn.To
		}
		for _, side := range []struct {
			f       *task.File
			renames bool
		}{{o, true}, {th, false}} {
			for _, x := range side.f.AllTasks() {
				id := x.ID
				if side.renames && moved[id] != "" {
					id = moved[id]
				}
				got := find(r.Merged, id)
				if got == nil {
					continue // deleted by the other side, unchanged here
				}
				var inBase []task.Link
				if bt := find(b, x.ID); bt != nil {
					inBase = bt.Links
				}
				for _, l := range x.Links {
					if slices.Contains(inBase, l) {
						continue
					}
					want := l
					if side.renames && moved[l.ID] != "" {
						want.ID = moved[l.ID]
					}
					n++
					if want.ID != got.ID && !slices.Contains(got.Links, want) {
						t.Fatalf("link %q added to %s was lost:\nours:\n%s\ntheirs:\n%s\nmerged:\n%s", want, id, write(o), write(th), write(r.Merged))
					}
				}
			}
		}
	})
	if n < 200 {
		t.Fatalf("only %d added links checked", n)
	}
}

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

func day2(d int) task.Date { return task.Date{Year: 2026, Month: 11, Day: d} }

func comments(cs ...task.Comment) func(*task.Task) {
	return func(t *task.Task) { t.Comments = cs }
}

func rx(spec string) []task.Reaction {
	var out []task.Reaction
	for _, e := range strings.Split(spec, "; ") {
		emoji, names, _ := strings.Cut(e, " ")
		out = append(out, task.Reaction{Emoji: emoji, By: strings.Split(names, ",")})
	}
	return out
}

// reactionsOf renders a comment's reactions the way the file line does.
func reactionsOf(f *task.File, id string, k int) string {
	x := find(f, id)
	if x == nil || k >= len(x.Comments) {
		return "<missing>"
	}
	var parts []string
	for _, r := range x.Comments[k].Reactions {
		parts = append(parts, r.Emoji+" "+strings.Join(r.By, ","))
	}
	return strings.Join(parts, "; ")
}

func reactOn(id string, k int, emoji, name string, remove bool) func(f *task.File) {
	return func(f *task.File) {
		if _, err := find(f, id).Comments[k].React(emoji, name, remove); err != nil {
			panic(err)
		}
	}
}

// TestMergeReactions also writes testdata/reactions/merge.json, which
// todomd.online's merge.ts checks against.
func TestMergeReactions(t *testing.T) {
	base := file(t,
		bd{"Backlog", []*task.Task{
			T("aaaa", "One", comments(
				task.Comment{Author: "anna", Date: day2(12), Text: "Can you look?", Reactions: rx("👍 walm")},
				task.Comment{Author: "agent", Date: day2(12), Text: "On it."},
			)),
			T("bbbb", "Two", comments(
				task.Comment{Author: "walm", Date: day2(13), Text: "Hearts", Reactions: rx("❤ anna")},
			)),
		}},
		bd{"Done", nil},
	)
	var golden []mergeCase
	type want struct {
		id    string
		k     int
		lines string
	}
	run := func(name string, b, o, th *task.File, opts merge.Options, conflicts int, wants ...want) {
		t.Run(name, func(t *testing.T) {
			r := m(t, b, o, th, opts)
			assertValid(t, r.Merged)
			for _, w := range wants {
				if got := reactionsOf(r.Merged, w.id, w.k); got != w.lines {
					t.Errorf("%s comment %d: got %q, want %q", w.id, w.k, got, w.lines)
				}
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

	run("ours reacts, theirs unchanged", base, ed(reactOn("aaaa", 1, "👀", "walm", false)), base, none, 0,
		want{"aaaa", 1, "👀 walm"})
	run("both react at once on one comment: both kept", base,
		ed(reactOn("aaaa", 1, "👀", "walm", false)), ed(reactOn("aaaa", 1, "👍", "anna", false)), none, 0,
		want{"aaaa", 1, "👍 anna; 👀 walm"})
	run("both add a name to one emoji: theirs' order, then ours'", base,
		ed(reactOn("aaaa", 0, "👍", "anna", false)), ed(reactOn("aaaa", 0, "👍", "bob", false)), none, 0,
		want{"aaaa", 0, "👍 walm,bob,anna"})
	run("both add the same reaction: once", base,
		ed(reactOn("aaaa", 0, "🎉", "anna", false)), ed(reactOn("aaaa", 0, "🎉", "anna", false)), none, 0,
		want{"aaaa", 0, "👍 walm; 🎉 anna"})
	run("ours removes, theirs adds another", base,
		ed(reactOn("aaaa", 0, "👍", "walm", true)), ed(reactOn("aaaa", 0, "🎉", "anna", false)), none, 0,
		want{"aaaa", 0, "🎉 anna"})
	run("theirs removes, ours adds to the same emoji", base,
		ed(reactOn("aaaa", 0, "👍", "anna", false)), ed(reactOn("aaaa", 0, "👍", "walm", true)), none, 0,
		want{"aaaa", 0, "👍 anna"})
	run("reactions on different cards", base,
		ed(reactOn("aaaa", 0, "✅", "agent", false)), ed(reactOn("bbbb", 0, "🙏", "walm", false)), none, 0,
		want{"aaaa", 0, "👍 walm; ✅ agent"}, want{"bbbb", 0, "❤ anna; 🙏 walm"})
	run("a reaction and a new comment", base,
		ed(reactOn("aaaa", 0, "👀", "agent", false)),
		ed(func(f *task.File) {
			x := find(f, "aaaa")
			x.Comments = append(x.Comments, task.Comment{Author: "walm", Date: day2(13), Text: "Thanks."})
		}), none, 0,
		want{"aaaa", 0, "👍 walm; 👀 agent"}, want{"aaaa", 2, ""})
	run("both add comments, one with a reaction", base,
		ed(func(f *task.File) {
			x := find(f, "aaaa")
			x.Comments = append(x.Comments, task.Comment{Author: "ai", Date: day2(13), Text: "Done.", Reactions: rx("✅ ai")})
		}),
		ed(func(f *task.File) {
			x := find(f, "aaaa")
			x.Comments = append(x.Comments, task.Comment{Author: "walm", Date: day2(13), Text: "Thanks."})
		}), none, 0,
		want{"aaaa", 2, ""}, want{"aaaa", 3, "✅ ai"})
	run("the fuller spelling wins: ❤️ over ❤", base,
		ed(func(f *task.File) { find(f, "bbbb").Comments[0].Reactions = rx("❤️ anna,walm") }), base, none, 0,
		want{"bbbb", 0, "❤️ anna,walm"})
	run("two spellings of one emoji are one reaction", base,
		ed(func(f *task.File) { find(f, "bbbb").Comments[0].Reactions = rx("❤️ anna,walm") }),
		ed(reactOn("bbbb", 0, "❤", "bob", false)), none, 0,
		want{"bbbb", 0, "❤️ anna,bob,walm"})
	run("skin tones are different reactions", base,
		ed(reactOn("aaaa", 0, "👍🏽", "anna", false)), ed(reactOn("aaaa", 0, "👍", "bob", false)), none, 0,
		want{"aaaa", 0, "👍 walm,bob; 👍🏽 anna"})
	run("theirs edits the comment ours reacted to: theirs' comment, its reactions", base,
		ed(reactOn("aaaa", 1, "🎉", "walm", false)),
		ed(func(f *task.File) { find(f, "aaaa").Comments[1].Text = "On it, ETA Friday." }), none, 0,
		want{"aaaa", 1, ""})
	run("ours edits the comment and theirs reacts to it: ours' text, theirs' reactions lost", base,
		ed(func(f *task.File) { find(f, "aaaa").Comments[0].Text = "Can you look? (urgent)" }),
		ed(reactOn("aaaa", 0, "👀", "agent", false)), none, 0,
		want{"aaaa", 0, "👍 walm"})
	run("no deletes from ours: ours' removal is undone", base,
		ed(reactOn("aaaa", 0, "👍", "walm", true)), ed(reactOn("aaaa", 0, "🎉", "anna", false)),
		merge.Options{NoDeletesFromOurs: true}, 0,
		want{"aaaa", 0, "👍 walm; 🎉 anna"})
	run("no deletes from ours: theirs' removal still wins", base, base, ed(reactOn("aaaa", 0, "👍", "walm", true)),
		merge.Options{NoDeletesFromOurs: true}, 0,
		want{"aaaa", 0, ""})
	run("baseless: union", &task.File{}, ed(reactOn("aaaa", 0, "👀", "agent", false)), ed(reactOn("aaaa", 0, "🎉", "anna", false)),
		merge.Options{Baseless: true}, 0,
		want{"aaaa", 0, "👍 walm; 🎉 anna; 👀 agent"})
	run("a reaction keeps a card the other side deleted", base, ed(reactOn("bbbb", 0, "👍", "walm", false)),
		ed(func(f *task.File) { f.Boards[0].Tasks = f.Boards[0].Tasks[:1] }), none, 1,
		want{"bbbb", 0, "❤ anna; 👍 walm"})

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(golden); err != nil {
		t.Fatal(err)
	}
	path := "../../testdata/reactions/merge.json"
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(buf.Bytes(), want2) {
		t.Errorf("merge.json differs; run with -update and review:\n%s", buf.Bytes())
	}
}

func TestCheckTaskReactions(t *testing.T) {
	c := func(rs []task.Reaction) *task.Task {
		return T("aaaa", "x", comments(task.Comment{Author: "a", Date: day2(1), Text: "t", Reactions: rs}))
	}
	for _, rs := range [][]task.Reaction{nil, rx("👍 walm"), rx("👍 walm,anna; 👍🏽 bob; 1️⃣ a")} {
		if err := merge.CheckTask(c(rs)); err != nil {
			t.Errorf("%v: %v", rs, err)
		}
	}
	for _, rs := range [][]task.Reaction{
		rx("👍 Walm"), rx("ok walm"), rx("👍 a,a"), rx("❤ a; ❤️ b"), {{Emoji: "👍"}}, rx("👍 a b"),
	} {
		err := merge.CheckTask(c(rs))
		var ve *merge.ValidationError
		if err == nil || !asValidation(err, &ve) || ve.Field != "reactions" {
			t.Errorf("%v: %v", rs, err)
		}
	}
}

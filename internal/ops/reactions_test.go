package ops_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/todogen"
)

const reactionsDoc = "# TODO\n\n## Backlog\n\n### One\n<!-- id:aaaa -->\n\n#### Comments\n\n" +
	"- **anna** (2026-11-12): Can you look?\n  <!-- reactions: 👍 walm -->\n" +
	"- **agent** (2026-11-12): On it.\n\n### Two\n<!-- id:bbbb -->\n\n## Done\n"

func TestDeltaReactions(t *testing.T) {
	base := doc(t, reactionsDoc)
	react := func(k int, emoji, name string, remove bool) func(f *task.File) {
		return func(f *task.File) {
			if _, err := f.Boards[0].Tasks[0].Comments[k].React(emoji, name, remove); err != nil {
				t.Fatal(err)
			}
		}
	}
	cases := []struct {
		name string
		edit func(f *task.File)
		want string
	}{
		{"reaction added to a comment", react(1, "👀", "walm", false),
			`[{"op":"comment.set","task":"aaaa","comments":[{"author":"anna","date":"2026-11-12","text":"Can you look?","reactions":[{"emoji":"👍","by":["walm"]}]},{"author":"agent","date":"2026-11-12","text":"On it.","reactions":[{"emoji":"👀","by":["walm"]}]}]}]`},
		{"second name on an emoji", react(0, "👍", "anna", false),
			`[{"op":"comment.set","task":"aaaa","comments":[{"author":"anna","date":"2026-11-12","text":"Can you look?","reactions":[{"emoji":"👍","by":["walm","anna"]}]},{"author":"agent","date":"2026-11-12","text":"On it."}]}]`},
		{"last reaction removed", react(0, "👍", "walm", true),
			`[{"op":"comment.set","task":"aaaa","comments":[{"author":"anna","date":"2026-11-12","text":"Can you look?"},{"author":"agent","date":"2026-11-12","text":"On it."}]}]`},
		{"comment added with reactions", func(f *task.File) {
			x := f.Boards[0].Tasks[1]
			x.Comments = []task.Comment{{Author: "ai", Date: task.Date{Year: 2026, Month: 11, Day: 13}, Text: "Done.",
				Reactions: []task.Reaction{{Emoji: "✅", By: []string{"walm"}}, {Emoji: "❤️", By: []string{"anna", "bob"}}}}}
		}, `[{"op":"comment.add","task":"bbbb","comment":{"author":"ai","date":"2026-11-13","text":"Done.","reactions":[{"emoji":"✅","by":["walm"]},{"emoji":"❤️","by":["anna","bob"]}]}}]`},
		{"task added with a reacted comment", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three", Comments: []task.Comment{{Author: "ai",
				Date: task.Date{Year: 2026, Month: 11, Day: 13}, Text: "x", Reactions: []task.Reaction{{Emoji: "🎉", By: []string{"walm"}}}}}}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"description":"","comments":[{"author":"ai","date":"2026-11-13","text":"x","reactions":[{"emoji":"🎉","by":["walm"]}]}]}}]`},
		{"plain comment added keeps the format-3 shape", func(f *task.File) {
			x := f.Boards[0].Tasks[0]
			x.Comments = append(x.Comments, task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 13}, Text: "Thanks."})
		}, `[{"op":"comment.add","task":"aaaa","comment":{"author":"walm","date":"2026-11-13","text":"Thanks."}}]`},
	}
	var golden []opsCase
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := edit(t, base, c.edit)
			got := roundTrip(t, base, b)
			if got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
			golden = append(golden, opsCase{c.name, string(markdown.Write(base)), string(markdown.Write(b)), json.RawMessage(got)})
		})
	}
	writeOpsGolden(t, "../../testdata/reactions/ops.json", golden)
}

func TestDecodeReactions(t *testing.T) {
	ok := `[{"op":"comment.add","task":"aaaa","comment":{"author":"a","date":"2026-01-01","text":"t","reactions":[{"emoji":"👍","by":["walm","anna"]},{"emoji":"1️⃣","by":["x"]}]}},` +
		`{"op":"comment.set","task":"aaaa","comments":[{"author":"a","date":"2026-01-01","text":"t","reactions":[]}]}]`
	list, err := ops.Decode([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if rs := list[0].Comment.Reactions; len(rs) != 2 || rs[0].Emoji != "👍" || strings.Join(rs[0].By, ",") != "walm,anna" {
		t.Errorf("reactions = %+v", rs)
	}
	if rs := list[1].Comments[0].Reactions; len(rs) != 0 {
		t.Errorf("[] read as %+v", rs)
	}
	for _, r := range []string{
		`{"emoji":"👍","by":[]}`,
		`{"emoji":"👍","by":["Walm"]}`,
		`{"emoji":"👍","by":["a b"]}`,
		`{"emoji":"👍","by":["a","a"]}`,
		`{"emoji":"ok","by":["a"]}`,
		`{"emoji":"","by":["a"]}`,
		`{"emoji":"👍","by":"a"}`,
		`{"emoji":"👍"}`,
		`{"emoji":"👍","by":["a"],"at":1}`,
	} {
		raw := `[{"op":"comment.add","task":"aaaa","comment":{"author":"a","date":"2026-01-01","text":"t","reactions":[` + r + `]}}]`
		if _, err := ops.Decode([]byte(raw)); err == nil || !strings.HasPrefix(err.Error(), "op ") {
			t.Errorf("%s: %v", r, err)
		}
	}
	for _, rs := range []string{
		`[{"emoji":"❤","by":["a"]},{"emoji":"❤️","by":["b"]}]`, // one emoji twice
		`null`,
		`{}`,
	} {
		raw := `[{"op":"comment.add","task":"aaaa","comment":{"author":"a","date":"2026-01-01","text":"t","reactions":` + rs + `}}]`
		if _, err := ops.Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", rs)
		}
	}
}

func TestGeneratorMakesReactions(t *testing.T) {
	reacted, escaped := 0, 0
	for seed := uint32(1); seed <= 500; seed++ {
		r := todogen.New(seed)
		f := todogen.Mutate(r, todogen.File(r, 1), todogen.EditOptions{})
		for _, x := range f.AllTasks() {
			for _, c := range x.Comments {
				if len(c.Reactions) > 0 {
					reacted++
				}
			}
		}
		if strings.Contains(string(markdown.Write(f)), "\n  \\<!-- reactions: ") {
			escaped++
		}
	}
	if reacted < 100 || escaped < 3 {
		t.Errorf("generator too tame: %d comments with reactions, %d files with an escaped reactions line", reacted, escaped)
	}
}

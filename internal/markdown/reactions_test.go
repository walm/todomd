package markdown

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

// Any comment built from lines that look like a reactions line reads back
// exactly, with or without reactions of its own.
func TestReactionLookalikeCommentsRoundTrip(t *testing.T) {
	pool := []string{
		"<!-- reactions: 👍 walm -->", "\\<!-- reactions: 👍 walm -->", "\\\\<!-- reactions: 👀 a,b; ✅ c -->",
		"<!-- reactions: ❤ a; ❤️ b -->", "<!-- reactions: 👍 a,a -->", "<!-- reactions: 👍 Walm -->",
		"<!-- reactions: ok walm -->", "<!-- reactions: 👍 -->", " <!-- reactions: 👍 walm -->",
		"<!-- sticker: fire -->", "- **x** (2026-01-01): y", "#### Comments", "### fake", "```",
		"", "", "text", "  indented", "\\",
	}
	reactions := [][]task.Reaction{
		{{Emoji: "👍", By: []string{"walm"}}},
		{{Emoji: "❤️", By: []string{"a", "b"}}, {Emoji: "👀", By: []string{"agent"}}},
	}
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 50000; i++ {
		lines := make([]string, 1+r.IntN(4))
		for k := range lines {
			lines[k] = pool[r.IntN(len(pool))]
		}
		text := strings.TrimRight(strings.Join(lines, "\n"), "\n ")
		c := task.Comment{Author: "a", Date: task.Date{Year: 2026, Month: 1, Day: 1}, Text: text}
		if r.IntN(2) == 0 {
			c.Reactions = reactions[r.IntN(len(reactions))]
		}
		x := &task.Task{ID: "aaaa", Title: "t", Comments: []task.Comment{c, {Author: "b", Date: c.Date, Text: "next"}}}
		if r.IntN(2) == 0 {
			x.Comments = x.Comments[:1]
		}
		out := WriteTask(x)
		back, err := ParseTaskStrict(out)
		if err != nil {
			t.Fatalf("%q: %v\n%s", text, err, out)
		}
		if !task.SameComments(back.Comments, x.Comments) {
			t.Fatalf("read back differently:\nwant %+v\ngot  %+v\n%s", x.Comments, back.Comments, out)
		}
		if again := WriteTask(back); string(again) != string(out) {
			t.Fatalf("not a fixed point:\n%s\n---\n%s", out, again)
		}
	}
}

func TestReactionsFragment(t *testing.T) {
	x := &task.Task{ID: "aaaa", Title: "t", Comments: []task.Comment{{Author: "anna", Date: task.Date{Year: 2026, Month: 11, Day: 13},
		Text: "Look?", Reactions: []task.Reaction{{Emoji: "👍", By: []string{"walm", "anna"}}, {Emoji: "👀", By: []string{"agent"}}}}}}
	want := "### t\n<!-- id:aaaa -->\n\n#### Comments\n\n- **anna** (2026-11-13): Look?\n  <!-- reactions: 👍 walm,anna; 👀 agent -->\n"
	if got := string(WriteTask(x)); got != want {
		t.Fatalf("got\n%s", got)
	}
}

package markdown

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

// Any description built from lines that look like a links section reads back
// exactly, with or without links of the card's own, and with comments after.
func TestLinkLookalikeDescriptionsRoundTrip(t *testing.T) {
	pool := []string{
		"#### Links", "#### Links", "\\#### Links", "\\\\#### Links", "#### Links \t", "\\#### Links\t", "####  Links",
		"- related abcd", "- related abcd", "- Blocks zz01", "-\tsplit-from 0000 ", "- bogus abcd", "- related ABCD",
		"", "", " ", " ", "text", "```", "~~~", "### fake task", "\\### escaped", "#### Comments",
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 50000; i++ {
		lines := make([]string, 1+r.IntN(7))
		for k := range lines {
			lines[k] = pool[r.IntN(len(pool))]
		}
		if isBlank(lines[0]) || isBlank(lines[len(lines)-1]) {
			continue
		}
		desc := strings.Join(lines, "\n")
		if UnclosedFence(desc) {
			continue
		}
		x := &task.Task{ID: "aaaa", Title: "t", Description: desc}
		if r.IntN(2) == 0 {
			x.Links = []task.Link{{Kind: "related", ID: "bbbb"}}
		}
		if r.IntN(3) == 0 {
			x.Comments = []task.Comment{{Author: "a", Date: task.Date{Year: 2026, Month: 1, Day: 1}, Text: "c"}}
		}
		text := WriteTask(x)
		back, err := ParseTaskStrict(text)
		if err != nil {
			t.Fatalf("%q: %v\n%s", desc, err, text)
		}
		if back.Description != x.Description || !slices.Equal(back.Links, x.Links) || len(back.Comments) != len(x.Comments) {
			t.Fatalf("read back differently:\ndesc  %q\nlinks %v\ngot   %q %v\n%s", desc, x.Links, back.Description, back.Links, text)
		}
		if again := WriteTask(back); string(again) != string(text) {
			t.Fatalf("not a fixed point:\n%s\n---\n%s", text, again)
		}
	}
}

func TestLinksFragmentRoundTrip(t *testing.T) {
	x := &task.Task{ID: "aaaa", Title: "t", Tags: []string{"x"}, Description: "d",
		Links:    []task.Link{{Kind: "split from", ID: "mpbv"}, {Kind: "blocked by", ID: "zz01"}},
		Comments: []task.Comment{{Author: "a", Date: task.Date{Year: 2026, Month: 1, Day: 1}, Text: "c"}}}
	want := "### t\n<!-- id:aaaa -->\n`#x`\n\nd\n\n#### Links\n\n- split from mpbv\n- blocked by zz01\n\n#### Comments\n\n- **a** (2026-01-01): c\n"
	if got := string(WriteTask(x)); got != want {
		t.Fatalf("got\n%s", got)
	}
	back, err := ParseTask([]byte(want))
	if err != nil || !slices.Equal(back.Links, x.Links) || back.Description != "d" {
		t.Fatalf("%+v %v", back, err)
	}
}

func TestSelfLinkDroppedAfterIDRepair(t *testing.T) {
	// Two cards share an id; the second gets a new one, so its link to the
	// first card's id is no longer a self-link and stays.
	f := mustParse(t, "# T\n\n## B\n\n### One\n<!-- id:aaaa -->\n\n#### Links\n\n- related aaaa\n- related bbbb\n\n### Two\n<!-- id:aaaa -->\n\n#### Links\n\n- related aaaa\n")
	one, two := f.Boards[0].Tasks[0], f.Boards[0].Tasks[1]
	if !slices.Equal(one.Links, []task.Link{{Kind: "related", ID: "bbbb"}}) {
		t.Errorf("one: %v", one.Links)
	}
	if two.ID == "aaaa" || !slices.Equal(two.Links, []task.Link{{Kind: "related", ID: "aaaa"}}) {
		t.Errorf("two %s: %v", two.ID, two.Links)
	}
}

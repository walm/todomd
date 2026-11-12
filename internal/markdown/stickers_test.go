package markdown

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

// Any description built from lines that look like a sticker line reads back
// exactly, with or without a sticker and metadata of the card's own.
func TestStickerLookalikeDescriptionsRoundTrip(t *testing.T) {
	pool := []string{
		"<!-- sticker: fire -->", "<!-- sticker: red -->", "\\<!-- sticker: star -->", "\\\\<!-- sticker: bug -->",
		"<!-- sticker: banana -->", "\\<!-- sticker: banana -->", "<!-- sticker: Fire -->", "<!-- sticker: fire --> ",
		"`#tag`", "\\`#tag`", "**priority:** high", "#### Links", "- related abcd", "<!-- id:abcd -->",
		"", "", " ", "text", "```", "~~~", "### fake task", "#### Comments",
	}
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 50000; i++ {
		lines := make([]string, 1+r.IntN(5))
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
			x.Sticker = task.Stickers[r.IntN(len(task.Stickers))].Name
		}
		if r.IntN(2) == 0 {
			x.Tags = []string{"x"}
		}
		if r.IntN(3) == 0 {
			x.Links = []task.Link{{Kind: "related", ID: "bbbb"}}
		}
		text := WriteTask(x)
		back, err := ParseTaskStrict(text)
		if err != nil {
			t.Fatalf("%q: %v\n%s", desc, err, text)
		}
		if back.Description != x.Description || back.Sticker != x.Sticker || !slices.Equal(back.Tags, x.Tags) || !slices.Equal(back.Links, x.Links) {
			t.Fatalf("read back differently:\ndesc    %q sticker %q\ngot     %q sticker %q\n%s", desc, x.Sticker, back.Description, back.Sticker, text)
		}
		if again := WriteTask(back); string(again) != string(text) {
			t.Fatalf("not a fixed point:\n%s\n---\n%s", text, again)
		}
	}
}

func TestStickerFragment(t *testing.T) {
	x := &task.Task{ID: "aaaa", Title: "t", Tags: []string{"x"}, Priority: task.PriorityHigh, Sticker: "fire", Description: "d"}
	want := "### t\n<!-- id:aaaa -->\n`#x` **priority:** high\n\n<!-- sticker: fire -->\n\nd\n"
	if got := string(WriteTask(x)); got != want {
		t.Fatalf("got\n%s", got)
	}
	x.Tags, x.Priority, x.Description = nil, task.PriorityNormal, ""
	want = "### t\n<!-- id:aaaa -->\n\n<!-- sticker: fire -->\n"
	if got := string(WriteTask(x)); got != want {
		t.Fatalf("got\n%s", got)
	}
	back, err := ParseTask([]byte(want))
	if err != nil || back.Sticker != "fire" || back.Description != "" {
		t.Fatalf("%+v %v", back, err)
	}
}

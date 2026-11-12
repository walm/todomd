package ops_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/todogen"
)

const stickersDoc = "# TODO\n\n## Backlog\n\n### One\n<!-- id:aaaa -->\n`#x` **priority:** high\n\n<!-- sticker: fire -->\n\nDesc.\n\n### Two\n<!-- id:bbbb -->\n\nDesc.\n\n## Done\n"

// writeOpsGolden compares (or with -update rewrites) a shared ops.json.
func writeOpsGolden(t *testing.T, path string, golden []opsCase) {
	t.Helper()
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
		t.Errorf("%s differs; run with -update and review:\n%s", path, buf.Bytes())
	}
}

func TestDeltaStickers(t *testing.T) {
	base := doc(t, stickersDoc)
	cases := []struct {
		name string
		edit func(f *task.File)
		want string
	}{
		{"sticker set", func(f *task.File) {
			f.Boards[0].Tasks[1].Sticker = "star"
		}, `[{"op":"task.update","id":"bbbb","set":{"sticker":"star"}}]`},
		{"sticker changed to a ribbon", func(f *task.File) {
			f.Boards[0].Tasks[0].Sticker = "red"
		}, `[{"op":"task.update","id":"aaaa","set":{"sticker":"red"}}]`},
		{"sticker removed", func(f *task.File) {
			f.Boards[0].Tasks[0].Sticker = ""
		}, `[{"op":"task.update","id":"aaaa","set":{"sticker":null}}]`},
		{"sticker next to other fields", func(f *task.File) {
			x := f.Boards[0].Tasks[1]
			x.Title, x.Description, x.Priority = "Two!", "New.", task.PriorityLow
			x.Sticker = "bug"
			x.Links = []task.Link{{Kind: task.LinkRelated, ID: "aaaa"}}
		}, `[{"op":"task.update","id":"bbbb","set":{"title":"Two!","priority":"low","sticker":"bug","description":"New.","links":[{"kind":"related","id":"aaaa"}]}}]`},
		{"description changed under a sticker", func(f *task.File) {
			f.Boards[0].Tasks[0].Description = "Other."
		}, `[{"op":"task.update","id":"aaaa","set":{"description":"Other."}}]`},
		{"task added with a sticker", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three", Sticker: "heart",
				Links: []task.Link{{Kind: task.LinkBlocks, ID: "aaaa"}}}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"sticker":"heart","description":"","links":[{"kind":"blocks","id":"aaaa"}],"comments":[]}}]`},
		{"task added without a sticker has no sticker key", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three"}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"description":"","comments":[]}}]`},
		{"moved task keeps its sticker", func(f *task.File) {
			x := f.Boards[0].Tasks[0]
			f.Boards[0].Tasks = f.Boards[0].Tasks[1:]
			f.Boards[1].Tasks = []*task.Task{x}
		}, `[{"op":"task.move","id":"aaaa","board":"Done","after":null}]`},
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
	writeOpsGolden(t, "../../testdata/stickers/ops.json", golden)
}

func TestDecodeStickers(t *testing.T) {
	ok := `[{"op":"task.update","id":"aaaa","set":{"sticker":"violet"}},` +
		`{"op":"task.update","id":"aaaa","set":{"sticker":null}},` +
		`{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"sticker":"fire","description":"","comments":[]}},` +
		`{"op":"task.add","board":"B","after":null,"task":{"id":"abce","title":"t","tags":[],"priority":"low","due":null,"sticker":null,"description":"","comments":[]}}]`
	list, err := ops.Decode([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if s := list[0].Set.Sticker; s == nil || *s != "violet" {
		t.Errorf("sticker = %v", s)
	}
	if s := list[1].Set.Sticker; s == nil || *s != "" {
		t.Errorf("cleared sticker = %v", s)
	}
	if list[2].Task.Sticker != "fire" || list[3].Task.Sticker != "" {
		t.Errorf("task stickers = %q %q", list[2].Task.Sticker, list[3].Task.Sticker)
	}
	for _, raw := range []string{
		`[{"op":"task.update","id":"aaaa","set":{"sticker":""}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"sticker":"Fire"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"sticker":"🔥"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"sticker":"banana"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"sticker":1}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"sticker":["fire"]}}]`,
		`[{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"sticker":"none","description":"","comments":[]}}]`,
	} {
		if _, err := ops.Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		} else if !strings.HasPrefix(err.Error(), "op ") {
			t.Errorf("%s: not an OpError: %v", raw, err)
		}
	}
}

func TestGeneratorMakesStickers(t *testing.T) {
	cards, escaped, lookalike := 0, 0, 0
	for seed := uint32(1); seed <= 500; seed++ {
		r := todogen.New(seed)
		f := todogen.Mutate(r, todogen.File(r, 1), todogen.EditOptions{})
		for _, x := range f.AllTasks() {
			if x.Sticker != "" {
				cards++
			}
			if strings.HasPrefix(x.Description, "<!-- sticker:") {
				lookalike++
			}
		}
		if strings.Contains(string(markdown.Write(f)), "\n\\<!-- sticker: ") {
			escaped++
		}
	}
	if cards < 100 || lookalike < 10 || escaped < 3 {
		t.Errorf("generator too tame: %d cards with a sticker, %d descriptions starting with a sticker line, %d files with an escaped one", cards, lookalike, escaped)
	}
}

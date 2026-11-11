package ops_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
	"github.com/walm/todomd/internal/todogen"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const linksDoc = "# TODO\n\n## Backlog\n\n### One\n<!-- id:aaaa -->\n\n#### Links\n\n- blocks bbbb\n- related zzzz\n\n### Two\n<!-- id:bbbb -->\n\nDesc.\n\n## Done\n"

// opsCase is one entry of testdata/links/ops.json: Delta(a, b) must be ops,
// and Apply(a, ops) must write b. todomd.online's delta.ts reads the same
// file.
type opsCase struct {
	Name string          `json:"name"`
	A    string          `json:"a"`
	B    string          `json:"b"`
	Ops  json.RawMessage `json:"ops"`
}

func TestDeltaLinks(t *testing.T) {
	base := doc(t, linksDoc)
	link := func(kind, id string) task.Link { return task.Link{Kind: kind, ID: id} }
	cases := []struct {
		name string
		edit func(f *task.File)
		want string
	}{
		{"link added", func(f *task.File) {
			f.Boards[0].Tasks[1].Links = []task.Link{link(task.LinkSplitFrom, "aaaa")}
		}, `[{"op":"task.update","id":"bbbb","set":{"links":[{"kind":"split from","id":"aaaa"}]}}]`},
		{"links cleared", func(f *task.File) {
			f.Boards[0].Tasks[0].Links = nil
		}, `[{"op":"task.update","id":"aaaa","set":{"links":[]}}]`},
		{"links reordered", func(f *task.File) {
			l := f.Boards[0].Tasks[0].Links
			l[0], l[1] = l[1], l[0]
		}, `[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"related","id":"zzzz"},{"kind":"blocks","id":"bbbb"}]}}]`},
		{"links next to other fields", func(f *task.File) {
			t := f.Boards[0].Tasks[1]
			t.Title, t.Description = "Two!", "New."
			t.Links = []task.Link{link(task.LinkDuplicateOf, "aaaa")}
		}, `[{"op":"task.update","id":"bbbb","set":{"title":"Two!","description":"New.","links":[{"kind":"duplicate of","id":"aaaa"}]}}]`},
		{"task added with links", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three", Links: []task.Link{link(task.LinkBlockedBy, "aaaa")}}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"description":"","links":[{"kind":"blocked by","id":"aaaa"}],"comments":[]}}]`},
		{"task added without links has no links key", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three"}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"description":"","comments":[]}}]`},
		{"moved task keeps its links", func(f *task.File) {
			t := f.Boards[0].Tasks[0]
			f.Boards[0].Tasks = f.Boards[0].Tasks[1:]
			f.Boards[1].Tasks = []*task.Task{t}
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

	path := "../../testdata/links/ops.json"
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
		t.Errorf("ops.json differs; run with -update and review:\n%s", buf.Bytes())
	}
}

func TestDecodeLinks(t *testing.T) {
	ok := `[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocked by","id":"bbbb"}]}},` +
		`{"op":"task.update","id":"aaaa","set":{"links":[]}},` +
		`{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"description":"","links":[{"kind":"related","id":"zzzz"}],"comments":[]}}]`
	list, err := ops.Decode([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if l := *list[0].Set.Links; len(l) != 1 || l[0] != (task.Link{Kind: "blocked by", ID: "bbbb"}) {
		t.Errorf("links = %+v", l)
	}
	if list[1].Set.Links == nil || len(*list[1].Set.Links) != 0 {
		t.Errorf("cleared links = %+v", list[1].Set.Links)
	}
	if l := list[2].Task.Links; len(l) != 1 || l[0].ID != "zzzz" {
		t.Errorf("task links = %+v", l)
	}

	bad := []string{
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocked-by","id":"bbbb"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"Blocks","id":"bbbb"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"bogus","id":"bbbb"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocks","id":"BBBB"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocks"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocks","id":"bbbb","title":"x"}]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":[{"kind":"blocks","id":"bbbb"}],"extra":1}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":{"kind":"blocks","id":"bbbb"}}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":null}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"links":["blocks bbbb"]}}]`,
		`[{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"description":"","links":[]}}]`,
	}
	for _, raw := range bad {
		if _, err := ops.Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		} else if !strings.HasPrefix(err.Error(), "op ") {
			t.Errorf("%s: not an OpError: %v", raw, err)
		}
	}
}

func TestGeneratorMakesLinks(t *testing.T) {
	cards, escaped, lookalike := 0, 0, 0
	for seed := uint32(1); seed <= 500; seed++ {
		r := todogen.New(seed)
		f := todogen.Mutate(r, todogen.File(r, 1), todogen.EditOptions{})
		for _, x := range f.AllTasks() {
			if len(x.Links) > 0 {
				cards++
			}
			if strings.Contains(x.Description, "#### Links") {
				lookalike++
			}
		}
		if strings.Contains(string(markdown.Write(f)), "\n\\#### Links\n") {
			escaped++
		}
	}
	if cards < 100 || lookalike < 50 || escaped < 3 {
		t.Errorf("generator too tame: %d cards with links, %d descriptions with a heading, %d files with an escaped one", cards, lookalike, escaped)
	}
}

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

const assigneesDoc = "# TODO\n\n## Backlog\n\n### One\n<!-- id:aaaa -->\n`#x` **priority:** high\n\n<!-- sticker: fire -->\n\n<!-- assignee: anna -->\n\nDesc.\n\n### Two\n<!-- id:bbbb -->\n\nDesc.\n\n## Done\n"

// TestDeltaAssignees also writes testdata/assignees/ops.json.
func TestDeltaAssignees(t *testing.T) {
	base := doc(t, assigneesDoc)
	cases := []struct {
		name string
		edit func(f *task.File)
		want string
	}{
		{"assignee set", func(f *task.File) {
			f.Boards[0].Tasks[1].Assignee = "bob"
		}, `[{"op":"task.update","id":"bbbb","set":{"assignee":"bob"}}]`},
		{"assignee changed", func(f *task.File) {
			f.Boards[0].Tasks[0].Assignee = "agent"
		}, `[{"op":"task.update","id":"aaaa","set":{"assignee":"agent"}}]`},
		{"assignee removed", func(f *task.File) {
			f.Boards[0].Tasks[0].Assignee = ""
		}, `[{"op":"task.update","id":"aaaa","set":{"assignee":null}}]`},
		{"assignee next to other fields", func(f *task.File) {
			x := f.Boards[0].Tasks[1]
			x.Title, x.Description, x.Priority = "Two!", "New.", task.PriorityLow
			x.Sticker, x.Assignee = "bug", "claude"
			x.Links = []task.Link{{Kind: task.LinkRelated, ID: "aaaa"}}
		}, `[{"op":"task.update","id":"bbbb","set":{"title":"Two!","priority":"low","sticker":"bug","assignee":"claude","description":"New.","links":[{"kind":"related","id":"aaaa"}]}}]`},
		{"sticker removed, assignee kept", func(f *task.File) {
			f.Boards[0].Tasks[0].Sticker = ""
		}, `[{"op":"task.update","id":"aaaa","set":{"sticker":null}}]`},
		{"description changed under an assignee", func(f *task.File) {
			f.Boards[0].Tasks[0].Description = "Other."
		}, `[{"op":"task.update","id":"aaaa","set":{"description":"Other."}}]`},
		{"description that looks like an assignee line", func(f *task.File) {
			f.Boards[0].Tasks[1].Description = "<!-- assignee: bob -->"
			// Go's encoder escapes < and >; the same JSON value.
		}, `[{"op":"task.update","id":"bbbb","set":{"description":"\u003c!-- assignee: bob --\u003e"}}]`},
		{"task added with an assignee", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three", Sticker: "heart", Assignee: "walm",
				Links: []task.Link{{Kind: task.LinkBlocks, ID: "aaaa"}}}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"sticker":"heart","assignee":"walm","description":"","links":[{"kind":"blocks","id":"aaaa"}],"comments":[]}}]`},
		{"task added with only an assignee", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three", Assignee: "x-1"}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"assignee":"x-1","description":"","comments":[]}}]`},
		{"task added without an assignee has no assignee key", func(f *task.File) {
			f.Boards[1].Tasks = []*task.Task{{ID: "cccc", Title: "Three"}}
		}, `[{"op":"task.add","board":"Done","after":null,"task":{"id":"cccc","title":"Three","tags":[],"priority":"normal","due":null,"description":"","comments":[]}}]`},
		{"moved task keeps its assignee", func(f *task.File) {
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
	writeOpsGolden(t, "../../testdata/assignees/ops.json", golden)
}

func TestDecodeAssignees(t *testing.T) {
	ok := `[{"op":"task.update","id":"aaaa","set":{"assignee":"anna"}},` +
		`{"op":"task.update","id":"aaaa","set":{"assignee":null}},` +
		`{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"assignee":"a-1","description":"","comments":[]}},` +
		`{"op":"task.add","board":"B","after":null,"task":{"id":"abce","title":"t","tags":[],"priority":"low","due":null,"assignee":null,"description":"","comments":[]}}]`
	list, err := ops.Decode([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	if s := list[0].Set.Assignee; s == nil || *s != "anna" {
		t.Errorf("assignee = %v", s)
	}
	if s := list[1].Set.Assignee; s == nil || *s != "" {
		t.Errorf("cleared assignee = %v", s)
	}
	if list[2].Task.Assignee != "a-1" || list[3].Task.Assignee != "" {
		t.Errorf("task assignees = %q %q", list[2].Task.Assignee, list[3].Task.Assignee)
	}
	for _, raw := range []string{
		`[{"op":"task.update","id":"aaaa","set":{"assignee":""}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":"@anna"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":"Anna"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":"a_b"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":"-a"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":"` + strings.Repeat("a", 40) + `"}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":1}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignee":["anna"]}}]`,
		`[{"op":"task.update","id":"aaaa","set":{"assignees":"anna"}}]`,
		`[{"op":"task.add","board":"B","after":null,"task":{"id":"abcd","title":"t","tags":[],"priority":"low","due":null,"assignee":"anna bob","description":"","comments":[]}}]`,
	} {
		if _, err := ops.Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		} else if !strings.HasPrefix(err.Error(), "op ") {
			t.Errorf("%s: not an OpError: %v", raw, err)
		}
	}
}

func TestGeneratorMakesAssignees(t *testing.T) {
	cards, escaped, lookalike := 0, 0, 0
	for seed := uint32(1); seed <= 500; seed++ {
		r := todogen.New(seed)
		f := todogen.Mutate(r, todogen.File(r, 1), todogen.EditOptions{})
		for _, x := range f.AllTasks() {
			if x.Assignee != "" {
				cards++
			}
			if strings.HasPrefix(x.Description, "<!-- assignee:") {
				lookalike++
			}
		}
		if strings.Contains(string(markdown.Write(f)), "\n\\<!-- assignee: ") {
			escaped++
		}
	}
	if cards < 100 || lookalike < 10 || escaped < 3 {
		t.Errorf("generator too tame: %d cards with an assignee, %d descriptions starting with an assignee line, %d files with an escaped one", cards, lookalike, escaped)
	}
}

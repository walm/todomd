package markdown

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

// The golden files under testdata/links are shared with todomd.online's
// TypeScript implementation (packages/todofile), which must read and write
// them the same way. For each NAME.md:
//
//	NAME.out.md  Write(Parse(NAME.md)), byte for byte
//	NAME.json    the parsed model
//	NAME.err     instead of both: the parse error
//
// Regenerate with `go test ./internal/markdown -run Golden -update` and
// review the diff.
var update = flag.Bool("update", false, "rewrite the golden files")

const goldenDir = "../../testdata/links"

type goldenLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type goldenComment struct {
	Author string `json:"author"`
	Date   string `json:"date"`
	Text   string `json:"text"`
}

type goldenTask struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Tags        []string        `json:"tags"`
	Priority    string          `json:"priority"`
	Due         *string         `json:"due"`
	Description string          `json:"description"`
	Links       []goldenLink    `json:"links"`
	Comments    []goldenComment `json:"comments"`
}

type goldenBoard struct {
	Name  string       `json:"name"`
	Tasks []goldenTask `json:"tasks"`
}

type goldenFile struct {
	Title    string        `json:"title"`
	Preamble string        `json:"preamble"`
	Boards   []goldenBoard `json:"boards"`
}

func goldenModel(f *task.File) []byte {
	g := goldenFile{Title: f.Title, Preamble: f.Preamble, Boards: []goldenBoard{}}
	for _, b := range f.Boards {
		gb := goldenBoard{Name: b.Name, Tasks: []goldenTask{}}
		for _, t := range b.Tasks {
			gt := goldenTask{ID: t.ID, Title: t.Title, Tags: append([]string{}, t.Tags...),
				Priority: t.Priority.String(), Description: t.Description,
				Links: []goldenLink{}, Comments: []goldenComment{}}
			if t.Due != nil {
				d := t.Due.String()
				gt.Due = &d
			}
			for _, l := range t.Links {
				gt.Links = append(gt.Links, goldenLink{l.Kind, l.ID})
			}
			for _, c := range t.Comments {
				gt.Comments = append(gt.Comments, goldenComment{c.Author, c.Date.String(), c.Text})
			}
			gb.Tasks = append(gb.Tasks, gt)
		}
		g.Boards = append(g.Boards, gb)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", filepath.Base(path), got, want)
	}
}

func TestGoldenLinks(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join(goldenDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, in := range inputs {
		if strings.HasSuffix(in, ".out.md") || filepath.Base(in) == "README.md" {
			continue
		}
		n++
		name := strings.TrimSuffix(in, ".md")
		t.Run(filepath.Base(name), func(t *testing.T) {
			data, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			f, err := ParseStrict(data)
			if err != nil {
				var pe *ParseError
				if !errors.As(err, &pe) {
					t.Fatal(err)
				}
				checkGolden(t, name+".err", []byte(err.Error()+"\n"))
				if _, err2 := Parse(data); err2 == nil || err2.Error() != err.Error() {
					t.Errorf("Parse and ParseStrict disagree: %v / %v", err2, err)
				}
				return
			}
			if _, err := os.Stat(name + ".err"); err == nil && !*update {
				t.Fatalf("parsed, but %s.err expects an error", filepath.Base(name))
			}
			out := Write(f)
			checkGolden(t, name+".out.md", out)
			checkGolden(t, name+".json", goldenModel(f))
			// Write is a fixed point, and the model survives it.
			back, err := ParseStrict(out)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			if again := Write(back); !bytes.Equal(again, out) {
				t.Errorf("not a fixed point:\n%s\n---\n%s", out, again)
			}
			if !bytes.Equal(goldenModel(back), goldenModel(f)) {
				t.Error("model changed on reparse")
			}
		})
	}
	if n < 10 {
		t.Fatalf("only %d golden inputs found in %s", n, goldenDir)
	}
}

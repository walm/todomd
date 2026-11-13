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

// The golden files under testdata/links, testdata/stickers and
// testdata/reactions are shared
// with todomd.online's TypeScript implementation (packages/todofile), which
// must read and write them the same way. For each NAME.md:
//
//	NAME.out.md  Write(Parse(NAME.md)), byte for byte
//	NAME.json    the parsed model
//	NAME.err     instead of both: the parse error
//
// Regenerate with `go test ./internal/markdown -run Golden -update` and
// review the diff.
var update = flag.Bool("update", false, "rewrite the golden files")

const (
	linksDir     = "../../testdata/links"
	stickersDir  = "../../testdata/stickers"
	reactionsDir = "../../testdata/reactions"
)

// goldenMode is what a golden directory's JSON shows beyond format 2.
type goldenMode struct {
	sticker   bool // each card's sticker, a name or null
	reactions bool // each comment's reactions, an array
}

type goldenLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type goldenReaction struct {
	Emoji string   `json:"emoji"`
	By    []string `json:"by"`
}

type goldenComment struct {
	Author    string            `json:"author"`
	Date      string            `json:"date"`
	Text      string            `json:"text"`
	Reactions *[]goldenReaction `json:"reactions,omitempty"` // reactions only: always an array there
}

type goldenTask struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Tags        []string        `json:"tags"`
	Priority    string          `json:"priority"`
	Due         *string         `json:"due"`
	Sticker     json.RawMessage `json:"sticker,omitempty"` // stickers only: a name or null
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

// goldenModel is the parsed model as JSON. mode adds each card's sticker (a
// name or null) and each comment's reactions (an array); the files of older
// features predate them and leave them out.
func goldenModel(f *task.File, mode goldenMode) []byte {
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
			if mode.sticker {
				gt.Sticker = json.RawMessage("null")
				if t.Sticker != "" {
					gt.Sticker, _ = json.Marshal(t.Sticker)
				}
			}
			for _, l := range t.Links {
				gt.Links = append(gt.Links, goldenLink{l.Kind, l.ID})
			}
			for _, c := range t.Comments {
				gc := goldenComment{Author: c.Author, Date: c.Date.String(), Text: c.Text}
				if mode.reactions {
					rs := []goldenReaction{}
					for _, r := range c.Reactions {
						rs = append(rs, goldenReaction{r.Emoji, append([]string{}, r.By...)})
					}
					gc.Reactions = &rs
				}
				gt.Comments = append(gt.Comments, gc)
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

func TestGoldenLinks(t *testing.T)    { runGolden(t, linksDir, goldenMode{}, 10) }
func TestGoldenStickers(t *testing.T) { runGolden(t, stickersDir, goldenMode{sticker: true}, 10) }
func TestGoldenReactions(t *testing.T) {
	runGolden(t, reactionsDir, goldenMode{sticker: true, reactions: true}, 8)
}

func runGolden(t *testing.T, goldenDir string, mode goldenMode, min int) {
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
			checkGolden(t, name+".json", goldenModel(f, mode))
			// Write is a fixed point, and the model survives it.
			back, err := ParseStrict(out)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			if again := Write(back); !bytes.Equal(again, out) {
				t.Errorf("not a fixed point:\n%s\n---\n%s", out, again)
			}
			if !bytes.Equal(goldenModel(back, mode), goldenModel(f, mode)) {
				t.Error("model changed on reparse")
			}
		})
	}
	if n < min {
		t.Fatalf("only %d golden inputs found in %s", n, goldenDir)
	}
}

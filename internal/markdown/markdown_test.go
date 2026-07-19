package markdown

import (
	"strings"
	"testing"

	"github.com/walm/todomd/internal/task"
)

const sample = `# TODO

Some preamble text.

## Backlog

### Implement parser
<!-- id:3f2a -->
` + "`#parser` `#core`" + ` **due:** 2026-08-01

Line-based parser.

Second paragraph.

#### Comments

- **ai** (2026-07-18): Consider goldmark.
- **andreas** (2026-07-18): Agreed, keep it
  hand-rolled.

### Empty task
<!-- id:9c41 -->

## In Progress

### Set up CI
<!-- id:b7d0 -->
` + "`#infra`" + `

## Done
`

func mustParse(t *testing.T, s string) *task.File {
	t.Helper()
	f, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func TestParseSample(t *testing.T) {
	f := mustParse(t, sample)
	if f.Title != "TODO" {
		t.Errorf("title = %q", f.Title)
	}
	if f.Preamble != "Some preamble text." {
		t.Errorf("preamble = %q", f.Preamble)
	}
	if len(f.Boards) != 3 {
		t.Fatalf("boards = %d", len(f.Boards))
	}
	b := f.Boards[0]
	if b.Name != "Backlog" || len(b.Tasks) != 2 {
		t.Fatalf("backlog: %q %d tasks", b.Name, len(b.Tasks))
	}
	tk := b.Tasks[0]
	if tk.ID != "3f2a" || tk.Title != "Implement parser" {
		t.Errorf("task = %q %q", tk.ID, tk.Title)
	}
	if len(tk.Tags) != 2 || tk.Tags[0] != "parser" {
		t.Errorf("tags = %v", tk.Tags)
	}
	if tk.Due == nil || tk.Due.String() != "2026-08-01" {
		t.Errorf("due = %v", tk.Due)
	}
	if tk.Description != "Line-based parser.\n\nSecond paragraph." {
		t.Errorf("desc = %q", tk.Description)
	}
	if len(tk.Comments) != 2 {
		t.Fatalf("comments = %d", len(tk.Comments))
	}
	if tk.Comments[1].Text != "Agreed, keep it\nhand-rolled." {
		t.Errorf("comment = %q", tk.Comments[1].Text)
	}
	if f.Boards[2].Name != "Done" || len(f.Boards[2].Tasks) != 0 {
		t.Errorf("done board should be empty")
	}
}

// Write must be a fixed point over Parse.
func TestWriteFixedPoint(t *testing.T) {
	f := mustParse(t, sample)
	out1 := Write(f)
	f2 := mustParse(t, string(out1))
	out2 := Write(f2)
	if string(out1) != string(out2) {
		t.Errorf("write not a fixed point:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
	}
}

func date(t *testing.T, s string) task.Date {
	t.Helper()
	d, err := task.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Nasty descriptions and comments must round-trip exactly.
func TestRoundTripHostileContent(t *testing.T) {
	due := date(t, "2026-01-02")
	descs := []string{
		"## fake board\n### fake task\n#### Comments\n# fake title",
		"<!-- id:zzzz -->\ninjection attempt",
		"\\## already escaped\n\\\\### double",
		"```\n## heading inside fence\n<!-- id:aaaa -->\n```\nafter",
		"`#tag-lookalike`",
		"**due:** 2026-01-01",
		"code:\n\n    indented block\n\ntail",
		"~~~\nfence with tildes\n## boo\n~~~",
	}
	comments := []string{
		"simple",
		"multi\nline\n\nwith blank",
		"- **fake** (2026-01-01): nested item\n  deeper",
		"### heading in comment",
	}
	for _, desc := range descs {
		f := &task.File{Boards: []*task.Board{{Name: "B", Tasks: []*task.Task{{
			ID: "aaaa", Title: "T", Description: desc,
		}, {
			ID: "bbbb", Title: "Sentinel", Due: &due,
		}}}}}
		for _, c := range comments {
			f.Boards[0].Tasks[0].Comments = append(f.Boards[0].Tasks[0].Comments,
				task.Comment{Author: "ai", Date: due, Text: c})
		}
		out := Write(f)
		f2, err := Parse(out)
		if err != nil {
			t.Fatalf("desc %q: reparse: %v\n%s", desc, err, out)
		}
		if len(f2.Boards) != 1 || len(f2.Boards[0].Tasks) != 2 {
			t.Fatalf("desc %q: structure corrupted:\n%s", desc, out)
		}
		got := f2.Boards[0].Tasks[0]
		if got.Description != desc {
			t.Errorf("desc mismatch:\nwant %q\ngot  %q", desc, got.Description)
		}
		for i, c := range comments {
			if got.Comments[i].Text != c {
				t.Errorf("comment mismatch:\nwant %q\ngot  %q", c, got.Comments[i].Text)
			}
		}
		if f2.Boards[0].Tasks[1].Title != "Sentinel" {
			t.Errorf("desc %q swallowed the next task", desc)
		}
	}
}

func TestCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(sample, "\n", "\r\n")
	f := mustParse(t, crlf)
	if strings.Contains(string(Write(f)), "\r") {
		t.Error("output contains CR")
	}
	if f.Boards[0].Tasks[0].Description != "Line-based parser.\n\nSecond paragraph." {
		t.Errorf("desc = %q", f.Boards[0].Tasks[0].Description)
	}
}

func TestMissingAndDuplicateIDs(t *testing.T) {
	src := `# T

## B

### No id here

### Dup
<!-- id:aaaa -->

### Dup two
<!-- id:aaaa -->
`
	f := mustParse(t, src)
	seen := map[string]bool{}
	for _, tk := range f.AllTasks() {
		if !task.ValidID(tk.ID) {
			t.Errorf("invalid id %q", tk.ID)
		}
		if seen[tk.ID] {
			t.Errorf("duplicate id %q", tk.ID)
		}
		seen[tk.ID] = true
	}
	if f.Boards[0].Tasks[1].ID != "aaaa" {
		t.Errorf("first duplicate should keep its id, got %q", f.Boards[0].Tasks[1].ID)
	}
}

func TestFenceShieldsHeadings(t *testing.T) {
	src := "# T\n\n## B\n\n### Task\n<!-- id:aaaa -->\n\n```\n## not a board\n### not a task\n```\n\n### Real second task\n<!-- id:bbbb -->\n"
	f := mustParse(t, src)
	if len(f.Boards[0].Tasks) != 2 {
		t.Fatalf("tasks = %d", len(f.Boards[0].Tasks))
	}
	want := "```\n## not a board\n### not a task\n```"
	if got := f.Boards[0].Tasks[0].Description; got != want {
		t.Errorf("desc = %q", got)
	}
}

// A description whose first line looks like metadata must not be re-parsed
// as tags (the writer backslash-escapes such a first line; the parser
// unescapes it).
func TestMetadataLookalikeDescription(t *testing.T) {
	f := &task.File{Boards: []*task.Board{{Name: "B", Tasks: []*task.Task{{
		ID: "aaaa", Title: "T", Description: "`#nottag` **due:** 2026-01-01",
	}}}}}
	f2 := mustParse(t, string(Write(f)))
	got := f2.Boards[0].Tasks[0]
	if len(got.Tags) != 0 || got.Due != nil {
		t.Errorf("description leaked into metadata: tags=%v due=%v", got.Tags, got.Due)
	}
	if got.Description != "`#nottag` **due:** 2026-01-01" {
		t.Errorf("desc = %q", got.Description)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, src, wantSub string
		line               int
	}{
		{"task before board", "# T\n\n### orphan\n", "before any", 3},
		{"content under board", "# T\n\n## B\n\nstray text\n", "unexpected content", 5},
		{"malformed comment", "# T\n\n## B\n\n### X\n<!-- id:aaaa -->\n\n#### Comments\n\n- fix this later\n", "malformed comment", 10},
		{"bad comment date", "# T\n\n## B\n\n### X\n<!-- id:aaaa -->\n\n#### Comments\n\n- **a** (2026-13-01): x\n", "invalid date", 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.src))
			if err == nil {
				t.Fatal("expected error")
			}
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("not a ParseError: %v", err)
			}
			if !strings.Contains(pe.Msg, c.wantSub) || pe.Line != c.line {
				t.Errorf("got line %d %q, want line %d containing %q", pe.Line, pe.Msg, c.line, c.wantSub)
			}
		})
	}
}

func TestInvalidMetadataIsDescription(t *testing.T) {
	src := "# T\n\n## B\n\n### X\n<!-- id:aaaa -->\n`#tag` **due:** 2026-99-99\n"
	f := mustParse(t, src)
	tk := f.Boards[0].Tasks[0]
	if len(tk.Tags) != 0 {
		t.Errorf("tags = %v", tk.Tags)
	}
	if !strings.Contains(tk.Description, "2026-99-99") {
		t.Errorf("desc = %q", tk.Description)
	}
}

func TestUnclosedFence(t *testing.T) {
	if !UnclosedFence("```\nopen") {
		t.Error("want true for unclosed")
	}
	if UnclosedFence("```\nclosed\n```") {
		t.Error("want false for closed")
	}
	if UnclosedFence("plain text") {
		t.Error("want false for plain")
	}
}

func TestEmptyFile(t *testing.T) {
	f := mustParse(t, "")
	if len(f.Boards) != 0 {
		t.Errorf("boards = %d", len(f.Boards))
	}
	if string(Write(f)) != "# TODO\n" {
		t.Errorf("write = %q", Write(f))
	}
}

// Markdown formatters (prettier et al.) insert a blank line after the id
// comment; metadata must survive that.
func TestFormatterNormalizedFile(t *testing.T) {
	src := "# T\n\n## B\n\n### X\n<!-- id:aaaa -->\n\n`#tui` **due:** 2026-08-15\n\nReal description.\n"
	f := mustParse(t, src)
	tk := f.Boards[0].Tasks[0]
	if len(tk.Tags) != 1 || tk.Tags[0] != "tui" || tk.Due == nil {
		t.Fatalf("metadata lost: tags=%v due=%v", tk.Tags, tk.Due)
	}
	if tk.Description != "Real description." {
		t.Errorf("desc = %q", tk.Description)
	}
	// And the canonical rewrite re-attaches it adjacently.
	f2 := mustParse(t, string(Write(f)))
	if len(f2.Boards[0].Tasks[0].Tags) != 1 {
		t.Error("metadata lost after rewrite")
	}
}

func TestTaskFragmentRoundTrip(t *testing.T) {
	due := date(t, "2026-08-15")
	tk := &task.Task{
		ID: "aaaa", Title: "Frag", Tags: []string{"x", "y"}, Due: &due,
		Description: "Body with\n\n```\n## fenced\n```",
		Comments:    []task.Comment{{Author: "ai", Date: due, Text: "hi\nthere"}},
	}
	got, err := ParseTask(WriteTask(tk))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != tk.Title || got.Description != tk.Description ||
		len(got.Tags) != 2 || got.Due == nil || len(got.Comments) != 1 ||
		got.Comments[0].Text != "hi\nthere" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestParseTaskErrors(t *testing.T) {
	if _, err := ParseTask([]byte("### One\n\n### Two\n")); err == nil ||
		!strings.Contains(err.Error(), "exactly one task") {
		t.Errorf("two tasks: %v", err)
	}
	// Error line numbers refer to the fragment, not the wrapper.
	_, err := ParseTask([]byte("### X\n<!-- id:aaaa -->\n\n#### Comments\n\n- broken\n"))
	pe, ok := err.(*ParseError)
	if !ok || pe.Line != 6 {
		t.Errorf("want ParseError at fragment line 6, got %v", err)
	}
}

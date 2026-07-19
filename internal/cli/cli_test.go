package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the CLI with args against a fresh command tree, capturing
// stdout. Errors come back for exit-code classification by the caller.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	flagJSON = false // reset shared flag state between runs
	root := newRoot()
	root.SetArgs(args)
	root.SetOut(w)
	root.SetErr(w)
	err := root.Execute()

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String(), err
}

func testFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "TODO.md")
	if _, err := run(t, "--file", path, "init"); err != nil {
		t.Fatal(err)
	}
	return path
}

func addOne(t *testing.T, path, title string, extra ...string) string {
	t.Helper()
	args := append([]string{"--file", path, "add", title, "--json"}, extra...)
	out, err := run(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	var tk map[string]any
	if err := json.Unmarshal([]byte(out), &tk); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	return tk["id"].(string)
}

func TestAddListShow(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Fix the parser", "--tag", "parser", "--due", "2026-08-01", "--desc", "Some\n\ndetails")

	out, err := run(t, "--file", path, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, id) || !strings.Contains(out, "Fix the parser") || !strings.Contains(out, "#parser") {
		t.Errorf("list output: %s", out)
	}

	out, err = run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Board       string   `json:"board"`
		Tags        []string `json:"tags"`
		Due         *string  `json:"due"`
		Description string   `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &tk); err != nil {
		t.Fatal(err)
	}
	if tk.Board != "Backlog" || tk.Tags[0] != "parser" || *tk.Due != "2026-08-01" || tk.Description != "Some\n\ndetails" {
		t.Errorf("show = %+v", tk)
	}
}

func TestUpdateMoveDoneDelete(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Task A")

	if _, err := run(t, "--file", path, "update", id, "--title", "Task A2", "--tag", "x", "--due", "2026-12-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "update", id, "--clear-due", "--clear-tags"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, "--file", path, "show", id, "--json")
	if strings.Contains(out, "2026-12-01") || strings.Contains(out, `"x"`) {
		t.Errorf("clear flags failed: %s", out)
	}
	if !strings.Contains(out, "Task A2") {
		t.Errorf("title not updated: %s", out)
	}

	if _, err := run(t, "--file", path, "move", id, "--to", "In Progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "done", id); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, "--file", path, "show", id, "--json")
	if !strings.Contains(out, `"board": "Done"`) {
		t.Errorf("not done: %s", out)
	}

	out, err := run(t, "--file", path, "delete", id, "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"board": "Done"`) {
		t.Errorf("delete json should carry former board: %s", out)
	}
	if _, err := run(t, "--file", path, "show", id); err == nil {
		t.Error("task should be gone")
	}
}

func TestCommentAndFileContent(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Task B")
	if _, err := run(t, "--file", path, "comment", id, "--author", "ai", "Looks good.\nSecond line."); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "**ai** (") || !strings.Contains(string(data), "  Second line.") {
		t.Errorf("file content:\n%s", data)
	}
}

func TestExitCodeClassification(t *testing.T) {
	path := testFile(t)
	addOne(t, path, "One")

	// Not found.
	_, err := run(t, "--file", path, "show", "zzzz")
	if code := classify(err); code != 2 {
		t.Errorf("not-found exit = %d (%v)", code, err)
	}
	// Missing file.
	_, err = run(t, "--file", filepath.Join(t.TempDir(), "nope.md"), "list")
	if code := classify(err); code != 1 {
		t.Errorf("missing-file exit = %d (%v)", code, err)
	}
	// Injection attempts rejected.
	_, err = run(t, "--file", path, "add", "evil\ntitle")
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Errorf("newline title should be rejected: %v", err)
	}
}

func TestAmbiguousExitCode(t *testing.T) {
	path := testFile(t)
	// Force two tasks whose IDs share a prefix by retrying adds until true.
	var a, b string
	for i := 0; i < 500; i++ {
		id := addOne(t, path, "T")
		if a == "" {
			a = id
			continue
		}
		if id[0] == a[0] {
			b = id
			break
		}
	}
	if b == "" {
		t.Skip("no shared prefix in 500 tries")
	}
	_, err := run(t, "--file", path, "show", a[:1])
	if code := classify(err); code != 3 {
		t.Errorf("ambiguous exit = %d (%v)", code, err)
	}
}

func TestHostileContentViaCLI(t *testing.T) {
	path := testFile(t)
	desc := "## fake board\n### fake task\n<!-- id:aaaa -->"
	id := addOne(t, path, "Sneaky", "--desc", desc)
	addOne(t, path, "After")

	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Description string `json:"description"`
	}
	json.Unmarshal([]byte(out), &tk)
	if tk.Description != desc {
		t.Errorf("description mangled:\nwant %q\ngot  %q", desc, tk.Description)
	}
	out, _ = run(t, "--file", path, "boards", "--json")
	if strings.Contains(out, "fake board") {
		t.Errorf("injection created a board: %s", out)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walm/todomd/internal/selfupdate"
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
	if os.Getenv("XDG_STATE_HOME") == "" || !strings.HasPrefix(os.Getenv("XDG_STATE_HOME"), os.TempDir()) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}
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

func TestChangesFlow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	idA := addOne(t, path, "Task A")

	// First call initializes, no events.
	out, err := run(t, "--file", path, "changes", "--as", "bot", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var init struct {
		Initialized bool `json:"initialized"`
	}
	json.Unmarshal([]byte(out), &init)
	if !init.Initialized {
		t.Fatalf("first call should initialize: %s", out)
	}

	// Rename (same ID!), move, comment, add.
	if _, err := run(t, "--file", path, "update", idA, "--title", "Task A renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "move", idA, "--to", "In Progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "comment", idA, "--author", "walm", "please check this"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "comment", idA, "--author", "bot", "my own note"); err != nil {
		t.Fatal(err)
	}
	idB := addOne(t, path, "Task B")

	out, err = run(t, "--file", path, "changes", "--as", "bot", "--ignore-author", "bot", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Events []struct {
			Type    string         `json:"type"`
			TaskID  string         `json:"task"`
			Fields  map[string]any `json:"fields"`
			Comment *struct {
				Author string `json:"author"`
			} `json:"comment"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	types := map[string]int{}
	for _, e := range got.Events {
		types[e.Type]++
		if e.Type == "task_updated" && e.TaskID == idA {
			if _, ok := e.Fields["title"]; !ok {
				t.Errorf("rename must appear as title field change: %s", out)
			}
		}
		if e.Type == "comment_added" && e.Comment.Author == "bot" {
			t.Error("--ignore-author bot leaked through")
		}
		if e.Type == "task_added" && e.TaskID != idB {
			t.Errorf("unexpected task_added %s", e.TaskID)
		}
		if e.Type == "task_deleted" {
			t.Errorf("rename must never appear as delete+add: %s", out)
		}
	}
	if types["task_updated"] != 1 || types["task_moved"] != 1 || types["comment_added"] != 1 || types["task_added"] != 1 {
		t.Errorf("event mix = %v\n%s", types, out)
	}

	// Cursor advanced: next call is empty.
	out, _ = run(t, "--file", path, "changes", "--as", "bot", "--json")
	if !strings.Contains(out, `"events": []`) {
		t.Errorf("cursor did not advance: %s", out)
	}
}

func TestChangesPeekAndSeparateCursors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	if _, err := run(t, "--file", path, "changes", "--as", "a", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "changes", "--as", "b", "--json"); err != nil {
		t.Fatal(err)
	}
	addOne(t, path, "New one")

	// Peek twice: both see it; cursor b unaffected by a's reads.
	for i := 0; i < 2; i++ {
		out, err := run(t, "--file", path, "changes", "--as", "a", "--peek", "--json")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "task_added") {
			t.Errorf("peek %d missed event: %s", i, out)
		}
	}
	out, _ := run(t, "--file", path, "changes", "--as", "a", "--json")
	if !strings.Contains(out, "task_added") {
		t.Errorf("read after peek missed event: %s", out)
	}
	out, _ = run(t, "--file", path, "changes", "--as", "b", "--json")
	if !strings.Contains(out, "task_added") {
		t.Errorf("cursor b should still see the event: %s", out)
	}
}

func TestChangesPersistsHandAddedIDs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	if _, err := run(t, "--file", path, "changes", "--as", "bot", "--json"); err != nil {
		t.Fatal(err)
	}
	// Simulate a human adding a task in an editor, without an id comment.
	data, _ := os.ReadFile(path)
	data = append(data, []byte("\n### Hand added task\n\nSome notes.\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "--file", path, "changes", "--as", "bot", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Events []struct {
			Type   string `json:"type"`
			TaskID string `json:"task"`
		} `json:"events"`
	}
	json.Unmarshal([]byte(out), &got)
	if len(got.Events) != 1 || got.Events[0].Type != "task_added" {
		t.Fatalf("events = %s", out)
	}
	// The reported ID must now exist in the file itself.
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "id:"+got.Events[0].TaskID) {
		t.Errorf("event id %q not persisted to file:\n%s", got.Events[0].TaskID, data)
	}
}

// Regression for #1: a consumer that writes to the board was woken by its own
// mutations, because only comment_added honoured attribution.
func TestChangesSkipsOwnMutations(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	id := addOne(t, path, "probe")
	if _, err := run(t, "--file", path, "changes", "--as", "probe", "--json"); err != nil {
		t.Fatal(err)
	}

	// Every mutation kind, attributed to this consumer.
	for _, args := range [][]string{
		{"move", id, "--to", "In Progress", "--as", "probe"},
		{"update", id, "--title", "probe renamed", "--as", "probe"},
		{"comment", id, "--author", "probe", "mine", "--as", "probe"},
		{"add", "second task", "--as", "probe"},
		{"done", id, "--as", "probe"},
	} {
		if _, err := run(t, append([]string{"--file", path}, args...)...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if evs := changeTypes(t, path, "probe"); len(evs) != 0 {
		t.Errorf("own mutations reported back: %v", evs)
	}

	// An unattributed write (someone else) is still reported.
	if _, err := run(t, "--file", path, "move", id, "--to", "Backlog"); err != nil {
		t.Fatal(err)
	}
	if evs := changeTypes(t, path, "probe"); len(evs) != 1 || evs[0] != "task_moved" {
		t.Errorf("other writer's move = %v, want [task_moved]", evs)
	}
}

// Folding in the writer's own change must not hide what someone else did to the
// same task — the "please review" comment must survive the agent's own move.
func TestChangesKeepsOthersChangesOnSameTask(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	id := addOne(t, path, "probe")
	if _, err := run(t, "--file", path, "changes", "--as", "agent", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "comment", id, "--author", "human", "please review"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "move", id, "--to", "In Progress", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	if evs := changeTypes(t, path, "agent"); len(evs) != 1 || evs[0] != "comment_added" {
		t.Errorf("got %v, want just the human's [comment_added]", evs)
	}
}

func TestChangesOwnWriteIsPerCursor(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	id := addOne(t, path, "probe")
	for _, name := range []string{"agentA", "agentB"} {
		if _, err := run(t, "--file", path, "changes", "--as", name, "--json"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run(t, "--file", path, "move", id, "--to", "In Progress", "--as", "agentA"); err != nil {
		t.Fatal(err)
	}
	if evs := changeTypes(t, path, "agentA"); len(evs) != 0 {
		t.Errorf("writer saw its own move: %v", evs)
	}
	if evs := changeTypes(t, path, "agentB"); len(evs) != 1 {
		t.Errorf("other cursor should still see the move, got %v", evs)
	}
}

func TestChangesCursorFromEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	id := addOne(t, path, "probe")
	t.Setenv("TODOMD_CURSOR", "envbot")
	// No --as anywhere: both the read and the write use $TODOMD_CURSOR.
	if _, err := run(t, "--file", path, "changes", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "move", id, "--to", "Done"); err != nil {
		t.Fatal(err)
	}
	if evs := changeTypes(t, path, "envbot"); len(evs) != 0 {
		t.Errorf("own move via TODOMD_CURSOR reported back: %v", evs)
	}
}

// Attributing a write to a cursor that doesn't exist yet must not fail the
// write (the consumer's first read baselines from current state anyway).
func TestOwnWriteWithUntrackedCursor(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := testFile(t)
	if _, err := run(t, "--file", path, "add", "x", "--as", "neverseen"); err != nil {
		t.Fatalf("write failed for an untracked cursor: %v", err)
	}
	out, err := run(t, "--file", path, "list")
	if err != nil || !strings.Contains(out, "x") {
		t.Errorf("task not written: %v\n%s", err, out)
	}
}

// changeTypes reads and advances a cursor, returning the event types.
func changeTypes(t *testing.T, path, cursor string) []string {
	t.Helper()
	out, err := run(t, "--file", path, "changes", "--as", cursor, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	types := make([]string, len(got.Events))
	for i, e := range got.Events {
		types[i] = e.Type
	}
	return types
}

// runStreams runs the CLI capturing stdout and stderr separately.
func runStreams(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	flagJSON = false
	root := newRoot()
	root.SetArgs(args)
	root.SetOut(wOut)
	root.SetErr(wErr)
	_ = root.Execute()

	wOut.Close()
	wErr.Close()
	var bo, be bytes.Buffer
	bo.ReadFrom(rOut)
	be.ReadFrom(rErr)
	return bo.String(), be.String()
}

// The update notice is for humans only: it appears at the end of --help, on
// stderr, and nowhere else. Agents drive the other commands and their stdout
// must stay exactly as it was.
func TestUpdateNoticeOnlyInHelpAndOnStderr(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := selfupdate.SaveCache("v9.9.9", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Pretend this build is a release, otherwise the notice self-suppresses.
	old := version
	version = "v0.1.0"
	t.Cleanup(func() { version = old })

	stdout, stderr := runStreams(t, "--help")
	if !strings.Contains(stderr, "v9.9.9") || !strings.Contains(stderr, "todomd upgrade") {
		t.Errorf("--help should hint on stderr, got stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "v9.9.9") {
		t.Errorf("--help stdout must stay clean for parsers:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Available Commands") {
		t.Errorf("help text missing from stdout:\n%s", stdout)
	}

	// Ordinary commands never mention it, on either stream.
	path := testFile(t)
	addOne(t, path, "a task")
	for _, args := range [][]string{
		{"--file", path, "list"},
		{"--file", path, "list", "--json"},
		{"--file", path, "boards", "--json"},
		{"--file", path, "add", "another", "--json"},
	} {
		out, errOut := runStreams(t, args...)
		if strings.Contains(out, "v9.9.9") || strings.Contains(errOut, "v9.9.9") {
			t.Errorf("%v leaked the update notice:\nstdout=%s\nstderr=%s", args, out, errOut)
		}
	}
}

func TestUpdateNoticeSuppressedForDevBuilds(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := selfupdate.SaveCache("v9.9.9", time.Now()); err != nil {
		t.Fatal(err)
	}
	// version is "dev" in tests: nothing to compare, so stay quiet.
	_, stderr := runStreams(t, "--help")
	if strings.Contains(stderr, "v9.9.9") {
		t.Errorf("dev build should not nag: %s", stderr)
	}
}

func TestPriorityViaCLI(t *testing.T) {
	path := testFile(t)
	high := addOne(t, path, "Urgent thing", "--priority", "high")
	normal := addOne(t, path, "Ordinary thing")
	low := addOne(t, path, "Someday", "--priority", "low")

	// JSON always states the priority, so an agent never has to infer it.
	for id, want := range map[string]string{high: "high", normal: "normal", low: "low"} {
		out, err := run(t, "--file", path, "show", id, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var tk struct {
			Priority string `json:"priority"`
		}
		if err := json.Unmarshal([]byte(out), &tk); err != nil {
			t.Fatal(err)
		}
		if tk.Priority != want {
			t.Errorf("%s priority = %q, want %q", id, tk.Priority, want)
		}
	}

	// Filtering is what makes "do the high ones first" a one-liner.
	out, err := run(t, "--file", path, "list", "--priority", "high")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Urgent thing") || strings.Contains(out, "Ordinary thing") {
		t.Errorf("--priority high listed the wrong tasks:\n%s", out)
	}

	// update changes it; the file keeps normal implicit.
	if _, err := run(t, "--file", path, "update", low, "--priority", "high"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "update", high, "--priority", "normal"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "priority:** normal") {
		t.Errorf("normal should not be written to the file:\n%s", data)
	}
	if n := strings.Count(string(data), "**priority:** high"); n != 1 {
		t.Errorf("want exactly one high task in the file, got %d:\n%s", n, data)
	}

	// Bad values are rejected, not silently normalised.
	if _, err := run(t, "--file", path, "add", "x", "--priority", "urgent"); err == nil {
		t.Error("invalid priority should be rejected")
	}
	if _, err := run(t, "--file", path, "list", "--priority", "urgent"); err == nil {
		t.Error("invalid --priority filter should be rejected")
	}
}

// gitRepo turns dir into a repo, optionally committing path.
func gitRepo(t *testing.T, dir, path string, commit bool) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if commit {
		run("add", filepath.Base(path))
		run("commit", "-qm", "todo")
	}
}

// doneTasks moves n freshly added tasks to Done and returns their ids.
func doneTasks(t *testing.T, path string, n int) []string {
	t.Helper()
	var ids []string
	for i := 0; i < n; i++ {
		id := addOne(t, path, fmt.Sprintf("finished %d", i))
		if _, err := run(t, "--file", path, "done", id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// Without git history behind it, a bulk delete refuses rather than quietly
// destroying tasks.
func TestArchiveRefusesWhenNotRecoverable(t *testing.T) {
	path := testFile(t)
	doneTasks(t, path, 2)

	_, err := run(t, "--file", path, "archive", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("expected a refusal outside git, got %v", err)
	}
	out, _ := run(t, "--file", path, "list", "--board", "Done")
	if !strings.Contains(out, "finished 0") {
		t.Error("tasks must survive a refused archive")
	}

	// A repo alone isn't enough: uncommitted means history lacks these tasks.
	gitRepo(t, filepath.Dir(path), path, false)
	if _, err := run(t, "--file", path, "archive", "--yes"); err == nil ||
		!strings.Contains(err.Error(), "untracked") {
		t.Errorf("expected a refusal for an untracked file, got %v", err)
	}
	// --force is the escape hatch.
	if _, err := run(t, "--file", path, "archive", "--yes", "--force"); err != nil {
		t.Fatalf("--force should proceed: %v", err)
	}
	out, _ = run(t, "--file", path, "list", "--board", "Done")
	if strings.TrimSpace(out) != "" {
		t.Errorf("Done should be empty: %q", out)
	}
}

func TestArchiveCommittedFile(t *testing.T) {
	path := testFile(t)
	doneTasks(t, path, 3)
	addOne(t, path, "still open")
	gitRepo(t, filepath.Dir(path), path, true)

	out, err := run(t, "--file", path, "archive", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Board string `json:"board"`
		Count int    `json:"count"`
		Tasks []struct {
			Title string `json:"title"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Board != "Done" || res.Count != 3 || len(res.Tasks) != 3 {
		t.Errorf("result = %+v", res)
	}
	// The board itself stays, and other boards are untouched.
	out, _ = run(t, "--file", path, "boards", "--json")
	if !strings.Contains(out, `"name": "Done"`) {
		t.Error("archive should keep the board, only clear it")
	}
	out, _ = run(t, "--file", path, "list")
	if !strings.Contains(out, "still open") || strings.Contains(out, "finished") {
		t.Errorf("wrong tasks left:\n%s", out)
	}
}

func TestArchiveDryRunChangesNothing(t *testing.T) {
	path := testFile(t)
	doneTasks(t, path, 2)
	before, _ := os.ReadFile(path)

	out, err := run(t, "--file", path, "archive", "--dry-run")
	if err != nil {
		t.Fatal(err) // a dry run works even where a real one would refuse
	}
	if !strings.Contains(out, "would archive 2 tasks") || !strings.Contains(out, "permanent") {
		t.Errorf("dry run should preview and warn:\n%s", out)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("dry run modified the file")
	}
}

// --to keeps the tasks, so it doesn't need git history to be safe.
func TestArchiveToFile(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "shipped it", "--priority", "high", "--tag", "rel")
	if _, err := run(t, "--file", path, "comment", id, "--author", "ai", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "done", id); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(filepath.Dir(path), "ARCHIVE.md")

	if _, err := run(t, "--file", path, "archive", "--to", dest, "--yes"); err != nil {
		t.Fatalf("--to should not need git: %v", err)
	}
	// The destination is a valid board holding the task, metadata and all.
	out, err := run(t, "--file", dest, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Title    string `json:"title"`
		Priority string `json:"priority"`
		Tags     []string
		Comments []struct{ Text string }
	}
	json.Unmarshal([]byte(out), &tk)
	if tk.Title != "shipped it" || tk.Priority != "high" || len(tk.Comments) != 1 {
		t.Errorf("archived task lost detail: %+v", tk)
	}

	// Archiving again appends rather than replacing.
	id2 := addOne(t, path, "shipped again")
	if _, err := run(t, "--file", path, "done", id2); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "archive", "--to", dest, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, "--file", dest, "list")
	if !strings.Contains(out, "shipped it") || !strings.Contains(out, "shipped again") {
		t.Errorf("second archive should append:\n%s", out)
	}
}

func TestArchiveEdgeCases(t *testing.T) {
	path := testFile(t)
	// Empty board: nothing to do, and not an error.
	out, err := run(t, "--file", path, "archive", "--yes")
	if err != nil || !strings.Contains(out, "nothing to archive") {
		t.Errorf("empty Done: %v %q", err, out)
	}
	// Unknown board is an error, not a silent no-op.
	if _, err := run(t, "--file", path, "archive", "--board", "Nope", "--yes"); err == nil {
		t.Error("unknown board should error")
	}
	// Bulk deletion never proceeds unattended without --yes — even when the
	// file is committed and everything would be recoverable.
	doneTasks(t, path, 1)
	gitRepo(t, filepath.Dir(path), path, true)
	if _, err := run(t, "--file", path, "archive"); err == nil ||
		!strings.Contains(err.Error(), "--yes") {
		t.Errorf("non-interactive archive without --yes should refuse, got %v", err)
	}
}

func TestBoardsDelete(t *testing.T) {
	path := testFile(t)
	addOne(t, path, "keep me")

	// Empty board: no ceremony.
	out, err := run(t, "--file", path, "boards", "delete", "In Progress")
	if err != nil {
		t.Fatalf("empty board should delete without --force: %v", err)
	}
	if !strings.Contains(out, "deleted empty board") {
		t.Errorf("out = %q", out)
	}

	// Non-empty: refuses, and the tasks stay.
	_, err = run(t, "--file", path, "boards", "delete", "Backlog")
	if err == nil || !strings.Contains(err.Error(), "still holds 1 task") {
		t.Fatalf("want a refusal naming the cost, got %v", err)
	}
	out, _ = run(t, "--file", path, "list")
	if !strings.Contains(out, "keep me") {
		t.Error("a refused board delete must leave its tasks alone")
	}

	// --force takes the board and its tasks, and reports them.
	out, err = run(t, "--file", path, "boards", "delete", "backlog", "--force", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Board string `json:"board"`
		Tasks []struct {
			Title string `json:"title"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Board != "Backlog" || len(res.Tasks) != 1 || res.Tasks[0].Title != "keep me" {
		t.Errorf("result = %+v", res)
	}

	// Bare `boards` still lists, and unknown boards error.
	out, err = run(t, "--file", path, "boards")
	if err != nil || !strings.Contains(out, "Done") {
		t.Errorf("boards listing broke: %v %q", err, out)
	}
	if _, err := run(t, "--file", path, "boards", "delete", "Nope", "--force"); err == nil {
		t.Error("unknown board should error")
	}
}

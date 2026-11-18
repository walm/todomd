package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/changes"
)

type filtered struct {
	Filter string `json:"filter"`
	Events []struct {
		Type string `json:"type"`
		Task string `json:"task"`
	} `json:"events"`
}

func (f filtered) list() []string {
	var out []string
	for _, e := range f.Events {
		out = append(out, e.Type+":"+e.Task)
	}
	return out
}

func changesAs(t *testing.T, path, as string, args ...string) filtered {
	t.Helper()
	out, err := run(t, append([]string{"--file", path, "changes", "--as", as, "--json"}, args...)...)
	if err != nil {
		t.Fatalf("changes %v: %v\n%s", args, err, out)
	}
	var f filtered
	if err := json.Unmarshal([]byte(out), &f); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return f
}

func must(t *testing.T, path string, args ...string) {
	t.Helper()
	if out, err := run(t, append([]string{"--file", path}, args...)...); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func same(got, want []string) bool { return strings.Join(got, ",") == strings.Join(want, ",") }

func TestChangesFilterEnterLeave(t *testing.T) {
	path := testFile(t)
	mine := addOne(t, path, "iOS card", "--tag", "ios")
	other := addOne(t, path, "Android card", "--tag", "android")
	changesAs(t, path, "ios") // init the cursor

	must(t, path, "update", other, "--tag", "ios")        // enters
	must(t, path, "update", mine, "--tag", "backend")     // leaves
	added := addOne(t, path, "New iOS", "--tag", "ios")   // task_added with the tag
	addOne(t, path, "Unrelated", "--tag", "web")          // dropped
	must(t, path, "update", added, "--title", "New iOS!") // updated, matches
	got := changesAs(t, path, "ios", "--filter", "tag:ios", "--peek")
	// File order: mine, other (both updates), then the new card, once.
	want := []string{"task_updated:" + mine, "task_updated:" + other, "task_added:" + added}
	if !same(got.list(), want) {
		t.Errorf("events = %v, want %v", got.list(), want)
	}
	for _, e := range got.Events {
		if strings.HasPrefix(e.Type, "task_") && e.Task != other && e.Task != mine && e.Task != added {
			t.Errorf("unrelated card reported: %+v", e)
		}
	}
}

func TestChangesFilterLeaveByMoveAndAssign(t *testing.T) {
	path := testFile(t)
	a := addOne(t, path, "A", "--assignee", "ios")
	b := addOne(t, path, "B", "--assignee", "anna")
	changesAs(t, path, "ios")
	must(t, path, "assign", a, "anna") // handed over: leaves assignee:ios
	must(t, path, "assign", b, "ios")  // handed in
	got := changesAs(t, path, "ios", "--assignee", "ios", "--peek")
	if !same(got.list(), []string{"task_updated:" + a, "task_updated:" + b}) {
		t.Errorf("events = %v", got.list())
	}
	// board:
	changesAs(t, path, "ios") // consume
	must(t, path, "move", b, "--to", "Doing")
	if got := changesAs(t, path, "ios", "--filter", "assignee:ios board:Backlog", "--peek"); len(got.Events) != 1 || got.Events[0].Type != "task_moved" {
		t.Errorf("move off the board: %v", got.list())
	}
	if got := changesAs(t, path, "ios", "--filter", "assignee:anna", "--peek"); len(got.Events) != 0 {
		t.Errorf("a card that never matched: %v", got.list())
	}
}

func TestChangesFilterMentionPassThrough(t *testing.T) {
	path := testFile(t)
	x := addOne(t, path, "Backend thing", "--tag", "backend")
	changesAs(t, path, "ios")
	must(t, path, "comment", x, "hey @ios can you look?", "--author", "anna")
	must(t, path, "comment", x, "just chatting", "--author", "anna")
	got := changesAs(t, path, "ios", "--tag", "ios", "--peek")
	if !same(got.list(), []string{"comment_added:" + x}) {
		t.Errorf("only the @ios comment should come through: %v", got.list())
	}
	// ...unless the filter says otherwise.
	if got := changesAs(t, path, "ios", "--filter", "tag:ios -mentions:me", "--peek"); len(got.Events) != 0 {
		t.Errorf("-mentions:me should switch the pass-through off: %v", got.list())
	}
	// A description that newly mentions me counts too.
	must(t, path, "update", x, "--desc", "for @ios")
	got = changesAs(t, path, "ios", "--tag", "ios", "--peek")
	if len(got.Events) != 2 {
		t.Errorf("events = %v", got.list())
	}
}

func TestChangesFilterConsumed(t *testing.T) {
	path := testFile(t)
	x := addOne(t, path, "Web", "--tag", "web")
	changesAs(t, path, "ios")
	must(t, path, "update", x, "--title", "Web 2")
	got := changesAs(t, path, "ios", "--tag", "ios")
	if len(got.Events) != 0 {
		t.Fatalf("dropped event reported: %v", got.list())
	}
	// The cursor moved past it: with no filter nothing is left to report.
	if got := changesAs(t, path, "ios"); len(got.Events) != 0 {
		t.Errorf("filtered events piled up: %v", got.list())
	}
	cpath, _ := changes.CursorPath(path, "ios")
	snap, _ := os.ReadFile(cpath)
	cur, _ := os.ReadFile(path)
	if string(snap) != string(cur) {
		t.Error("cursor is not at the current file")
	}
}

func TestSavedFilter(t *testing.T) {
	path := testFile(t)
	a := addOne(t, path, "A", "--tag", "ios")
	b := addOne(t, path, "B", "--tag", "web")
	changesAs(t, path, "ios")
	changesAs(t, path, "ios", "--tag", "ios", "--assignee", "ios", "--save")
	cpath, _ := changes.CursorPath(path, "ios")
	if b, _ := os.ReadFile(filterPath(cpath)); string(b) != "tag:ios OR assignee:ios\n" {
		t.Errorf("saved = %q", b)
	}
	must(t, path, "update", a, "--title", "A2")
	must(t, path, "update", b, "--title", "B2")
	got := changesAs(t, path, "ios", "--peek") // the saved filter applies
	if !same(got.list(), []string{"task_updated:" + a}) || got.Filter != "tag:ios OR assignee:ios" {
		t.Errorf("saved filter: %v %q", got.list(), got.Filter)
	}
	// An explicit filter overrides for the run; "" turns it off.
	if got := changesAs(t, path, "ios", "--filter", "tag:web", "--peek"); !same(got.list(), []string{"task_updated:" + b}) {
		t.Errorf("override: %v", got.list())
	}
	if got := changesAs(t, path, "ios", "--filter", "", "--peek"); len(got.Events) != 2 {
		t.Errorf("--filter '': %v", got.list())
	}
	// Another cursor is unaffected.
	changesAs(t, path, "other")
	// show and clear
	out, _ := run(t, "--file", path, "changes", "--as", "ios", "--show-filter")
	if strings.TrimSpace(out) != "tag:ios OR assignee:ios" {
		t.Errorf("show: %q", out)
	}
	if out, err := run(t, "--file", path, "changes", "--as", "ios", "--clear-filter"); err != nil || !strings.Contains(out, "cleared") {
		t.Errorf("clear: %q %v", out, err)
	}
	out, _ = run(t, "--file", path, "changes", "--as", "ios", "--show-filter")
	if !strings.Contains(out, "no saved filter") {
		t.Errorf("show after clear: %q", out)
	}
	if got := changesAs(t, path, "ios", "--peek"); len(got.Events) != 2 {
		t.Errorf("after clear: %v", got.list())
	}
	// --save needs a filter; a bad one is rejected.
	if _, err := run(t, "--file", path, "changes", "--as", "ios", "--save"); err == nil {
		t.Error("--save without a filter should fail")
	}
	if _, err := run(t, "--file", path, "changes", "--as", "ios", "--filter", "tag:a OR"); err == nil {
		t.Error("a bad filter should fail")
	}
}

func TestWatchFilterAndIgnoreFlags(t *testing.T) {
	path := testFile(t)
	a := addOne(t, path, "A", "--tag", "ios")
	b := addOne(t, path, "B", "--tag", "web")
	runWatchJSON(t, path, "--tag", "ios", "--save", "--no-wait") // init
	must(t, path, "update", a, "--title", "A2")
	must(t, path, "update", b, "--title", "B2")
	must(t, path, "comment", a, "noise", "--author", "bot")
	w := runWatchJSON(t, path, "--no-wait", "--ignore-author", "bot")
	if got := w.types(); len(got) != 1 || got[0] != "task_updated" {
		t.Errorf("watch events = %v", got)
	}
	if w := runWatchJSON(t, path, "--no-wait"); len(w.Events) != 0 {
		t.Errorf("events piled up: %v", w.types())
	}
}

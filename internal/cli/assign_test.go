package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonAssignee(t *testing.T, path, id string) *string {
	t.Helper()
	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(out), &tk); err != nil || !strings.Contains(out, `"assignee":`) {
		t.Fatalf("%v\n%s", err, out)
	}
	return tk.Assignee
}

func listIDs(t *testing.T, path string, args ...string) []string {
	t.Helper()
	out, err := run(t, append([]string{"--file", path, "list", "--json"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Boards []struct {
			Tasks []struct {
				ID string `json:"id"`
			} `json:"tasks"`
		} `json:"boards"`
	}
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var ids []string
	for _, b := range l.Boards {
		for _, x := range b.Tasks {
			ids = append(ids, x.ID)
		}
	}
	return ids
}

func TestAssignCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // not logged in
	t.Setenv("TODOMD_CURSOR", "")
	path := testFile(t)
	a := addOne(t, path, "For anna", "--assignee", "@Anna")
	b := addOne(t, path, "For the agent")
	c := addOne(t, path, "Nobody's")
	if h := jsonAssignee(t, path, a); h == nil || *h != "anna" {
		t.Errorf("a: %v", h)
	}
	if h := jsonAssignee(t, path, c); h != nil {
		t.Errorf("c: %v", *h)
	}

	out, err := run(t, "--file", path, "assign", b, "me", "--as", "agent")
	if err != nil || !strings.Contains(out, "assigned "+b+" to @agent") {
		t.Fatalf("%v %s", err, out)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "\n\n<!-- assignee: agent -->\n") || !strings.Contains(string(data), "\n\n<!-- assignee: anna -->\n") {
		t.Errorf("file:\n%s", data)
	}
	if out, err := run(t, "--file", path, "assign", a, "bob"); err != nil || !strings.Contains(out, "(was @anna)") {
		t.Errorf("reassign: %v %s", err, out)
	}
	if out, err := run(t, "--file", path, "assign", a, "@bob"); err != nil || !strings.Contains(out, "already assigned to @bob") {
		t.Errorf("same again: %v %s", err, out)
	}
	if _, err := run(t, "--file", path, "assign", a, "Not_A_Handle"); err == nil || !strings.Contains(err.Error(), "invalid handle") {
		t.Errorf("bad handle: %v", err)
	}
	if _, err := run(t, "--file", path, "assign", a, "me"); err == nil || !strings.Contains(err.Error(), "who is 'me'") {
		t.Errorf("me without --as or login: %v", err)
	}
	if _, err := run(t, "--file", path, "assign", a, "me", "--as", "my_agent"); err == nil || !strings.Contains(err.Error(), "isn't a valid handle") {
		t.Errorf("me as an invalid --as name: %v", err)
	}
	// @me is the handle "me".
	if out, err := run(t, "--file", path, "assign", c, "@me"); err != nil || !strings.Contains(out, "to @me") {
		t.Errorf("@me: %v %s", err, out)
	}
	if _, err := run(t, "--file", path, "update", c, "--assignee", "none"); err != nil {
		t.Fatal(err)
	}
	if h := jsonAssignee(t, path, c); h != nil {
		t.Errorf("c after update --assignee none: %v", *h)
	}

	// list --assignee.
	if ids := listIDs(t, path, "--assignee", "me", "--as", "agent"); len(ids) != 1 || ids[0] != b {
		t.Errorf("me: %v", ids)
	}
	if ids := listIDs(t, path, "--assignee", "BOB"); len(ids) != 1 || ids[0] != a {
		t.Errorf("bob: %v", ids)
	}
	if ids := listIDs(t, path, "--assignee", "none"); len(ids) != 1 || ids[0] != c {
		t.Errorf("none: %v", ids)
	}
	if ids := listIDs(t, path, "--assignee", "any"); len(ids) != 2 {
		t.Errorf("any: %v", ids)
	}
	if out, _ := run(t, "--file", path, "list"); !strings.Contains(out, "@bob") || !strings.Contains(out, "@agent") {
		t.Errorf("list rows:\n%s", out)
	}
	if out, _ := run(t, "--file", path, "show", a); !strings.Contains(out, "assignee: @bob") {
		t.Errorf("show:\n%s", out)
	}

	// unassign, guarded by a handle.
	if out, err := run(t, "--file", path, "unassign", a, "me", "--as", "agent"); err != nil || !strings.Contains(out, "assigned to @bob, not @agent; left as is") {
		t.Errorf("guarded: %v %s", err, out)
	}
	if h := jsonAssignee(t, path, a); h == nil || *h != "bob" {
		t.Errorf("a after guarded unassign: %v", h)
	}
	if out, err := run(t, "--file", path, "unassign", b, "agent"); err != nil || !strings.Contains(out, "unassigned "+b+" (was @agent)") {
		t.Errorf("own: %v %s", err, out)
	}
	if out, err := run(t, "--file", path, "unassign", a); err != nil || !strings.Contains(out, "(was @bob)") {
		t.Errorf("plain: %v %s", err, out)
	}
	if out, err := run(t, "--file", path, "unassign", a); err != nil || !strings.Contains(out, "wasn't assigned") {
		t.Errorf("again: %v %s", err, out)
	}
	if ids := listIDs(t, path, "--assignee", "any"); len(ids) != 0 {
		t.Errorf("any after unassigning: %v", ids)
	}
}

// 'me' falls back to the handle this machine is logged in as.
func TestAssignMeIsTheLoggedInHandle(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("TODOMD_CURSOR", "")
	t.Setenv("TODOMD_HOST", "")
	if err := os.MkdirAll(filepath.Join(cfg, "todomd"), 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := `{"hosts":{"https://todomd.online":{"api":"https://todomd.online/api","token":"x","user":{"id":"u1","handle":"walm"}}}}`
	if err := os.WriteFile(filepath.Join(cfg, "todomd", "hosts.json"), []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	path := testFile(t)
	id := addOne(t, path, "Mine", "--assignee", "me")
	if h := jsonAssignee(t, path, id); h == nil || *h != "walm" {
		t.Errorf("assignee: %v", h)
	}
	if ids := listIDs(t, path, "--assignee", "me"); len(ids) != 1 {
		t.Errorf("me: %v", ids)
	}
	// --as wins over the login.
	if ids := listIDs(t, path, "--assignee", "me", "--as", "agent"); len(ids) != 0 {
		t.Errorf("me as agent: %v", ids)
	}
}

func TestChangesAssignees(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := testFile(t)
	mine := addOne(t, path, "Agent's", "--assignee", "bot")
	theirs := addOne(t, path, "Anna's", "--assignee", "anna")
	open := addOne(t, path, "Open")
	away := addOne(t, path, "Taken away", "--assignee", "bot")
	if _, err := run(t, "--file", path, "changes", "--as", "bot"); err != nil { // start the cursor
		t.Fatal(err)
	}
	steps := [][]string{
		{"comment", mine, "--author", "walm", "On yours"},
		{"comment", theirs, "--author", "walm", "On anna's"},
		{"comment", theirs, "--author", "walm", "@bot can you help anna?"},
		{"comment", open, "--author", "walm", "On the open one"},
		{"assign", away, "anna"},
		{"assign", open, "carl"},
		{"update", theirs, "--priority", "high"},
	}
	for _, s := range steps {
		if _, err := run(t, append([]string{"--file", path}, s...)...); err != nil {
			t.Fatal(err)
		}
	}
	type event struct {
		Type     string `json:"type"`
		Task     string `json:"task"`
		Assignee string `json:"assignee"`
		Fields   map[string]struct {
			Old any `json:"old"`
			New any `json:"new"`
		} `json:"fields"`
		Comment *struct {
			Text string `json:"text"`
		} `json:"comment"`
	}
	read := func(args ...string) []event {
		t.Helper()
		out, err := run(t, append([]string{"--file", path, "changes", "--as", "bot", "--json", "--peek"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			Events []event `json:"events"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return r.Events
	}
	all := read()
	if len(all) != 7 {
		t.Fatalf("want 7 events, got %+v", all)
	}
	var reassigned *event
	for i, e := range all {
		if e.Task == away && e.Type == "task_updated" {
			reassigned = &all[i]
		}
	}
	if reassigned == nil || reassigned.Assignee != "anna" || reassigned.Fields["assignee"].Old != "bot" || reassigned.Fields["assignee"].New != "anna" {
		t.Errorf("reassign event: %+v", reassigned)
	}

	kept := read("--ignore-assigned-to-others", "@Bot")
	var got []string
	for _, e := range kept {
		text := e.Type
		if e.Comment != nil {
			text += ":" + e.Comment.Text
		}
		got = append(got, e.Task+" "+text)
	}
	want := []string{
		mine + " comment_added:On yours",
		theirs + " comment_added:@bot can you help anna?",
		// open's comment is gone: the card is carl's by the time changes
		// reads it, and the filter goes by the card as it is now.
		away + " task_updated",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("kept:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if out, err := run(t, "--file", path, "watch", "--help"); err != nil || !strings.Contains(out, "--ignore-assigned-to-others") {
		t.Errorf("watch help: %v", err)
	}
}

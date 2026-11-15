package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloudsync"
)

func TestProjectChoicesGroupByOwner(t *testing.T) {
	ws := func(id, h, name, proj string) cloud.Project {
		return cloud.Project{ID: id, Slug: h + "/" + proj, Owner: h, OwnerKind: "workspace", OwnerName: name}
	}
	user := func(id, h, proj string) cloud.Project {
		return cloud.Project{ID: id, Slug: h + "/" + proj, Owner: h, OwnerKind: "user"}
	}
	all := []cloud.Project{
		user("1", "anna", "shared-one"),
		ws("2", "zed", "Zed Co", "api"),
		user("3", "walm", "notes"),
		ws("4", "acme", "Acme", "website"),
		ws("5", "acme", "Acme", "docs"),
		{ID: "6", Slug: "walm/old-server"}, // an older server: no owner fields
	}
	options, picks, headings := projectChoices(all, []cloud.Project{all[3]}, "walm")
	want := []string{
		"acme/website  (this repo)", "Create a new project",
		"walm/notes", "walm/old-server", // Personal
		"acme/docs",       // Acme (website is already above)
		"zed/api",         // Zed Co
		"anna/shared-one", // shared with you
	}
	if strings.Join(options, "|") != strings.Join(want, "|") {
		t.Fatalf("options:\n%q\nwant\n%q", options, want)
	}
	if picks[1] != nil || picks[0].ID != "4" {
		t.Errorf("picks: %v", picks)
	}
	wantHeads := map[int]string{2: "Personal", 4: "Acme", 5: "Zed Co", 6: "shared with you"}
	if len(headings) != len(wantHeads) {
		t.Fatalf("headings %v", headings)
	}
	for i, h := range wantHeads {
		if headings[i] != h {
			t.Errorf("heading %d = %q, want %q", i, headings[i], h)
		}
	}
}

func TestLinkCreateInWorkspace(t *testing.T) {
	srv, path := cloudEnv(t)
	srv.Workspaces = map[string]string{"acme": "Acme"}
	srv.Denied = map[string]string{
		"locked": "only admins can make projects in Locked",
		"full":   "402:Free workspaces have 3 projects",
	}
	login(t)
	addOne(t, path, "First task")

	// Refusals say what the server said, and change nothing.
	for _, c := range []struct{ owner, want string }{
		{"locked", "only admins can make projects in Locked"},
		{"full", "Free workspaces have 3 projects"},
		{"nope", "check the --owner handle"},
	} {
		out, err := run(t, "--file", path, "link", "--create", "--name", "web", "--owner", c.owner)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("--owner %s: %v\n%s", c.owner, err, out)
		}
		if _, err := cloudsync.LoadLink(path); err == nil {
			t.Fatalf("--owner %s left a link", c.owner)
		}
	}
	if _, err := run(t, "--file", path, "link", "--project", "acme/x", "--owner", "acme"); err == nil {
		t.Error("--owner with --project should be refused")
	}

	out, err := run(t, "--file", path, "link", "--create", "--name", "website", "--owner", "@acme", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var linked struct {
		Project struct {
			Slug, Owner string
			OwnerName   string `json:"owner_name"`
		}
	}
	if err := json.Unmarshal([]byte(out), &linked); err != nil || linked.Project.Slug != "acme/website" ||
		linked.Project.Owner != "acme" || linked.Project.OwnerName != "Acme" {
		t.Fatalf("link output %s (%v)", out, err)
	}
	st, err := run(t, "--file", path, "sync", "status")
	if err != nil || !strings.Contains(st, "acme/website (workspace Acme)") {
		t.Errorf("status: %v\n%s", err, st)
	}
}

func TestSyncFollowsAMovedProject(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	addOne(t, path, "Task")
	out, err := run(t, "--file", path, "link", "--create", "--name", "site", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var linked struct{ Project struct{ ID string } }
	_ = json.Unmarshal([]byte(out), &linked)

	if out, err = run(t, "--file", path, "sync"); err != nil || strings.Contains(out, "moved") {
		t.Fatalf("unmoved sync: %v\n%s", err, out)
	}

	srv.Move(linked.Project.ID, "acme", "Acme", "website")
	out, err = run(t, "--file", path, "sync")
	if err != nil || !strings.Contains(out, "project moved to acme/website") {
		t.Fatalf("moved sync: %v\n%s", err, out)
	}
	l, err := cloudsync.LoadLink(path)
	if err != nil || l.ProjectSlug != "acme/website" || l.Owner != "acme" || l.OwnerName != "Acme" ||
		!strings.HasSuffix(l.ProjectURL, "/acme/website") {
		t.Fatalf("link after move: %+v (%v)", l, err)
	}
	// Said once.
	if out, err = run(t, "--file", path, "sync"); err != nil || strings.Contains(out, "moved") {
		t.Errorf("second sync: %v\n%s", err, out)
	}

	// A plain rename shows in --json too.
	srv.Move(linked.Project.ID, "acme", "Acme", "site2")
	out, err = run(t, "--file", path, "sync", "--json")
	var res syncJSON
	if err != nil || json.Unmarshal([]byte(out), &res) != nil || res.Moved != "acme/site2" {
		t.Errorf("json: %v\n%s", err, out)
	}
}

func TestLinkByOldAddressAfterMove(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	p := srv.AddProject("old", "# TODO\n\n## Backlog\n\n### A\n<!-- id:aaaa -->\n")
	srv.Move(p.ID, "acme", "Acme", "new")
	if out, err := run(t, "--file", path, "link", "--project", "walm/old", "--yes"); err != nil ||
		!strings.Contains(out, "Linked") || !strings.Contains(out, "acme/new (workspace Acme)") {
		t.Fatalf("%v\n%s", err, out)
	}
	if l, _ := cloudsync.LoadLink(path); l == nil || l.ProjectSlug != "acme/new" {
		t.Errorf("link %+v", l)
	}
}

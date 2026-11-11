package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type linkOut struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Title    *string `json:"title"`
	Board    *string `json:"board"`
	StoredOn string  `json:"stored_on"`
}

func showLinks(t *testing.T, path, id string) []linkOut {
	t.Helper()
	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Links []linkOut `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &tk); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return tk.Links
}

func TestSplitAndLinks(t *testing.T) {
	path := testFile(t)
	parent := addOne(t, path, "Offline mode", "--board", "In Progress", "--tag", "sync")
	out, err := run(t, "--file", path, "split", parent, "Queue writes", "Replay on reconnect", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var kids []struct {
		ID    string    `json:"id"`
		Board string    `json:"board"`
		Tags  []string  `json:"tags"`
		Links []linkOut `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &kids); err != nil || len(kids) != 2 {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, k := range kids {
		if k.Board != "In Progress" || len(k.Tags) != 1 || k.Tags[0] != "sync" ||
			len(k.Links) != 1 || k.Links[0].Kind != "split from" || k.Links[0].ID != parent || k.Links[0].StoredOn != k.ID {
			t.Errorf("child %+v", k)
		}
	}
	a, b := kids[0].ID, kids[1].ID

	// The parent isn't changed, and sees its children from its side.
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "#### Links") != 2 || strings.Count(string(data), "- split from "+parent) != 2 {
		t.Errorf("file:\n%s", data)
	}
	pl := showLinks(t, path, parent)
	if len(pl) != 2 || pl[0].Kind != "split into" || pl[0].ID != a || pl[0].StoredOn != a || *pl[0].Title != "Queue writes" {
		t.Errorf("parent links %+v", pl)
	}

	// Text output names the board and the parent.
	out, err = run(t, "--file", path, "split", parent, "Third", "--board", "Backlog")
	if err != nil || !strings.Contains(out, "Third") || !strings.Contains(out, "Backlog · split from "+parent) {
		t.Errorf("%v\n%s", err, out)
	}

	// links add, idempotent across both ends.
	out, err = run(t, "--file", path, "links", "add", a, "blocks", b)
	if err != nil || out != "linked "+a+" blocks "+b+" \"Replay on reconnect\"\n" {
		t.Errorf("%v %q", err, out)
	}
	out, err = run(t, "--file", path, "links", "add", b, "blocked", "by", a)
	if err != nil || !strings.HasPrefix(out, "already linked: "+b+" blocked by "+a) {
		t.Errorf("%v %q", err, out)
	}
	bl := showLinks(t, path, b)
	if len(bl) != 2 || bl[0].Kind != "blocked by" || bl[0].ID != a || bl[0].StoredOn != a {
		t.Errorf("b links %+v", bl)
	}

	// show prints a links block.
	out, _ = run(t, "--file", path, "show", b)
	if !strings.Contains(out, "links:  blocked by  "+a+"  Queue writes") || !strings.Contains(out, "        split from  "+parent+"  Offline mode") {
		t.Errorf("show:\n%s", out)
	}

	// list --json carries links too.
	out, _ = run(t, "--file", path, "list", "--json")
	if !strings.Contains(out, `"stored_on": "`+a+`"`) {
		t.Errorf("list --json lacks links:\n%s", out)
	}

	// links <id>
	out, err = run(t, "--file", path, "links", parent)
	if err != nil || strings.Count(out, "split into") != 3 {
		t.Errorf("%v\n%s", err, out)
	}

	// Errors: missing target exits 2, self-link, bad kind.
	if _, err := run(t, "--file", path, "links", "add", a, "related", "zzzz"); classify(err) != 2 {
		t.Errorf("missing target: %v", err)
	}
	if _, err := run(t, "--file", path, "links", "add", a, "related", a); err == nil || classify(err) != 1 {
		t.Errorf("self-link: %v", err)
	}
	if _, err := run(t, "--file", path, "links", "add", a, "relates", b); err == nil {
		t.Error("bad kind accepted")
	}

	// rm from the other end, with a kind read from the first card's side.
	if _, err := run(t, "--file", path, "links", "rm", b, a, "--kind", "blocks"); classify(err) != 2 {
		t.Errorf("wrong-direction kind: %v", err)
	}
	out, err = run(t, "--file", path, "links", "rm", b, a, "--kind", "blocked-by", "--json")
	if err != nil || !strings.Contains(out, `"stored_on": "`+a+`"`) || !strings.Contains(out, `"kind": "blocks"`) {
		t.Errorf("%v\n%s", err, out)
	}
	if _, err := run(t, "--file", path, "links", "rm", b, a); classify(err) != 2 {
		t.Errorf("rm again: %v", err)
	}
}

func TestAddWithLinks(t *testing.T) {
	path := testFile(t)
	p := addOne(t, path, "Parent")
	out, err := run(t, "--file", path, "add", "Child", "--link", "split-from:"+p, "--link", "Related:"+p, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var tk struct {
		Links []linkOut `json:"links"`
	}
	json.Unmarshal([]byte(out), &tk)
	if len(tk.Links) != 2 || tk.Links[0].Kind != "split from" || tk.Links[1].Kind != "related" {
		t.Errorf("%s", out)
	}
	if _, err := run(t, "--file", path, "add", "X", "--link", "split-from:zzzz"); classify(err) != 2 {
		t.Errorf("missing target: %v", err)
	}
	if _, err := run(t, "--file", path, "add", "X", "--link", "split-from"); err == nil {
		t.Error("link without id accepted")
	}
}

func TestLinksToCardsNotOnTheBoard(t *testing.T) {
	path := testFile(t)
	a := addOne(t, path, "A")
	data, _ := os.ReadFile(path)
	text := strings.Replace(string(data), "<!-- id:"+a+" -->\n", "<!-- id:"+a+" -->\n\n#### Links\n\n- related k2pq\n", 1)
	os.WriteFile(path, []byte(text), 0o644)
	l := showLinks(t, path, a)
	if len(l) != 1 || l[0].ID != "k2pq" || l[0].Title != nil || l[0].Board != nil {
		t.Errorf("%+v", l)
	}
	out, _ := run(t, "--file", path, "show", a)
	if !strings.Contains(out, "links:  related  k2pq  (not on the board)") {
		t.Errorf("show:\n%s", out)
	}
	// rm works with the archived card's id.
	if _, err := run(t, "--file", path, "links", "rm", "k2pq", a); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "#### Links") {
		t.Errorf("section left behind:\n%s", data)
	}
}

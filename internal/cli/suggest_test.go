package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCommentSuggest(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Task A")
	if _, err := run(t, "--file", path, "comment", id, "--author", "bot", "Ship it?",
		"--suggest", "GO", "--suggest", "push and merge"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Comments []struct {
			Text        string   `json:"text"`
			Suggestions []string `json:"suggestions"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	c := got.Comments[0]
	if c.Text != "Ship it?\n<!-- suggest: GO | push and merge -->" {
		t.Errorf("text %q", c.Text)
	}
	if !reflect.DeepEqual(c.Suggestions, []string{"GO", "push and merge"}) {
		t.Errorf("suggestions %q", c.Suggestions)
	}
	txt, err := run(t, "--file", path, "show", id)
	if err != nil || !strings.Contains(txt, "→ reply: GO · push and merge") {
		t.Errorf("show text: %v\n%s", err, txt)
	}
	if strings.Contains(txt, "<!--") || !strings.Contains(txt, "Ship it?") {
		t.Errorf("show text should hide the raw suggest line:\n%s", txt)
	}
	// a plain comment has [] and no reply line
	if _, err := run(t, "--file", path, "comment", id, "--author", "bot", "thanks"); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, "--file", path, "show", id, "--json")
	if !strings.Contains(out, `"suggestions": []`) {
		t.Errorf("want empty suggestions: %s", out)
	}
	txt, _ = run(t, "--file", path, "show", id)
	if strings.Contains(txt, "→ reply") {
		t.Errorf("reply line on a non-suggesting last comment:\n%s", txt)
	}
	for _, bad := range []string{"a|b", "x-->", strings.Repeat("y", 41)} {
		if _, err := run(t, "--file", path, "comment", id, "--author", "bot", "q", "--suggest", bad); err == nil {
			t.Errorf("--suggest %q should fail", bad)
		}
	}
}

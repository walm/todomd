package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type reactedCard struct {
	Comments []struct {
		Text      string `json:"text"`
		Reactions []struct {
			Emoji string   `json:"emoji"`
			By    []string `json:"by"`
		} `json:"reactions"`
	} `json:"comments"`
}

func showReacted(t *testing.T, path, id string) (reactedCard, string) {
	t.Helper()
	out, err := run(t, "--file", path, "show", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var c reactedCard
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return c, out
}

func TestReactCommand(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Card")
	for _, text := range []string{"Can you check the export?", "On it."} {
		if _, err := run(t, "--file", path, "comment", id, "--author", "anna", text); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"1", "👍", "--author", "walm"}, "walm reacted 👍 to comment 1 on " + id + ": Card"},
		{[]string{"1", "👍", "--author", "walm"}, "walm already reacted 👍"},
		{[]string{"1", "👍", "--author", "Anna"}, "anna reacted 👍 to comment 1"},
		{[]string{"last", "👀", "--author", "agent", "--match", "on  IT"}, "agent reacted 👀 to comment 2"},
		{[]string{"2", "❤", "--author", "walm"}, "walm reacted ❤️ to comment 2"},
		{[]string{"#2", "❤️", "--author", "walm", "--remove"}, "removed walm's ❤️ from comment 2"},
		{[]string{"2", "🎉", "--author", "walm", "--remove"}, "walm hadn't reacted 🎉 to comment 2"},
	}
	for _, s := range steps {
		out, err := run(t, append([]string{"--file", path, "react", id}, s.args...)...)
		if err != nil || !strings.Contains(out, s.want) {
			t.Fatalf("%v: %v %q, want %q", s.args, err, out, s.want)
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "- **anna** (") || !strings.Contains(string(data), "Can you check the export?\n  <!-- reactions: 👍 walm,anna -->\n") ||
		!strings.Contains(string(data), "On it.\n  <!-- reactions: 👀 agent -->\n") {
		t.Errorf("file:\n%s", data)
	}
	c, raw := showReacted(t, path, id)
	if len(c.Comments) != 2 || len(c.Comments[0].Reactions) != 1 || strings.Join(c.Comments[0].Reactions[0].By, ",") != "walm,anna" ||
		c.Comments[1].Reactions[0].Emoji != "👀" {
		t.Errorf("json: %s", raw)
	}
	if out, _ := run(t, "--file", path, "show", id); !strings.Contains(out, "  1. anna (") || !strings.Contains(out, "\n     👍 walm, anna\n") {
		t.Errorf("show:\n%s", out)
	}
	if out, _ := run(t, "--file", path, "react", id, "1"); strings.TrimSpace(out) != "👍 walm, anna" {
		t.Errorf("print: %q", out)
	}
	if out, _ := run(t, "--file", path, "list", "--json"); !strings.Contains(out, `"reactions": [`) {
		t.Errorf("list --json lacks reactions:\n%s", out)
	}

	for _, bad := range []struct {
		args []string
		want string
	}{
		{[]string{"3", "👍", "--author", "walm"}, "no comment 3"},
		{[]string{"0", "👍", "--author", "walm"}, "no comment 0"},
		{[]string{"first", "👍", "--author", "walm"}, "want its number"},
		{[]string{"1", "👍", "--author", "walm", "--match", "on it"}, "doesn't contain"},
		{[]string{"1", "thumbsup", "--author", "walm"}, "not an emoji"},
		{[]string{"1", "👍"}, `"author"`},
		{[]string{"1", "👍", "--author", "Bob Smith"}, "invalid name"},
	} {
		if _, err := run(t, append([]string{"--file", path, "react", id}, bad.args...)...); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%v: %v, want %q", bad.args, err, bad.want)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(data) {
		t.Error("a refused reaction changed the file")
	}
}

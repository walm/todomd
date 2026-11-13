package cli

import (
	"encoding/json"
	"fmt"
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

// watch and changes report a change to reactions alone only when it is on
// a comment the watcher wrote (--author, else its --as name).
func TestWatchReportsReactionsOnOwnComments(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Card")
	for _, c := range [][]string{{"--author", "anna", "Can you check?"}, {"--author", "agent", "--as", "agent", "Plan: X then Y."}} {
		if _, err := run(t, append([]string{"--file", path, "comment", id}, c...)...); err != nil {
			t.Fatal(err)
		}
	}
	react := func(args ...string) {
		t.Helper()
		if _, err := run(t, append([]string{"--file", path, "react", id}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	changesFor := func(args ...string) string {
		t.Helper()
		out, err := run(t, append([]string{"--file", path, "changes", "--json"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	runWatchJSON(t, path, "--no-wait")
	changesFor("--as", "bot")
	changesFor("--as", "bot2")

	// walm reacts to anna's comment: not the agent's business.
	react("1", "👍", "--author", "walm")
	if w := runWatchJSON(t, path, "--no-wait"); len(w.Events) != 0 {
		t.Errorf("woke for a reaction on someone else's comment: %+v", w.Events)
	}
	// The agent reacting itself (--as agent) isn't reported back either.
	react("1", "👀", "--author", "agent", "--as", "agent")
	if w := runWatchJSON(t, path, "--no-wait"); len(w.Events) != 0 {
		t.Errorf("own reaction reported: %+v", w.Events)
	}
	// walm approves the agent's plan: that one counts.
	react("2", "✅", "--author", "walm")
	w := runWatchJSON(t, path, "--no-wait")
	if len(w.Events) != 1 || w.Events[0].Type != "task_updated" || !strings.Contains(fmt.Sprint(w.Events[0].Fields["reactions"]), "✅") {
		t.Fatalf("events = %+v", w.Events)
	}
	// changes: --author names whose comments are yours; without it, the --as name.
	if out := changesFor("--as", "bot"); strings.Contains(out, `"reactions": {`) {
		t.Errorf("bot saw reactions on comments it didn't write:\n%s", out)
	}
	if out := changesFor("--as", "bot2", "--author", "Agent"); !strings.Contains(out, `"reactions": {`) {
		t.Errorf("--author agent missed reactions on agent's comment:\n%s", out)
	}
	// Removing it counts too.
	react("2", "✅", "--author", "walm", "--remove")
	if w := runWatchJSON(t, path, "--no-wait"); len(w.Events) != 1 {
		t.Errorf("removal: %+v", w.Events)
	}
	// A change with more than reactions in it is always reported.
	react("1", "🎉", "--author", "walm")
	if _, err := run(t, "--file", path, "update", id, "--title", "Card!"); err != nil {
		t.Fatal(err)
	}
	if w := runWatchJSON(t, path, "--no-wait"); len(w.Events) != 1 || w.Events[0].Fields["title"] == nil {
		t.Errorf("mixed: %+v", w.Events)
	}

}

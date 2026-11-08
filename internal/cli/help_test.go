package cli

import (
	"regexp"
	"strings"
	"testing"
)

func TestHelpIsGrouped(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour in help that isn't going to a terminal")
	}
	// Each heading, in order, with a command under it.
	want := []string{"Tasks:\n  list ", "Boards:\n  boards ", "todomd.online:\n  login ",
		"Agents:\n  watch ", "Other:\n  init "}
	at := 0
	for _, w := range want {
		i := strings.Index(out[at:], w)
		if i < 0 {
			t.Fatalf("help lacks %q after offset %d:\n%s", w, at, out)
		}
		at += i
	}
	for _, name := range []string{"download", "skill", "changes", "completion", "help"} {
		if !regexp.MustCompile(`(?m)^  ` + name + ` `).MatchString(out) {
			t.Errorf("help lacks %s", name)
		}
	}
	if strings.Contains(out, "Additional Commands:") || strings.Contains(out, "canonicalize") {
		t.Errorf("ungrouped or hidden commands shown:\n%s", out)
	}
}

func TestHelpColour(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	if colorAllowed(false) || !colorAllowed(true) {
		t.Error("colour only on a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if colorAllowed(true) {
		t.Error("NO_COLOR must turn colour off")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorAllowed(true) {
		t.Error("TERM=dumb must turn colour off")
	}

	old := helpColor
	helpColor = func() bool { return true }
	t.Cleanup(func() { helpColor = old })
	out, err := run(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b[1mAgents:\x1b[0m") || !strings.Contains(out, "\x1b[36mwatch\x1b[0m ") {
		t.Errorf("no colour on a terminal:\n%q", out[:200])
	}
}

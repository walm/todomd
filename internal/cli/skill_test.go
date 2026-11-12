package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillPrints(t *testing.T) {
	out, err := run(t, "skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "---\nname: todomd\ndescription: ") {
		t.Errorf("no frontmatter:\n%.200s", out)
	}
	for _, want := range []string{
		"todomd list --json", "todomd show", "--author agent --as agent",
		"todomd move", "todomd add", "todomd update", "todomd watch --as agent --json",
		"Backlog", "GO", "Never delete cards", "todomd attach", "todomd download",
		"the user's to change", "todomd split", "todomd links add", "--link related:", "blocked by",
		"todomd sticker 3f2a fire", "only\nwhen the user asks",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
}

func TestSkillInstall(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out, err := run(t, "skill", "--install")
	path := filepath.Join(".claude", "skills", "todomd", "SKILL.md")
	if err != nil || !strings.Contains(out, path) {
		t.Fatalf("%v\n%s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != skillText {
		t.Fatalf("installed skill differs: %v", err)
	}

	// No overwrite without --force.
	os.WriteFile(path, []byte("mine"), 0o644)
	if _, err := run(t, "skill", "--install"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("overwrite without --force: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Error("the existing SKILL.md was changed")
	}
	if _, err := run(t, "skill", "--install", "--force"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != skillText {
		t.Error("--force didn't overwrite")
	}

	// --dir elsewhere; --dir without --install is a mistake.
	other := filepath.Join(dir, "skills", "todomd")
	if _, err := run(t, "skill", "--install", "--dir", other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "SKILL.md")); err != nil {
		t.Error(err)
	}
	if _, err := run(t, "skill", "--dir", other); err == nil {
		t.Error("--dir without --install should fail")
	}
}

package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFileState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")

	// Outside a repo there is no history to recover from.
	if err := os.WriteFile(path, []byte("# T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileState(path); got != NoRepo || got.Recoverable() {
		t.Errorf("outside a repo: %v", got)
	}

	git(t, dir, "init", "-q")
	if got := FileState(path); got != Untracked || got.Recoverable() {
		t.Errorf("never added: %v, want untracked", got)
	}

	git(t, dir, "add", "TODO.md")
	if got := FileState(path); got != Modified {
		t.Errorf("staged but not committed: %v, want modified", got)
	}

	git(t, dir, "commit", "-qm", "add todo")
	if got := FileState(path); got != Clean || !got.Recoverable() {
		t.Errorf("committed: %v, want clean", got)
	}

	if err := os.WriteFile(path, []byte("# T\n\n## B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileState(path); got != Modified || got.Recoverable() {
		t.Errorf("edited after commit: %v, want modified", got)
	}

	// A gitignored file reports clean in `git status --porcelain`; it must not
	// be mistaken for being in history.
	ignored := filepath.Join(dir, "IGNORED.md")
	if err := os.WriteFile(ignored, []byte("# T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("IGNORED.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileState(ignored); got != Untracked {
		t.Errorf("gitignored file: %v, want untracked", got)
	}
}

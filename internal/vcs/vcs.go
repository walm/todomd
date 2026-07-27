// Package vcs answers one question for destructive commands: is this file
// safely in git history, so that what we remove can be recovered?
package vcs

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// State describes how a file relates to git history.
type State int

const (
	// NoRepo: git isn't available, or the file isn't in a working tree.
	// Nothing we remove is recoverable.
	NoRepo State = iota
	// Untracked: inside a repo, but git has never stored this file (which
	// includes being gitignored), so there is no history to fall back on.
	Untracked
	// Modified: tracked, but with uncommitted changes — history exists, yet
	// it doesn't include what's on disk right now.
	Modified
	// Clean: tracked with no uncommitted changes, so the current contents are
	// in history and anything removed can be recovered from it.
	Clean
)

func (s State) String() string {
	switch s {
	case Untracked:
		return "untracked"
	case Modified:
		return "modified"
	case Clean:
		return "committed"
	}
	return "not in a git repository"
}

// Recoverable reports whether the file's current contents are in git history.
func (s State) Recoverable() bool { return s == Clean }

// FileState inspects path with the git CLI. Anything unexpected — no git
// binary, not a repo, a git error — reports NoRepo: callers treat that as
// "assume nothing is recoverable", which is the safe direction.
func FileState(path string) State {
	git, err := exec.LookPath("git")
	if err != nil {
		return NoRepo
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return NoRepo
	}
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := run("rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return NoRepo
	}
	// Tracked? An ignored or never-added file reports clean in `status
	// --porcelain`, so ask explicitly rather than inferring from silence.
	if _, err := run("ls-files", "--error-unmatch", "--", base); err != nil {
		return Untracked
	}
	out, err := run("status", "--porcelain", "--", base)
	if err != nil {
		return NoRepo
	}
	if out != "" {
		return Modified
	}
	return Clean
}

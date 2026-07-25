// Package statedir resolves the per-todo-file state directory:
// $XDG_STATE_HOME/todomd/<hash> (default ~/.local/state), where <hash> keys
// the todo file's absolute path — never its content. Lock files and change
// cursors live here so repositories stay free of sidecar files.
package statedir

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Global returns the state directory shared by every todo file (not created),
// for state that isn't about one particular file — the update check, say.
func Global() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "todomd"), nil
}

// For returns the state directory for the given todo file (not created).
func For(filePath string) (string, error) {
	base, err := Global()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(base, hex.EncodeToString(sum[:8])), nil
}

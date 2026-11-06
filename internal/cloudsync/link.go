// Package cloudsync keeps a local TODO.md and a todomd.online project in
// step: the link between them, sync rounds (push and pull in one, merged per
// task), the watcher loop, and how the file relates to git.
//
// Everything about the link lives in todomd's per-file state directory, next
// to the lock and the change cursors, never next to the TODO.md:
//
//	sync.json        the link: host, project, base revision, git mode, …
//	sync-base.md     the document at base_rev, in canonical form
//	sync.watch.lock  held by the one process watching this file
package cloudsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/walm/todomd/internal/statedir"
)

// ErrNotLinked: the file has no link to a project.
var ErrNotLinked = errors.New("this TODO.md isn't linked to a todomd.online project (run 'todomd link')")

// Git modes (spec §12).
const (
	GitIgnore = "ignore" // the file is no longer tracked; the service keeps history
	GitTrack  = "track"  // the file stays in git too, with safeguards
)

// FullFileMode records why a link sends whole files instead of ops, and for
// which versions: it lifts once the CLI or the server's format version
// changes.
type FullFileMode struct {
	Reason        string    `json:"reason"`
	CLIVersion    string    `json:"cli_version"`
	FormatVersion int       `json:"format_version"`
	Since         time.Time `json:"since"`
}

// LastSync is the outcome of the most recent round.
type LastSync struct {
	At    time.Time `json:"at"`
	Rev   int       `json:"rev"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// Link is sync.json.
type Link struct {
	Host         string        `json:"host"`
	ProjectID    string        `json:"project_id"`
	ProjectSlug  string        `json:"project_slug"`
	ProjectURL   string        `json:"project_url"`
	Jurisdiction string        `json:"jurisdiction,omitempty"`
	BaseRev      int           `json:"base_rev"`
	LinkedAt     time.Time     `json:"linked_at"`
	Git          string        `json:"git,omitempty"`
	GitHead      string        `json:"git_head,omitempty"`
	FullFile     *FullFileMode `json:"full_file,omitempty"`
	ReadOnly     bool          `json:"read_only,omitempty"`
	LastSync     *LastSync     `json:"last_sync,omitempty"`
}

func linkPaths(file string) (link, base string, err error) {
	dir, err := statedir.For(file)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "sync.json"), filepath.Join(dir, "sync-base.md"), nil
}

// LoadLink reads the link for file; ErrNotLinked when there is none.
func LoadLink(file string) (*Link, error) {
	lp, _, err := linkPaths(file)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(lp)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	var l Link
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", lp, err)
	}
	return &l, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Save writes the link for file.
func (l *Link) Save(file string) error {
	lp, _, err := linkPaths(file)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(lp, append(data, '\n'))
}

// LoadBase reads sync-base.md.
func LoadBase(file string) ([]byte, error) {
	_, bp, err := linkPaths(file)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(bp)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the sync base for this file is missing; run 'todomd link' again")
	}
	return data, err
}

// SaveBase writes sync-base.md.
func SaveBase(file string, canonical []byte) error {
	_, bp, err := linkPaths(file)
	if err != nil {
		return err
	}
	return writeAtomic(bp, canonical)
}

// Unlink removes the link and base. It never touches the file or the server.
func Unlink(file string) error {
	lp, bp, err := linkPaths(file)
	if err != nil {
		return err
	}
	if _, err := os.Stat(lp); errors.Is(err, os.ErrNotExist) {
		return ErrNotLinked
	}
	for _, p := range []string{lp, bp} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

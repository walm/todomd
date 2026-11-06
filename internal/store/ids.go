package store

import (
	"os"
	"regexp"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

var rawIDRe = regexp.MustCompile(`<!--\s*id:\s*([0-9a-z]+)\s*-->`)

// IDsPersisted reports whether every parsed task ID literally exists in the
// raw file — false when a hand-added task was assigned a fresh ID at parse
// time, which must be written back before anything can refer to it.
func IDsPersisted(raw []byte, f *task.File) bool {
	present := map[string]bool{}
	for _, m := range rawIDRe.FindAllSubmatch(raw, -1) {
		present[string(m[1])] = true
	}
	for _, t := range f.AllTasks() {
		if !present[t.ID] {
			return false
		}
	}
	return true
}

// ReadPersisted reads and parses the file, first writing back any IDs the
// parser had to assign (tasks added by hand), so the returned model's IDs
// all exist in the file. raw is the file as it is now.
func (s *Store) ReadPersisted() (raw []byte, f *task.File, err error) {
	read := func() error {
		if raw, err = os.ReadFile(s.Path); err != nil {
			if os.IsNotExist(err) {
				return ErrNoFile
			}
			return err
		}
		f, err = markdown.Parse(raw)
		return err
	}
	if err := read(); err != nil {
		return nil, nil, err
	}
	if IDsPersisted(raw, f) {
		return raw, f, nil
	}
	if err := s.Mutate(func(*task.File) error { return nil }); err != nil {
		return nil, nil, err
	}
	if err := read(); err != nil {
		return nil, nil, err
	}
	return raw, f, nil
}

// Locked runs fn holding the file's exclusive advisory lock, for callers
// that read, decide and write themselves (sync's write-back).
func (s *Store) Locked(fn func() error) error { return s.withLock(fn) }

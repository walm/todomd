package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/walm/todomd/internal/statedir"
)

// CheckInterval is how long a recorded check stays fresh. The passive notice
// is only ever shown to humans, so once a day is plenty.
const CheckInterval = 24 * time.Hour

// NoCheckEnv disables the background check and the notice entirely.
const NoCheckEnv = "TODOMD_NO_UPDATE_CHECK"

type cacheFile struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

func cachePath() (string, error) {
	dir, err := statedir.Global()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-check.json"), nil
}

// Disabled reports whether update checking is switched off, either by the
// user or because this build has no version to compare against.
func Disabled(current string) bool {
	return os.Getenv(NoCheckEnv) != "" || !IsRelease(current)
}

// LoadCache returns the last recorded latest version and when it was recorded.
// A missing or unreadable cache is not an error: it just isn't fresh.
func LoadCache() (latest string, checkedAt time.Time, ok bool) {
	p, err := cachePath()
	if err != nil {
		return "", time.Time{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", time.Time{}, false
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil || c.Latest == "" {
		return "", time.Time{}, false
	}
	return c.Latest, c.CheckedAt, true
}

// SaveCache records the latest known version and the time of the check.
func SaveCache(latest string, now time.Time) error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cacheFile{Latest: latest, CheckedAt: now})
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// Notice returns the upgrade hint to show a human, or "" when there is
// nothing to say. It reads only the cache — never the network — so it is safe
// on latency-sensitive paths like --help.
func Notice(current string) string {
	if Disabled(current) {
		return ""
	}
	latest, _, ok := LoadCache()
	if !ok || !Newer(current, latest) {
		return ""
	}
	return "todomd " + latest + " is available (you have " + current + ") — run: todomd upgrade"
}

// RefreshCache looks up the latest release and records it, but only if the
// cached answer has gone stale. Callers run this off the critical path.
func RefreshCache(ctx context.Context, current string, now time.Time) {
	if Disabled(current) {
		return
	}
	if _, checkedAt, ok := LoadCache(); ok && now.Sub(checkedAt) < CheckInterval {
		return
	}
	latest, err := NewClient().Latest(ctx)
	if err != nil {
		return // offline, rate-limited, whatever: the notice simply waits
	}
	_ = SaveCache(latest, now)
}

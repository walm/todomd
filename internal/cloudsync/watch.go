package cloudsync

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/statedir"
)

// ErrAccessRevoked and ErrProjectDeleted end a watcher.
var (
	ErrAccessRevoked  = errors.New("access to the project was revoked, or the token is no longer valid (run 'todomd login')")
	ErrProjectDeleted = errors.New("the linked project was deleted (run 'todomd unlink')")
)

// WatchLock is the per-file lock that makes one process the file's watcher.
type WatchLock struct{ f *os.File }

// AcquireWatchLock takes the watcher lock without blocking. If another
// process holds it, it returns (nil, pid of the holder, nil).
func AcquireWatchLock(file string) (*WatchLock, int, error) {
	dir, err := statedir.For(file)
	if err != nil {
		return nil, 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "sync.watch.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		data, _ := os.ReadFile(f.Name())
		f.Close()
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return nil, pid, nil
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &WatchLock{f: f}, 0, nil
}

// Release gives the lock up.
func (w *WatchLock) Release() {
	if w == nil || w.f == nil {
		return
	}
	_ = w.f.Truncate(0)
	_ = unix.Flock(int(w.f.Fd()), unix.LOCK_UN)
	w.f.Close()
}

// WatcherPID returns the pid of the process watching file, or 0.
func WatcherPID(file string) int {
	l, pid, err := AcquireWatchLock(file)
	if err != nil {
		return 0
	}
	if l != nil {
		l.Release()
		return 0
	}
	return pid
}

// Watcher states reported through OnState.
const (
	StateOnline  = "online"
	StateOffline = "offline"
	StatePaused  = "paused" // the file can't be synced as it is (conflict markers, parse error)
)

// Watcher keeps a linked file in sync until its context ends.
type Watcher struct {
	Syncer *Syncer
	// OnRound is called after every round that did something, and after
	// every failed round.
	OnRound func(*Result, error)
	// OnState is called on transitions only.
	OnState func(state, detail string)

	PollEvery time.Duration // local stat interval (default 1s)
	Debounce  time.Duration // after a local change (default 750ms)
	Safety    time.Duration // a round even without signals (default 5m)
	Fallback  time.Duration // pull interval while the event stream is down (default 30s)

	// Primed skips the first round: the caller has just run one.
	Primed bool

	rev atomic.Int64 // the link's base revision, for the stream goroutine
}

func (w *Watcher) defaults() {
	if w.PollEvery == 0 {
		w.PollEvery = time.Second
	}
	if w.Debounce == 0 {
		w.Debounce = 750 * time.Millisecond
	}
	if w.Safety == 0 {
		w.Safety = 5 * time.Minute
	}
	if w.Fallback == 0 {
		w.Fallback = 30 * time.Second
	}
	if w.OnRound == nil {
		w.OnRound = func(*Result, error) {}
	}
	if w.OnState == nil {
		w.OnState = func(string, string) {}
	}
}

type fileStamp struct {
	mod  time.Time
	size int64
	hash [32]byte
	ok   bool
}

func stamp(path string, prev fileStamp) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	if prev.ok && st.ModTime().Equal(prev.mod) && st.Size() == prev.size {
		return prev
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: st.ModTime(), size: st.Size(), hash: sha256.Sum256(data), ok: true}
}

// streamMsg is what the event-stream goroutine reports.
type streamMsg struct {
	rev   int
	state string // "up", "down"
	fatal error
}

// Run syncs once, then on every remote revision, local change and safety
// tick, until ctx ends or the link becomes unusable. The caller holds the
// watch lock.
func (w *Watcher) Run(ctx context.Context) error {
	w.defaults()
	s := w.Syncer
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	state := ""
	setState := func(st, detail string) {
		if st != state {
			state = st
			w.OnState(st, detail)
		}
	}

	var last fileStamp
	retry := time.NewTimer(time.Hour)
	retry.Stop()
	backoff := time.Second
	// round runs one sync round and classifies its outcome. It returns a
	// non-nil error only when the watcher must stop.
	round := func() error {
		res, err := s.Round(ctx, Options{})
		w.rev.Store(int64(s.Link.BaseRev))
		last = stamp(s.Path, fileStamp{})
		if err == nil {
			backoff = time.Second
			retry.Stop()
			setState(StateOnline, "")
			if !res.Unchanged {
				w.OnRound(res, nil)
			}
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		s.RecordError(err)
		if fatal := FatalError(err); fatal != nil {
			return fatal
		}
		switch RoundState(err) {
		case StateOffline:
			setState(StateOffline, err.Error())
			retry.Reset(jitter(backoff))
			backoff = min(backoff*2, time.Minute)
		case StatePaused:
			setState(StatePaused, err.Error())
		default:
			w.OnRound(nil, err)
		}
		return nil
	}

	w.rev.Store(int64(s.Link.BaseRev))
	if w.Primed {
		last = stamp(s.Path, fileStamp{})
		setState(StateOnline, "")
	} else if err := round(); err != nil {
		return err
	}

	// The event stream, reconnecting with backoff.
	stream := make(chan streamMsg, 8)
	go w.stream(ctx, stream)

	poll := time.NewTicker(w.PollEvery)
	defer poll.Stop()
	safety := time.NewTicker(w.Safety)
	defer safety.Stop()
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	fallback := time.NewTicker(w.Fallback)
	fallback.Stop()
	streamDown := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-stream:
			switch {
			case m.fatal != nil:
				return m.fatal
			case m.state == "up":
				streamDown = 0
				fallback.Stop()
			case m.state == "down":
				if streamDown++; streamDown == 3 {
					fallback.Reset(w.Fallback)
				}
			case m.rev > int(w.rev.Load()):
				if err := round(); err != nil {
					return err
				}
			}
		case <-poll.C:
			if now := stamp(s.Path, last); now != last {
				last = now
				debounce.Reset(w.Debounce)
			}
		case <-debounce.C:
			if err := round(); err != nil {
				return err
			}
		case <-retry.C:
			if err := round(); err != nil {
				return err
			}
		case <-fallback.C:
			if err := round(); err != nil {
				return err
			}
		case <-safety.C:
			if err := round(); err != nil {
				return err
			}
		}
	}
}

// FatalError returns the error a failed round ends syncing with, or nil
// when syncing can go on (a later round may succeed): the token is no longer
// valid (ErrAccessRevoked), the project is gone (ErrProjectDeleted), or this
// todomd is too old for the service (err itself).
func FatalError(err error) error {
	switch {
	case cloud.IsStatus(err, 401, ""):
		return ErrAccessRevoked
	case cloud.IsStatus(err, 404, ""):
		return ErrProjectDeleted
	case cloud.IsStatus(err, 426, ""):
		return err
	}
	return nil
}

// RoundState classifies a round's non-fatal error: StateOffline when
// todomd.online can't be reached, StatePaused when the file can't be synced
// as it is (conflict markers, a parse error), "" for anything else.
func RoundState(err error) string {
	var ue *cloud.UnavailableError
	var cm *ConflictMarkersError
	var pe interface{ ErrCode() string }
	switch {
	case errors.As(err, &ue):
		return StateOffline
	case errors.As(err, &cm), errors.As(err, &pe):
		return StatePaused
	}
	return ""
}

func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d)))
}

// stream follows the project's event stream, reporting revisions and
// connection state, reconnecting with exponential backoff (1s doubling to
// 60s, jittered).
func (w *Watcher) stream(ctx context.Context, out chan<- streamMsg) {
	s := w.Syncer
	backoff := time.Second
	send := func(m streamMsg) bool {
		select {
		case out <- m:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for ctx.Err() == nil {
		connected := false
		err := s.Client.Events(ctx, s.Link.ProjectID, int(w.rev.Load()), func(ev cloud.Event) bool {
			if !connected {
				connected = true
				backoff = time.Second
				if !send(streamMsg{state: "up"}) {
					return false
				}
			}
			switch ev.Type {
			case cloud.EventRevision:
				r, err := ev.Revision()
				if err == nil {
					return send(streamMsg{rev: r.Rev})
				}
			case cloud.EventAccessRevoked:
				send(streamMsg{fatal: ErrAccessRevoked})
				return false
			case cloud.EventProjectDeleted:
				send(streamMsg{fatal: ErrProjectDeleted})
				return false
			}
			return true
		})
		if ctx.Err() != nil {
			return
		}
		switch {
		case cloud.IsStatus(err, 401, ""):
			send(streamMsg{fatal: ErrAccessRevoked})
			return
		case cloud.IsStatus(err, 404, ""):
			send(streamMsg{fatal: ErrProjectDeleted})
			return
		case cloud.IsStatus(err, 426, ""):
			send(streamMsg{fatal: err})
			return
		}
		if err == nil && connected {
			continue // a clean rotation: reconnect right away
		}
		if !send(streamMsg{state: "down"}) {
			return
		}
		select {
		case <-time.After(jitter(backoff)):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, time.Minute)
	}
}

// Describe summarizes a round for people, in one line.
func Describe(r *Result) string {
	var parts []string
	if r.Restored {
		parts = append(parts, "restored the file from the project")
	}
	if c := countChanges(len(r.Pushed), len(r.PushedBoards)); c != "" {
		parts = append(parts, "pushed "+c)
	}
	if c := countChanges(len(r.Pulled), len(r.PulledBoards)); c != "" {
		parts = append(parts, "pulled "+c)
	}
	if n := len(r.Conflicts); n > 0 {
		parts = append(parts, fmt.Sprintf("%d conflict%s", n, plural(n)))
	}
	if n := len(r.Attachments.Uploaded); n > 0 {
		parts = append(parts, fmt.Sprintf("uploaded %d image%s", n, plural(n)))
	}
	if n := len(r.Attachments.Downloaded); n > 0 {
		parts = append(parts, fmt.Sprintf("downloaded %d image%s", n, plural(n)))
	}
	if r.ReadOnly {
		parts = append(parts, "local edits kept but not pushed (read-only access)")
	}
	if len(parts) == 0 {
		parts = append(parts, "up to date")
	}
	return strings.Join(parts, " · ") + fmt.Sprintf(" · rev %d", r.Rev)
}

// countChanges words task and board change counts: "2 changes",
// "1 board change", "2 changes, 1 board change", or "" for none.
func countChanges(tasks, boards int) string {
	var out []string
	if tasks > 0 {
		out = append(out, fmt.Sprintf("%d change%s", tasks, plural(tasks)))
	}
	if boards > 0 {
		out = append(out, fmt.Sprintf("%d board change%s", boards, plural(boards)))
	}
	return strings.Join(out, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

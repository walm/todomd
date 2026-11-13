package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
)

// errInterrupted ends a wait cut short by a signal (Ctrl-C, or an agent's
// shell tool killing the command). The cursor is left where it was, so no
// event is consumed without being read.
var errInterrupted = errors.New("interrupted; the cursor was not advanced")

// syncStateJSON is what `watch --json` says about syncing.
type syncStateJSON struct {
	Linked     bool   `json:"linked"`
	Project    string `json:"project,omitempty"`
	Rev        int    `json:"rev,omitempty"`
	WatcherPID int    `json:"watcher_pid,omitempty"` // another process syncs the file
	Problem    string `json:"problem,omitempty"`     // the last sync problem, if it lasts
}

type watchJSON struct {
	changesJSON
	Sync syncStateJSON `json:"sync"`
}

// boardSync keeps a linked file in sync for one command: a round when asked,
// and a background watcher in between. When another process (a 'sync
// --watch', the TUI) already holds the file's watch lock, that process does
// the syncing and boardSync does nothing.
type boardSync struct {
	syncer *cloudsync.Syncer
	lock   *cloudsync.WatchLock // nil when another process syncs the file
	pid    int                  // that process

	mu      sync.Mutex
	problem string

	cancel context.CancelFunc
	done   chan struct{}
	fatal  chan error // the watcher stopped for good
}

// openBoardSync takes the file's watch lock if it is free.
func openBoardSync(s *cloudsync.Syncer) (*boardSync, error) {
	lock, pid, err := cloudsync.AcquireWatchLock(s.Path)
	if err != nil {
		return nil, err
	}
	return &boardSync{syncer: s, lock: lock, pid: pid, fatal: make(chan error, 1)}, nil
}

func (b *boardSync) owner() bool { return b != nil && b.lock != nil }

// setProblem records the current sync problem ("" when syncing works) and
// says so on stderr when it changes, so an agent reading the output sees
// that online edits aren't arriving.
func (b *boardSync) setProblem(p string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p == b.problem {
		return
	}
	switch {
	case p != "":
		fmt.Fprintf(os.Stderr, "todomd: sync: %s\n", p)
	case b.problem != "":
		fmt.Fprintln(os.Stderr, "todomd: sync: back online")
	}
	b.problem = p
}

func (b *boardSync) state() syncStateJSON {
	if b == nil {
		return syncStateJSON{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return syncStateJSON{Linked: true, Project: b.syncer.Link.ProjectSlug, Rev: b.syncer.Link.BaseRev,
		WatcherPID: b.pid, Problem: b.problem}
}

func describeProblem(state, detail string) string {
	switch state {
	case cloudsync.StateOffline:
		return "offline, retrying: " + detail
	case cloudsync.StatePaused:
		return "paused: " + detail
	}
	return detail
}

// round runs one sync round when this process owns the file. It returns an
// error only when syncing can't go on (the login or the project is gone);
// anything else is reported on stderr and retried later.
func (b *boardSync) round(ctx context.Context) (ok bool, err error) {
	if !b.owner() {
		return false, nil
	}
	_, err = b.syncer.Round(ctx, cloudsync.Options{})
	if err == nil {
		b.setProblem("")
		return true, nil
	}
	if ctx.Err() != nil {
		return false, nil
	}
	b.syncer.RecordError(err)
	if fatal := cloudsync.FatalError(err); fatal != nil {
		return false, fatal
	}
	b.setProblem(describeProblem(cloudsync.RoundState(err), err.Error()))
	return false, nil
}

// start runs the watcher in the background until stop. primed: a round
// just succeeded, so the watcher needn't begin with one.
func (b *boardSync) start(ctx context.Context, primed bool) {
	if !b.owner() {
		return
	}
	ctx, b.cancel = context.WithCancel(ctx)
	b.done = make(chan struct{})
	w := &cloudsync.Watcher{
		Syncer: b.syncer,
		Primed: primed,
		OnRound: func(_ *cloudsync.Result, err error) {
			if err != nil {
				b.setProblem(err.Error())
			}
		},
		OnState: func(state, detail string) {
			if state == cloudsync.StateOnline {
				b.setProblem("")
				return
			}
			b.setProblem(describeProblem(state, detail))
		},
	}
	go func() {
		defer close(b.done)
		if err := w.Run(ctx); err != nil && ctx.Err() == nil {
			b.fatal <- err
		}
	}()
}

// stop ends the background watcher, if one runs.
func (b *boardSync) stop() {
	if b == nil || b.cancel == nil {
		return
	}
	b.cancel()
	<-b.done
	b.cancel = nil
}

func (b *boardSync) close() {
	if b == nil {
		return
	}
	b.stop()
	b.lock.Release()
}

func (b *boardSync) fatalCh() <-chan error {
	if b == nil {
		return nil
	}
	return b.fatal
}

type lookFunc func(snapshot []byte) (evs []changes.Event, raw []byte, err error)

// cursorLook returns the function that reads the file (persisting
// parse-assigned IDs of hand-added tasks, so events carry IDs that exist in
// the file) and diffs it against a cursor snapshot. A nil snapshot reads
// without diffing.
//
// A task_updated that only changes reactions is dropped unless one of the
// reacted comments was written by one of mine (the watcher's own names, see
// myNames): a reaction to someone else's comment isn't worth waking for.
func cursorLook(s *store.Store, cpath string, ignoreAuthors, mine []string) lookFunc {
	return func(snapshot []byte) ([]changes.Event, []byte, error) {
		raw, cur, err := s.ReadPersisted()
		if errors.Is(err, store.ErrNoFile) {
			return nil, nil, err
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", s.Path, err)
		}
		if snapshot == nil {
			return nil, raw, nil
		}
		old, err := parseSnapshot(snapshot, cpath)
		if err != nil {
			return nil, nil, err
		}
		var evs []changes.Event
		for _, e := range changes.Diff(old, cur) {
			if e.Type == changes.CommentAdded && authorIn(e.Comment.Author, ignoreAuthors) {
				continue
			}
			if changes.OnlyReactions(e) && !reactsToMine(e, mine) {
				continue
			}
			evs = append(evs, e)
		}
		return evs, raw, nil
	}
}

// pollChanges looks at the file whenever it changes until look reports
// events, ctx ends (returning ctx's error) or the sync stops for good.
func pollChanges(ctx context.Context, path string, snapshot []byte, look lookFunc,
	fatal <-chan error) ([]changes.Event, []byte, error) {
	var lastMod time.Time
	var lastSize int64 = -1
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		st, err := os.Stat(path)
		if err == nil && (!st.ModTime().Equal(lastMod) || st.Size() != lastSize) {
			lastMod, lastSize = st.ModTime(), st.Size()
			evs, raw, err := look(snapshot)
			if err != nil {
				return nil, nil, err
			}
			if len(evs) > 0 {
				return evs, raw, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case err := <-fatal:
			return nil, nil, err
		case <-tick.C:
		}
	}
}

// waitContexts returns the signal context (Ctrl-C, SIGTERM) and, below it,
// the one that also ends after timeout (0: never).
func waitContexts(timeout time.Duration) (sig, wait context.Context, cancel func()) {
	sig, cancelSig := cmdContext()
	wait, cancelWait := sig, context.CancelFunc(func() {})
	if timeout > 0 {
		wait, cancelWait = context.WithTimeout(sig, timeout)
	}
	return sig, wait, func() { cancelWait(); cancelSig() }
}

func newWatch() *cobra.Command {
	var as string
	var ignoreAuthors, authors []string
	var timeout time.Duration
	var noWait bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Sync, then wait for card changes and print them (one round per run)",
		Long: `For agents: run it, handle the events it prints, run it again.

Each run syncs with todomd.online when the file is linked (pushing your own
writes, pulling the project's), keeps syncing while it waits, and syncs
once more before it prints. A file that isn't linked is simply watched.
When another process already syncs the file ('todomd sync --watch', the
TUI), that process does the syncing and watch only waits.

Events are those of 'todomd changes' (task_added, task_deleted, task_moved,
task_updated, comment_added), read with a cursor (--as). Writes made with
the same --as (for example 'todomd comment <id> --author agent --as agent')
are not reported back. A change to reactions alone wakes it only when it is
on a comment of yours: written by --author, or by the --as name. The first
run starts the cursor at the synced file and then waits for what changes
next; read the board with 'todomd list'.

--no-wait syncs and reports what is pending without waiting: run it once
before you stop, so your last writes reach todomd.online.

A run cut short by Ctrl-C or SIGTERM doesn't advance the cursor, so the
next run reports the same events. Sync problems (offline, not logged in)
are printed on stderr and under "sync" in --json.

Exit codes: 4 not logged in, 5 the linked project is gone, 7 this todomd is
too old.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sigCtx, ctx, cancel := waitContexts(timeout)
			defer cancel()

			var s *store.Store
			var bs *boardSync
			syncer, _, err := openSyncer()
			switch {
			case errors.Is(err, cloudsync.ErrNotLinked):
				if s, err = newStore(false); err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				s = syncer.Store
				if bs, err = openBoardSync(syncer); err != nil {
					return err
				}
				defer bs.close()
			}

			cpath, err := changes.CursorPath(s.Path, as)
			if err != nil {
				return err
			}
			look := cursorLook(s, cpath, ignoreAuthors, myNames(as, authors))

			primed, err := bs.round(ctx)
			if err != nil {
				return err
			}
			if sigCtx.Err() != nil {
				return errInterrupted
			}

			prev, ok, err := changes.LoadCursor(cpath)
			if err != nil {
				return err
			}
			evs, raw, err := look(prev)
			if err != nil {
				return err
			}
			if !ok {
				// Start from the synced file: what is there now is the board
				// to read, not news.
				if err := changes.SaveCursor(cpath, raw); err != nil {
					return err
				}
				prev = raw
			}

			timedOut := false
			if len(evs) == 0 && !noWait {
				bs.start(ctx, primed)
				evs, raw, err = pollChanges(ctx, s.Path, prev, look, bs.fatalCh())
				bs.stop()
				switch {
				case sigCtx.Err() != nil:
					return errInterrupted
				case errors.Is(err, context.DeadlineExceeded):
					timedOut = true
				case err != nil:
					return err
				}
				// Sync once more (push anything written meanwhile, pull what
				// arrived since) and report everything up to now.
				if _, err := bs.round(sigCtx); err != nil {
					return err
				}
				if sigCtx.Err() != nil {
					return errInterrupted
				}
				if evs, raw, err = look(prev); err != nil {
					return err
				}
				timedOut = timedOut && len(evs) == 0
			}
			if err := changes.SaveCursor(cpath, raw); err != nil {
				return err
			}

			if flagJSON {
				out := watchJSON{
					changesJSON: changesJSON{File: s.Path, Cursor: as, Initialized: !ok, Events: eventsJSON(evs), TimedOut: timedOut},
					Sync:        bs.state(),
				}
				return printJSON(out)
			}
			if !ok {
				fmt.Printf("cursor %q initialized\n", as)
			}
			return printEvents(evs, timedOut)
		},
	}
	cmd.Flags().StringVar(&as, "as", defaultCursor(),
		"cursor name, and the --as of your own writes (e.g. agent; env TODOMD_CURSOR)")
	cmd.Flags().StringArrayVar(&ignoreAuthors, "ignore-author", nil, "drop comment_added events by this author (repeatable)")
	authorFlag(cmd, &authors)
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "give up waiting after this long (reports no events, timed_out: true)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "sync and report what is pending without waiting")
	jsonFlag(cmd)
	return cmd
}

// authorFlag adds --author to watch and changes: whose comments are the
// watcher's own, for reactions.
func authorFlag(cmd *cobra.Command, authors *[]string) {
	cmd.Flags().StringArrayVar(authors, "author", nil,
		"your comment author name, so reactions to your comments are reported (repeatable; default: the --as name)")
}

// myNames is who the watcher writes comments as: --author, else the cursor
// name (an agent runs 'comment --author agent --as agent').
func myNames(as string, authors []string) []string {
	if len(authors) > 0 {
		return authors
	}
	return []string{as}
}

// reactsToMine reports whether a reaction change in e is on a comment
// written by one of names (case aside).
func reactsToMine(e changes.Event, names []string) bool {
	for _, n := range changes.ReactedComments(e) {
		if n >= 1 && n <= len(e.Task.Comments) && authorIn(e.Task.Comments[n-1].Author, names) {
			return true
		}
	}
	return false
}

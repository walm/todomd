package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/store"
)

type fieldJSON struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type eventJSON struct {
	Type    string               `json:"type"`
	TaskID  string               `json:"task"`
	Title   string               `json:"title"`
	Board   string               `json:"board"`
	From    string               `json:"from,omitempty"`
	To      string               `json:"to,omitempty"`
	Fields  map[string]fieldJSON `json:"fields,omitempty"`
	Comment *commentJSON         `json:"comment,omitempty"`
	Detail  *taskJSON            `json:"detail,omitempty"` // full task, task_added only
}

type changesJSON struct {
	File        string      `json:"file"`
	Cursor      string      `json:"cursor"`
	Initialized bool        `json:"initialized"`
	Events      []eventJSON `json:"events"`
	TimedOut    bool        `json:"timed_out,omitempty"`
}

func toEventJSON(e changes.Event) eventJSON {
	out := eventJSON{
		Type:   e.Type,
		TaskID: e.Task.ID,
		Title:  e.Task.Title,
		Board:  e.Board,
		From:   e.From,
		To:     e.To,
	}
	if e.Comment != nil {
		out.Comment = &commentJSON{Author: e.Comment.Author, Date: e.Comment.Date, Text: e.Comment.Text}
	}
	if len(e.Fields) > 0 {
		out.Fields = map[string]fieldJSON{}
		for k, v := range e.Fields {
			out.Fields[k] = fieldJSON{Old: v.Old, New: v.New}
		}
	}
	if e.Type == changes.TaskAdded {
		d := toJSON(e.Task, e.Board)
		out.Detail = &d
	}
	return out
}

func newChanges() *cobra.Command {
	var as string
	var peek bool
	var ignoreAuthors []string
	var wait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "Show what changed since this cursor last looked",
		Long: `Diffs the file against a per-cursor snapshot (stored under
$XDG_STATE_HOME/todomd, default ~/.local/state/todomd) and reports semantic
events: task_added, task_deleted, task_moved, task_updated, comment_added.
Reading advances the cursor unless --peek is given. The first call
initializes the cursor and reports no events.

--wait blocks until there is something to report (or --timeout passes), for
an agent waiting on a human's reply. If the file is linked to todomd.online
and no watcher is running, it syncs while it waits, so online edits count.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newStore(false)
			if err != nil {
				return err
			}
			cpath, err := changes.CursorPath(s.Path, as)
			if err != nil {
				return err
			}
			// look reads the file (persisting parse-assigned IDs of
			// hand-added tasks, so events carry IDs that exist in the file)
			// and diffs it against snapshot.
			look := func(snapshot []byte) (evs []changes.Event, raw []byte, err error) {
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
				old, err := markdown.Parse(snapshot)
				if err != nil {
					return nil, nil, fmt.Errorf("cursor snapshot %s: %w", cpath, err)
				}
				for _, e := range changes.Diff(old, cur) {
					if e.Type == changes.CommentAdded && authorIn(e.Comment.Author, ignoreAuthors) {
						continue
					}
					evs = append(evs, e)
				}
				return evs, raw, nil
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
				if !peek {
					if err := changes.SaveCursor(cpath, raw); err != nil {
						return err
					}
				}
				if !wait {
					if flagJSON {
						return printJSON(changesJSON{File: s.Path, Cursor: as, Initialized: true, Events: []eventJSON{}})
					}
					fmt.Printf("cursor %q initialized; future calls report changes from now\n", as)
					return nil
				}
				prev = raw // wait for changes from now on
			}
			timedOut := false
			if wait && len(evs) == 0 {
				if evs, raw, timedOut, err = waitForChanges(s, prev, timeout, look); err != nil {
					return err
				}
			}
			if !peek {
				if err := changes.SaveCursor(cpath, raw); err != nil {
					return err
				}
			}

			if flagJSON {
				out := changesJSON{File: s.Path, Cursor: as, Initialized: !ok, Events: []eventJSON{}, TimedOut: timedOut}
				for _, e := range evs {
					out.Events = append(out.Events, toEventJSON(e))
				}
				return printJSON(out)
			}
			if len(evs) == 0 {
				if timedOut {
					fmt.Println("no changes (timed out)")
				} else {
					fmt.Println("no changes")
				}
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			for _, e := range evs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Type, e.Task.ID, e.Task.Title, eventDetail(e))
			}
			return w.Flush()
		},
	}
	defaultCursor := "default"
	if env := os.Getenv("TODOMD_CURSOR"); env != "" {
		defaultCursor = env
	}
	cmd.Flags().StringVar(&as, "as", defaultCursor,
		"cursor name (one per consumer, e.g. an agent name; env TODOMD_CURSOR)")
	cmd.Flags().BoolVar(&peek, "peek", false, "report without advancing the cursor")
	cmd.Flags().StringArrayVar(&ignoreAuthors, "ignore-author", nil, "drop comment_added events by this author (repeatable)")
	cmd.Flags().BoolVar(&wait, "wait", false, "block until there are changes to report")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "with --wait, give up after this long (reports no events, timed_out: true)")
	jsonFlag(cmd)
	return cmd
}

func authorIn(author string, list []string) bool {
	for _, a := range list {
		if strings.EqualFold(a, author) {
			return true
		}
	}
	return false
}

func eventDetail(e changes.Event) string {
	switch e.Type {
	case changes.TaskMoved:
		return e.From + " → " + e.To
	case changes.CommentAdded:
		text := e.Comment.Text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[:i] + "…"
		}
		return e.Comment.Author + ": " + text
	case changes.TaskUpdated:
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return "changed: " + strings.Join(keys, ", ")
	case changes.TaskDeleted:
		return "was on " + e.Board
	default:
		return "on " + e.Board
	}
}

// waitForChanges polls the file until look reports events or timeout passes.
// While it waits on a linked file that nobody is watching, it runs the sync
// watcher itself, so edits made on todomd.online arrive and count.
func waitForChanges(s *store.Store, snapshot []byte, timeout time.Duration,
	look func([]byte) ([]changes.Event, []byte, error)) ([]changes.Event, []byte, bool, error) {
	ctx, cancel := cmdContext()
	defer cancel()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if syncer, _, err := openSyncer(); err == nil {
		if lock, _, err := cloudsync.AcquireWatchLock(s.Path); err == nil && lock != nil {
			defer lock.Release()
			w := &cloudsync.Watcher{Syncer: syncer, OnRound: func(_ *cloudsync.Result, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "todomd: sync: %v\n", err)
				}
			}}
			done := make(chan struct{})
			go func() {
				defer close(done)
				if err := w.Run(ctx); err != nil && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "todomd: sync stopped: %v\n", explain(err))
				}
			}()
			defer func() { cancel(); <-done }()
		}
	}

	var lastMod time.Time
	var lastSize int64 = -1
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			// One last look, so a change in the final second isn't skipped.
			evs, raw, err := look(snapshot)
			return evs, raw, len(evs) == 0, err
		case <-tick.C:
		}
		st, err := os.Stat(s.Path)
		if err != nil || (st.ModTime().Equal(lastMod) && st.Size() == lastSize) {
			continue
		}
		lastMod, lastSize = st.ModTime(), st.Size()
		evs, raw, err := look(snapshot)
		if err != nil {
			return nil, nil, false, err
		}
		if len(evs) > 0 {
			return evs, raw, false, nil
		}
	}
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/walm/todomd/internal/cloud/cloudtest"
	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

type watchOut struct {
	Initialized bool `json:"initialized"`
	TimedOut    bool `json:"timed_out"`
	Events      []struct {
		Type    string `json:"type"`
		Task    string `json:"task"`
		Comment *struct {
			Author string `json:"author"`
			Text   string `json:"text"`
		} `json:"comment"`
	} `json:"events"`
	Sync struct {
		Linked     bool   `json:"linked"`
		WatcherPID int    `json:"watcher_pid"`
		Problem    string `json:"problem"`
	} `json:"sync"`
}

func runWatchJSON(t *testing.T, path string, args ...string) watchOut {
	t.Helper()
	out, err := run(t, append([]string{"--file", path, "watch", "--as", "agent", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("watch %v: %v\n%s", args, err, out)
	}
	var w watchOut
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return w
}

func (w watchOut) types() []string {
	var out []string
	for _, e := range w.Events {
		out = append(out, e.Type)
	}
	return out
}

func linkProject(t *testing.T, path string) string {
	t.Helper()
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var linked struct{ Project struct{ ID string } }
	if err := json.Unmarshal([]byte(out), &linked); err != nil {
		t.Fatal(err)
	}
	return linked.Project.ID
}

// webComment adds a comment to the project's only task, as if on the web.
func webComment(srv *cloudtest.Server, project, text string) {
	srv.Edit(project, func(f *task.File) {
		var tk *task.Task
		for _, b := range f.Boards {
			for _, t := range b.Tasks {
				tk = t
			}
		}
		tk.Comments = append(tk.Comments, task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 8}, Text: text})
	})
}

func TestWatchLocalFile(t *testing.T) {
	path := testFile(t)
	id := addOne(t, path, "Task")

	w := runWatchJSON(t, path, "--no-wait")
	if !w.Initialized || len(w.Events) != 0 || w.Sync.Linked {
		t.Fatalf("first run: %+v", w)
	}
	// The agent's own writes aren't reported back.
	for _, args := range [][]string{
		{"comment", id, "--author", "agent", "on it", "--as", "agent"},
		{"move", id, "--to", "In Progress", "--as", "agent"},
	} {
		if _, err := run(t, append([]string{"--file", path}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		time.Sleep(time.Second)
		s := &store.Store{Path: path}
		s.Mutate(func(f *task.File) error {
			b, i, err := store.FindTask(f, id)
			if err != nil {
				return err
			}
			tk := b.Tasks[i]
			tk.Comments = append(tk.Comments, task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 8}, Text: "thanks"})
			return nil
		})
	}()
	w = runWatchJSON(t, path, "--timeout", "10s")
	if got := w.types(); len(got) != 1 || got[0] != "comment_added" || w.Events[0].Comment.Text != "thanks" {
		t.Errorf("events = %+v", w.Events)
	}

	w = runWatchJSON(t, path, "--timeout", "1s")
	if !w.TimedOut || len(w.Events) != 0 {
		t.Errorf("timeout: %+v", w)
	}
}

func TestWatchInterruptKeepsCursor(t *testing.T) {
	path := testFile(t)
	addOne(t, path, "Task")
	runWatchJSON(t, path, "--no-wait")
	go func() {
		time.Sleep(time.Second)
		syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	out, err := run(t, "--file", path, "watch", "--as", "agent", "--json", "--timeout", "20s")
	if !errors.Is(err, errInterrupted) || out != "" {
		t.Fatalf("interrupted watch: %v\n%s", err, out)
	}
}

func TestWatchLinkedFile(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	id := addOne(t, path, "Task")
	project := linkProject(t, path)

	w := runWatchJSON(t, path, "--no-wait")
	if !w.Initialized || !w.Sync.Linked || w.Sync.WatcherPID != 0 {
		t.Fatalf("first run: %+v", w)
	}

	// The agent answers; nothing syncs until its next watch.
	if _, err := run(t, "--file", path, "comment", id, "--author", "agent", "on it", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "move", id, "--to", "In Progress", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		webComment(srv, project, "reply from the web")
	}()
	start := time.Now()
	w = runWatchJSON(t, path, "--timeout", "20s")
	if got := w.types(); len(got) != 1 || w.Events[0].Comment.Text != "reply from the web" {
		t.Errorf("events = %+v (own writes must not echo)", w.Events)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("took too long")
	}
	head := srv.Head(project)
	if !strings.Contains(head, "on it") || !strings.Contains(head, "## In Progress\n\n### Task") {
		t.Errorf("the agent's writes weren't pushed:\n%s", head)
	}

	// The last write before stopping goes up with --no-wait.
	if _, err := run(t, "--file", path, "comment", id, "--author", "agent", "done, see the diff", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	w = runWatchJSON(t, path, "--no-wait")
	if len(w.Events) != 0 || w.TimedOut {
		t.Errorf("--no-wait: %+v", w)
	}
	if !strings.Contains(srv.Head(project), "done, see the diff") {
		t.Error("--no-wait didn't push")
	}

	// An online edit made while no watch ran is pulled at the start.
	webComment(srv, project, "one more thing")
	w = runWatchJSON(t, path, "--timeout", "5s")
	if len(w.Events) != 1 || w.Events[0].Comment.Text != "one more thing" {
		t.Errorf("pull on start: %+v", w)
	}
}

func TestWatchWithAnotherWatcher(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	id := addOne(t, path, "Task")
	project := linkProject(t, path)

	// Another process's 'sync --watch', holding the lock.
	link, err := cloudsync.LoadLink(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := openAccount(link.Host, true)
	if err != nil {
		t.Fatal(err)
	}
	syncer := &cloudsync.Syncer{Path: path, Store: &store.Store{Path: path}, Client: a.client, Link: link}
	lock, _, err := cloudsync.AcquireWatchLock(path)
	if err != nil || lock == nil {
		t.Fatalf("lock: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&cloudsync.Watcher{Syncer: syncer, PollEvery: 100 * time.Millisecond, Debounce: 100 * time.Millisecond}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done; lock.Release() })

	w := runWatchJSON(t, path, "--no-wait")
	if !w.Sync.Linked || w.Sync.WatcherPID != os.Getpid() {
		t.Fatalf("first run: %+v", w.Sync)
	}
	if _, err := run(t, "--file", path, "comment", id, "--author", "agent", "on it", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		webComment(srv, project, "reply from the web")
	}()
	w = runWatchJSON(t, path, "--timeout", "20s")
	if len(w.Events) != 1 || w.Events[0].Comment.Text != "reply from the web" {
		t.Errorf("events = %+v", w.Events)
	}
	if !strings.Contains(srv.Head(project), "on it") {
		t.Error("the other watcher didn't push the agent's comment")
	}
}

func TestWatchLinkedButLoggedOut(t *testing.T) {
	_, path := cloudEnv(t)
	login(t)
	addOne(t, path, "Task")
	linkProject(t, path)
	if _, err := run(t, "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "watch", "--no-wait"); classify(err) != 4 {
		t.Errorf("watch logged out: exit %d (%v), want 4", classify(err), err)
	}
}

// changes --wait: a signal leaves the cursor alone, and a linked file that
// can't sync says so instead of waiting silently on a stale file.
func TestChangesWaitInterruptAndSyncProblems(t *testing.T) {
	_, path := cloudEnv(t)
	login(t)
	addOne(t, path, "Task")
	linkProject(t, path)
	if _, err := run(t, "--file", path, "changes", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(time.Second)
		syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	if _, err := run(t, "--file", path, "changes", "--as", "agent", "--wait", "--timeout", "20s"); !errors.Is(err, errInterrupted) {
		t.Errorf("interrupted changes --wait: %v", err)
	}

	if _, err := run(t, "logout"); err != nil {
		t.Fatal(err)
	}
	_, stderr := runStreams(t, "--file", path, "changes", "--as", "agent", "--wait", "--timeout", "1s")
	if !strings.Contains(stderr, "not syncing while waiting") || !strings.Contains(stderr, "todomd login") {
		t.Errorf("stderr = %q", stderr)
	}
}

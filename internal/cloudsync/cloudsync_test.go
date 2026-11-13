package cloudsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/cloud/cloudtest"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

const startDoc = `# TODO

## Backlog

### One
<!-- id:aaaa -->

### Two
<!-- id:bbbb -->

## Done
`

type env struct {
	t   *testing.T
	srv *cloudtest.Server
	s   *Syncer
	ctx context.Context
}

func setup(t *testing.T, content string) *env {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := cloudtest.New()
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "TODO.md")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Syncer{
		Path: path, Store: &store.Store{Path: path},
		Client: cloud.New(srv.URL, srv.URL+"/api/v1", cloudtest.Token, "0.10.0"),
		Link:   &Link{Host: srv.URL}, Version: "0.10.0", FormatVersion: markdown.FormatVersion,
	}
	return &env{t: t, srv: srv, s: s, ctx: context.Background()}
}

func (e *env) create() {
	e.t.Helper()
	if _, err := e.s.CreateProject(e.ctx, "proj", "", ""); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) round() *Result {
	e.t.Helper()
	r, err := e.s.Round(e.ctx, Options{})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) local() *task.File {
	e.t.Helper()
	f, err := e.s.Store.Load()
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func (e *env) head() *task.File {
	e.t.Helper()
	f, err := markdown.Parse([]byte(e.srv.Head(e.s.Link.ProjectID)))
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func (e *env) mutate(fn func(f *task.File) error) {
	e.t.Helper()
	if err := e.s.Store.Mutate(fn); err != nil {
		e.t.Fatal(err)
	}
}

func find(f *task.File, id string) *task.Task {
	for _, t := range f.AllTasks() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func addTask(id, title string) func(f *task.File) error {
	return func(f *task.File) error {
		f.Boards[0].Tasks = append(f.Boards[0].Tasks, &task.Task{ID: id, Title: title})
		return nil
	}
}

func (e *env) sameAsHead() {
	e.t.Helper()
	if got, want := string(markdown.Write(e.local())), e.srv.Head(e.s.Link.ProjectID); got != want {
		e.t.Fatalf("local differs from head\nlocal:\n%s\nhead:\n%s", got, want)
	}
}

func TestCreateAndRoundTrip(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	if e.s.Link.BaseRev != 1 || e.s.Link.ProjectSlug != "walm/proj" {
		t.Fatalf("link = %+v", e.s.Link)
	}
	if r := e.round(); !r.Unchanged {
		t.Errorf("fresh link should be up to date: %+v", r)
	}

	e.mutate(addTask("cccc", "Three"))
	r := e.round()
	if len(r.Pushed) != 1 || r.Pushed[0].Type != "task_added" || len(r.Pulled) != 0 {
		t.Errorf("push round: pushed %v pulled %v", r.Pushed, r.Pulled)
	}
	if find(e.head(), "cccc") == nil {
		t.Fatal("task not online")
	}
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("sent %s form, want ops", last.Form)
	}

	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) {
		f.Boards[0].Tasks[0].Comments = append(f.Boards[0].Tasks[0].Comments, task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 6}, Text: "from the web"})
	})
	r = e.round()
	if len(r.Pulled) != 1 || r.Pulled[0].Type != "comment_added" {
		t.Errorf("pull round: pulled %v", r.Pulled)
	}
	e.sameAsHead()
	if e.s.Link.BaseRev != e.srv.Rev(e.s.Link.ProjectID) {
		t.Errorf("base rev %d, head %d", e.s.Link.BaseRev, e.srv.Rev(e.s.Link.ProjectID))
	}
	base, _ := LoadBase(e.s.Path)
	if string(base) != e.srv.Head(e.s.Link.ProjectID) {
		t.Error("sync-base.md isn't head")
	}
}

func TestConcurrentEditsConflict(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.mutate(func(f *task.File) error { f.Boards[0].Tasks[0].Title = "Ours"; return nil })
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Title = "Theirs" })
	r := e.round()
	if len(r.Conflicts) != 1 || r.Conflicts[0].Field != "title" {
		t.Fatalf("conflicts %+v", r.Conflicts)
	}
	one := find(e.local(), "aaaa")
	if one.Title != "Theirs" || len(one.Comments) != 1 || one.Comments[0].Author != "todomd-sync" ||
		!strings.Contains(one.Comments[0].Text, "Ours") {
		t.Errorf("task after conflict: %+v", one)
	}
	e.sameAsHead()
}

func TestLocalEditDuringRoundIsKept(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Title = "Web" })
	once := true
	e.srv.OnSync = func(map[string]any) {
		if once {
			once = false
			// Written while the round is in flight, after it read the file.
			if err := e.s.Store.Mutate(addTask("dddd", "Typed meanwhile")); err != nil {
				t.Error(err)
			}
		}
	}
	e.round()
	f := e.local()
	if find(f, "aaaa").Title != "Web" || find(f, "dddd") == nil {
		t.Fatalf("lost a side:\n%s", markdown.Write(f))
	}
	e.srv.OnSync = nil
	e.round()
	if find(e.head(), "dddd") == nil {
		t.Error("the meanwhile edit wasn't pushed on the next round")
	}
	e.sameAsHead()
}

func TestReadOnlyKeepsLocalEdits(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.srv.SetReadOnly(e.s.Link.ProjectID, true)
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[1].Title = "Web edit" })
	e.mutate(addTask("cccc", "Mine"))
	r := e.round()
	if !r.ReadOnly || len(r.Pushed) != 0 {
		t.Errorf("result %+v", r)
	}
	f := e.local()
	if find(f, "cccc") == nil || find(f, "bbbb").Title != "Web edit" {
		t.Fatalf("file:\n%s", markdown.Write(f))
	}
	if find(e.head(), "cccc") != nil {
		t.Error("a read-only edit reached the server")
	}
	if !e.s.Link.ReadOnly {
		t.Error("link not marked read-only")
	}
}

func TestBaseMismatchSwitchesToFullFile(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	// A base that differs from the server's copy of that revision.
	bad := strings.Replace(startDoc, "### Two", "### Two (edited locally)", 1)
	if err := SaveBase(e.s.Path, []byte(bad)); err != nil {
		t.Fatal(err)
	}
	e.mutate(addTask("cccc", "Three"))
	r := e.round()
	if !r.FullFile || e.s.Link.FullFile == nil || e.s.Link.FullFile.Reason != "base_mismatch" {
		t.Fatalf("full-file mode not entered: %+v / %+v", r, e.s.Link.FullFile)
	}
	if find(e.head(), "cccc") == nil {
		t.Error("push lost")
	}
	e.mutate(addTask("dddd", "Four"))
	e.round()
	calls := e.srv.Calls()
	if calls[len(calls)-1].Form != "content" {
		t.Error("full-file mode not used on the next round")
	}
	// A new CLI version lifts it.
	e.s.Version = "0.11.0"
	e.mutate(addTask("eeee", "Five"))
	e.round()
	calls = e.srv.Calls()
	if calls[len(calls)-1].Form != "ops" {
		t.Error("full-file mode survived a version change")
	}
}

func TestBaseGoneRetriesWithBaseContent(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.srv.Forget[1] = true
	e.mutate(addTask("cccc", "Three"))
	r := e.round()
	if find(e.head(), "cccc") == nil {
		t.Fatal("push lost")
	}
	if !r.FullFile || e.s.Link.FullFile != nil {
		t.Errorf("base_gone should retry in full form once, not switch modes: %+v %+v", r, e.s.Link.FullFile)
	}
	e.sameAsHead()
}

func TestBadChecksumFetchesDocument(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Title = "Web" })
	e.srv.BadSHA = true
	e.mutate(addTask("cccc", "Three"))
	e.round()
	if e.s.Link.FullFile == nil {
		t.Error("checksum failure didn't switch to full-file mode")
	}
	e.sameAsHead()
}

func TestMissingFileIsRestoredNotDeleted(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	if err := os.Remove(e.s.Path); err != nil {
		t.Fatal(err)
	}
	r := e.round()
	if !r.Restored {
		t.Error("not reported as restored")
	}
	e.sameAsHead()
	if len(e.head().AllTasks()) != 2 {
		t.Error("tasks were deleted online")
	}
}

func TestConflictMarkersRefused(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	n := len(e.srv.Calls())
	text := strings.Replace(startDoc, "### Two\n<!-- id:bbbb -->\n", "### Two\n<!-- id:bbbb -->\n\n<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> other\n", 1)
	if err := os.WriteFile(e.s.Path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := e.s.Round(e.ctx, Options{})
	var cm *ConflictMarkersError
	if !errors.As(err, &cm) || cm.Line == 0 {
		t.Fatalf("got %v", err)
	}
	if len(e.srv.Calls()) != n {
		t.Error("a file with conflict markers was sent")
	}
}

func TestHandAddedTaskGetsIDBeforePush(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	data, _ := os.ReadFile(e.s.Path)
	if err := os.WriteFile(e.s.Path, append(data, "\n### Typed by hand\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	e.round()
	e.round() // must not duplicate the task
	n := 0
	for _, x := range e.head().AllTasks() {
		if x.Title == "Typed by hand" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("hand-added task appears %d times online", n)
	}
	e.sameAsHead()
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitImportNeverDeletesOnline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	e := setup(t, startDoc)
	dir := filepath.Dir(e.s.Path)
	git(t, dir, "init", "-q")
	git(t, dir, "add", "TODO.md")
	git(t, dir, "commit", "-qm", "one")
	e.s.Link.Git = GitTrack
	e.create()
	if e.s.Link.GitHead == "" {
		t.Fatal("git head not recorded")
	}
	// A checkout of an older board: bbbb is missing, and HEAD moved.
	e.mutate(func(f *task.File) error {
		f.Boards[0].Tasks = f.Boards[0].Tasks[:1]
		f.Boards[0].Tasks = append(f.Boards[0].Tasks, &task.Task{ID: "nnnn", Title: "On the branch"})
		return nil
	})
	git(t, dir, "commit", "-qam", "branch state")
	r := e.round()
	if !r.Import {
		t.Error("not sent as an import")
	}
	calls := e.srv.Calls()
	if !calls[len(calls)-1].Import {
		t.Error("import flag missing on the wire")
	}
	h := e.head()
	if find(h, "bbbb") == nil || find(h, "nnnn") == nil {
		t.Fatalf("head after import:\n%s", markdown.Write(h))
	}
	e.sameAsHead()
	// Without a HEAD move, a deletion is a deletion.
	e.mutate(func(f *task.File) error { f.Boards[0].Tasks = f.Boards[0].Tasks[1:]; return nil })
	if r := e.round(); r.Import {
		t.Error("plain edit sent as an import")
	}
	if find(e.head(), "aaaa") != nil {
		t.Error("a normal delete didn't reach the server")
	}
}

func TestFirstLinkModes(t *testing.T) {
	online := "# TODO\n\n## Backlog\n\n### Online\n<!-- id:oooo -->\n\n## Done\n"
	for _, mode := range []string{LinkMerge, LinkReplaceLocal, LinkReplaceRemote} {
		t.Run(mode, func(t *testing.T) {
			e := setup(t, startDoc)
			p := e.srv.AddProject("existing", online)
			*e.s.Link = *NewLink(e.srv.URL, cloud.Project{ID: p.ID, Slug: p.Slug})
			prev, err := e.s.PreviewFirst(e.ctx, mode)
			if err != nil {
				t.Fatal(err)
			}
			if mode == LinkMerge && len(prev.FromOnline.Pulled) != 1 {
				t.Errorf("preview: %+v", prev.FromOnline.Pulled)
			}
			if e.srv.Rev(p.ID) != 1 {
				t.Fatal("preview changed the project")
			}
			if _, err := e.s.FirstLink(e.ctx, mode); err != nil {
				t.Fatal(err)
			}
			f := e.local()
			hasOnline, hasLocal := find(f, "oooo") != nil, find(f, "aaaa") != nil
			switch mode {
			case LinkMerge:
				if !hasOnline || !hasLocal {
					t.Errorf("merge lost a side:\n%s", markdown.Write(f))
				}
			case LinkReplaceLocal:
				if !hasOnline || hasLocal {
					t.Errorf("replace-local:\n%s", markdown.Write(f))
				}
			case LinkReplaceRemote:
				if hasOnline || !hasLocal {
					t.Errorf("replace-remote:\n%s", markdown.Write(f))
				}
			}
			e.sameAsHead()
			if r := e.round(); !r.Unchanged {
				t.Errorf("not up to date after linking: %+v", r)
			}
		})
	}
}

func TestFirstLinkWithoutFileWritesIt(t *testing.T) {
	e := setup(t, "")
	p := e.srv.AddProject("existing", startDoc)
	*e.s.Link = *NewLink(e.srv.URL, cloud.Project{ID: p.ID, Slug: p.Slug})
	if _, err := e.s.FirstLink(e.ctx, LinkMerge); err != nil {
		t.Fatal(err)
	}
	e.sameAsHead()
}

func TestWatcherBothWays(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	w := &Watcher{Syncer: e.s, PollEvery: 50 * time.Millisecond, Debounce: 50 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if ok() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	time.Sleep(100 * time.Millisecond)
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Title = "From the web" })
	waitFor("web edit to land locally", func() bool {
		f, err := e.s.Store.Load()
		return err == nil && find(f, "aaaa").Title == "From the web"
	})
	e.mutate(addTask("cccc", "From here"))
	waitFor("local edit to reach the server", func() bool { return find(e.head(), "cccc") != nil })

	e.srv.Revoke()
	select {
	case err := <-done:
		if !errors.Is(err, ErrAccessRevoked) {
			t.Errorf("watcher ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher didn't stop on access_revoked")
	}
}

func TestWatchLockIsExclusive(t *testing.T) {
	e := setup(t, startDoc)
	l1, _, err := AcquireWatchLock(e.s.Path)
	if err != nil || l1 == nil {
		t.Fatalf("first lock: %v", err)
	}
	defer l1.Release()
	l2, pid, err := AcquireWatchLock(e.s.Path)
	if err != nil || l2 != nil || pid != os.Getpid() {
		t.Fatalf("second lock: %v %v %d", l2, err, pid)
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:walm/todomd.git":         "github.com/walm/todomd",
		"https://github.com/walm/todomd":         "github.com/walm/todomd",
		"https://user@GitHub.com:443/Walm/X.git": "github.com/walm/x",
		"ssh://git@gitlab.com/a/b/c.git":         "gitlab.com/a/b/c",
		"/local/path":                            "",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinksSyncAsOps(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.mutate(func(f *task.File) error {
		f.Boards[0].Tasks[0].Links = []task.Link{{Kind: task.LinkBlocks, ID: "zzzz"}}
		return nil
	})
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" || !strings.Contains(fmt.Sprint(last.Raw["ops"]), "links") {
		t.Errorf("sent %s form: %v", last.Form, last.Raw["ops"])
	}
	e.sameAsHead()
	if !strings.Contains(e.srv.Head(e.s.Link.ProjectID), "\n#### Links\n\n- blocks zzzz\n") {
		t.Errorf("head:\n%s", e.srv.Head(e.s.Link.ProjectID))
	}
}

// A server on format 1 can't read links in ops, so a change that carries
// links goes as the whole file, which it keeps as description text.
func TestLinksToAnOldServerSendTheWholeFile(t *testing.T) {
	e := setup(t, startDoc)
	e.s.FormatVersion = 1
	e.create()
	e.mutate(addTask("cccc", "Three"))
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("no links: sent %s form, want ops", last.Form)
	}
	e.mutate(func(f *task.File) error {
		f.Boards[0].Tasks[0].Links = []task.Link{{Kind: task.LinkRelated, ID: "cccc"}}
		return nil
	})
	r := e.round()
	calls = e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "content" || !r.FullFile {
		t.Errorf("links: sent %s form, want content", last.Form)
	}
	if e.s.Link.FullFile != nil {
		t.Error("the link switched to full-file mode for good")
	}
	e.sameAsHead()
}

func TestStickersSyncAsOps(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.mutate(func(f *task.File) error {
		f.Boards[0].Tasks[0].Sticker = "fire"
		return nil
	})
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" || !strings.Contains(fmt.Sprint(last.Raw["ops"]), "sticker") {
		t.Errorf("sent %s form: %v", last.Form, last.Raw["ops"])
	}
	e.sameAsHead()
	if !strings.Contains(e.srv.Head(e.s.Link.ProjectID), "\n\n<!-- sticker: fire -->\n") {
		t.Errorf("head:\n%s", e.srv.Head(e.s.Link.ProjectID))
	}
}

// A server on format 2 can't read stickers in ops, and holds a sticker line
// as description text, which a description op would overwrite. While the
// file has stickers, every change goes as the whole file.
func TestStickersToAnOldServerSendTheWholeFile(t *testing.T) {
	e := setup(t, startDoc)
	e.s.FormatVersion = 2
	e.create()
	e.mutate(addTask("cccc", "Three"))
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("no stickers: sent %s form, want ops", last.Form)
	}
	steps := []func(f *task.File){
		func(f *task.File) { f.Boards[0].Tasks[0].Sticker = "star" },         // sets one
		func(f *task.File) { f.Boards[0].Tasks[0].Description = "Changed." }, // edits a stickered card
		func(f *task.File) { f.Boards[0].Tasks[1].Title = "Other card" },     // edits another card
		func(f *task.File) { f.Boards[0].Tasks[0].Sticker = "" },             // removes the last one
	}
	for i, step := range steps {
		e.mutate(func(f *task.File) error { step(f); return nil })
		r := e.round()
		calls = e.srv.Calls()
		if last := calls[len(calls)-1]; last.Form != "content" || !r.FullFile {
			t.Errorf("step %d: sent %s form, want content", i, last.Form)
		}
		e.sameAsHead()
	}
	if e.s.Link.FullFile != nil {
		t.Error("the link switched to full-file mode for good")
	}
	e.mutate(func(f *task.File) error { f.Boards[0].Tasks[1].Title = "Again"; return nil })
	e.round()
	calls = e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("no stickers left: sent %s form, want ops", last.Form)
	}
}

func TestReactionsSyncAsOps(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.mutate(func(f *task.File) error {
		x := f.Boards[0].Tasks[0]
		x.Comments = append(x.Comments, task.Comment{Author: "anna", Date: task.Today(), Text: "Look?"})
		_, err := x.Comments[len(x.Comments)-1].React("👍", "walm", false)
		return err
	})
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" || !strings.Contains(fmt.Sprint(last.Raw["ops"]), "reactions") {
		t.Errorf("sent %s form: %v", last.Form, last.Raw["ops"])
	}
	e.sameAsHead()
	if !strings.Contains(e.srv.Head(e.s.Link.ProjectID), "Look?\n  <!-- reactions: 👍 walm -->\n") {
		t.Errorf("head:\n%s", e.srv.Head(e.s.Link.ProjectID))
	}
}

// A server on format 3 can't read reactions in ops, and holds a reactions
// line as the comment's last text line. While the file has reactions, every
// change goes as the whole file.
func TestReactionsToAnOldServerSendTheWholeFile(t *testing.T) {
	e := setup(t, startDoc)
	e.s.FormatVersion = 3
	e.create()
	e.mutate(func(f *task.File) error {
		x := f.Boards[0].Tasks[0]
		x.Comments = append(x.Comments, task.Comment{Author: "anna", Date: task.Today(), Text: "Look?"})
		return nil
	})
	e.round()
	calls := e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("no reactions: sent %s form, want ops", last.Form)
	}
	react := func(remove bool) func(f *task.File) {
		return func(f *task.File) {
			if _, err := f.Boards[0].Tasks[0].Comments[0].React("👀", "agent", remove); err != nil {
				t.Fatal(err)
			}
		}
	}
	steps := []func(f *task.File){
		react(false), // adds one
		func(f *task.File) { f.Boards[0].Tasks[1].Title = "Other card" }, // edits another card
		react(true), // removes the last one
	}
	for i, step := range steps {
		e.mutate(func(f *task.File) error { step(f); return nil })
		r := e.round()
		calls = e.srv.Calls()
		if last := calls[len(calls)-1]; last.Form != "content" || !r.FullFile {
			t.Errorf("step %d: sent %s form, want content", i, last.Form)
		}
		e.sameAsHead()
	}
	if e.s.Link.FullFile != nil {
		t.Error("the link switched to full-file mode for good")
	}
	e.mutate(func(f *task.File) error { f.Boards[0].Tasks[1].Title = "Again"; return nil })
	e.round()
	calls = e.srv.Calls()
	if last := calls[len(calls)-1]; last.Form != "ops" {
		t.Errorf("no reactions left: sent %s form, want ops", last.Form)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/walm/todomd/internal/cloud/cloudtest"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

// cloudEnv is a fake todomd.online plus isolated config/state dirs and a
// todo file, with TODOMD_HOST pointing at the fake.
func cloudEnv(t *testing.T) (*cloudtest.Server, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TODOMD_TOKEN", "")
	srv := cloudtest.New()
	t.Cleanup(srv.Close)
	t.Setenv("TODOMD_HOST", srv.URL)
	// Resolved, so the path matches what os.Getwd reports (macOS temp dirs
	// sit behind the /var → /private/var symlink).
	path, err := filepath.EvalSymlinks(testFile(t))
	if err != nil {
		t.Fatal(err)
	}
	return srv, path
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, _ := os.Pipe()
	w.WriteString(input)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}

func login(t *testing.T) {
	t.Helper()
	withStdin(t, cloudtest.Token+"\n")
	if out, err := run(t, "login", "--with-token"); err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
}

func TestLoginDeviceFlowAndWhoami(t *testing.T) {
	cloudEnv(t)
	out, err := run(t, "login", "--no-browser")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "WDJB-MJHT") || !strings.Contains(out, "Logged in to") || !strings.Contains(out, "walm") {
		t.Errorf("login output:\n%s", out)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "todomd", "hosts.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("hosts.json: %v %v", info, err)
	}

	out, err = run(t, "whoami", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var who struct {
		User  struct{ Handle string }
		Token struct{ Kind, Prefix string }
	}
	if err := json.Unmarshal([]byte(out), &who); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if who.User.Handle != "walm" || who.Token.Kind != "device" || strings.Contains(out, cloudtest.Token) {
		t.Errorf("whoami: %s", out)
	}

	if _, err := run(t, "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "whoami"); classify(err) != 4 {
		t.Errorf("whoami after logout: exit %d (%v)", classify(err), err)
	}
}

func TestLinkCreateSyncAndExitCodes(t *testing.T) {
	srv, path := cloudEnv(t)

	// Not logged in → 4; not linked → 5.
	if _, err := run(t, "--file", path, "link", "--create"); classify(err) != 4 {
		t.Errorf("link while logged out: exit %d (%v)", classify(err), err)
	}
	login(t)
	if _, err := run(t, "--file", path, "sync", "--json"); classify(err) != 5 {
		t.Errorf("sync unlinked: exit %d (%v)", classify(err), err)
	}

	id := addOne(t, path, "First task")
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var linked struct {
		Project struct{ ID, Slug string }
		Rev     int
	}
	if err := json.Unmarshal([]byte(out), &linked); err != nil || linked.Project.Slug != "walm/proj" || linked.Rev != 1 {
		t.Fatalf("link output %s (%v)", out, err)
	}
	if !strings.Contains(srv.Head(linked.Project.ID), "First task") {
		t.Fatal("task not online after create")
	}
	if _, err := run(t, "--file", path, "link", "--create"); err == nil {
		t.Error("linking twice should fail")
	}

	// Push.
	if _, err := run(t, "--file", path, "comment", id, "--author", "ai", "pushed from the CLI"); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, "--file", path, "sync", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var res syncJSON
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(res.Pushed) != 1 || res.Pushed[0].Type != "comment_added" || res.Rev != 2 {
		t.Errorf("sync output %s", out)
	}

	// Pull a web edit.
	srv.Edit(linked.Project.ID, func(f *task.File) { f.Boards[0].Tasks[0].Title = "Renamed on the web" })
	out, err = run(t, "--file", path, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	res = syncJSON{}
	json.Unmarshal([]byte(out), &res)
	if len(res.Pulled) != 1 || res.Pulled[0].Type != "task_updated" {
		t.Errorf("pull output %s", out)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Renamed on the web") {
		t.Error("web edit not in the file")
	}

	// A deleted file is restored, found through its link.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	os.Chdir(filepath.Dir(path))
	out, err = run(t, "sync", "--json")
	os.Chdir(wd)
	if err != nil || !strings.Contains(out, `"restored": true`) {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "Renamed on the web") {
		t.Error("restored file lacks head's content")
	}

	// Up to date.
	out, _ = run(t, "--file", path, "sync")
	if !strings.Contains(out, "up to date") {
		t.Errorf("idle sync: %s", out)
	}

	// Status.
	out, err = run(t, "--file", path, "sync", "status", "--json", "--remote")
	if err != nil || !strings.Contains(out, `"head_rev": 3`) || !strings.Contains(out, `"attachments": "`+filepath.Join(os.Getenv("XDG_STATE_HOME"), "todomd", "attachments", strings.TrimPrefix(srv.URL, "http://"), linked.Project.ID)+`"`) {
		t.Errorf("status: %v %s", err, out)
	}

	// Unreachable → 6; too old → 7; revoked → 4.
	srv.Down = true
	if _, err := run(t, "--file", path, "sync"); classify(err) != 6 {
		t.Errorf("sync while down: exit %d (%v)", classify(err), err)
	}
	srv.Down = false
	srv.MinCLI = "99.0.0"
	old := version
	version = "v0.10.0"
	if _, err := run(t, "--file", path, "sync"); classify(err) != 7 {
		t.Errorf("sync too old: exit %d (%v)", classify(err), err)
	}
	version = old
	srv.MinCLI = "0.0.1"
	srv.Revoke()
	if _, err := run(t, "--file", path, "sync"); classify(err) != 4 {
		t.Errorf("sync revoked: exit %d (%v)", classify(err), err)
	}

	if _, err := run(t, "--file", path, "unlink"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--file", path, "sync", "status"); classify(err) != 5 {
		t.Errorf("status after unlink: exit %d", classify(err))
	}
}

func TestLinkExistingNeedsYesOffTerminal(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	addOne(t, path, "Local task")
	p := srv.AddProject("existing", "# TODO\n\n## Backlog\n\n### Online task\n<!-- id:oooo -->\n")
	if _, err := run(t, "--file", path, "link", "--project", p.Slug); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a --yes refusal, got %v", err)
	}
	if srv.Rev(p.ID) != 1 {
		t.Fatal("a refused link changed the project")
	}
	if out, err := run(t, "--file", path, "link", "--project", p.Slug, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	f, _ := markdown.Parse([]byte(srv.Head(p.ID)))
	if len(f.AllTasks()) != 2 {
		t.Errorf("merged head:\n%s", srv.Head(p.ID))
	}
}

func TestChangesWaitSeesWebEdit(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	addOne(t, path, "Task")
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var linked struct{ Project struct{ ID string } }
	json.Unmarshal([]byte(out), &linked)
	if _, err := run(t, "--file", path, "changes", "--as", "agent"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		srv.Edit(linked.Project.ID, func(f *task.File) {
			f.Boards[0].Tasks[0].Comments = append(f.Boards[0].Tasks[0].Comments,
				task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 6}, Text: "reply from the web"})
		})
	}()
	start := time.Now()
	out, err = run(t, "--file", path, "changes", "--as", "agent", "--wait", "--timeout", "20s", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "reply from the web") || strings.Contains(out, "timed_out") {
		t.Errorf("changes --wait output:\n%s", out)
	}
	if time.Since(start) > 15*time.Second {
		t.Error("took too long")
	}

	out, err = run(t, "--file", path, "changes", "--as", "agent", "--wait", "--timeout", "1s", "--json")
	if err != nil || !strings.Contains(out, `"timed_out": true`) {
		t.Errorf("timeout: %v %s", err, out)
	}
}

func pngBytes(seed string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte("image "+seed)...)
}

func TestAttachSyncsImages(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	id := addOne(t, path, "Task with a screenshot")
	img := filepath.Join(t.TempDir(), "Screen Shot.png")
	os.WriteFile(img, pngBytes("cli"), 0o644)

	// Unlinked: exit 5, nothing copied.
	if _, err := run(t, "--file", path, "attach", id, img); classify(err) != 5 {
		t.Fatalf("attach unlinked: exit %d (%v)", classify(err), err)
	}
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var linked struct{ Project struct{ ID string } }
	json.Unmarshal([]byte(out), &linked)

	notImage := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(notImage, []byte("hello"), 0o644)
	if _, err := run(t, "--file", path, "attach", id, notImage); err == nil {
		t.Error("attached a text file")
	}

	if out, err := run(t, "--file", path, "attach", id, img); err != nil || !strings.Contains(out, "Screen-Shot.png") {
		t.Fatalf("attach: %v %s", err, out)
	}
	data, _ := os.ReadFile(path)
	// Hosts other than todomd.online get their own folder under the root.
	hostDir := strings.TrimPrefix(srv.URL, "http://")
	if !strings.Contains(string(data), "![Screen-Shot](") || !strings.Contains(string(data), "/todomd/attachments/"+hostDir+"/"+linked.Project.ID+"/att_") {
		t.Fatalf("file after attach:\n%s", data)
	}
	out, err = run(t, "--file", path, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res syncJSON
	json.Unmarshal([]byte(out), &res)
	if len(res.Attachments.Uploaded) != 1 || srv.Uploads != 1 {
		t.Fatalf("sync: %s", out)
	}
	if head := srv.Head(linked.Project.ID); !strings.Contains(head, srv.URL+"/a/"+linked.Project.ID+"/att_") {
		t.Errorf("head has no image URL:\n%s", head)
	}

	// An image pasted on the web comes down as a local path.
	url := srv.AddImage(linked.Project.ID, pngBytes("web"), "pasted.png")
	srv.Edit(linked.Project.ID, func(f *task.File) {
		f.Boards[0].Tasks[0].Comments = append(f.Boards[0].Tasks[0].Comments,
			task.Comment{Author: "walm", Date: task.Date{Year: 2026, Month: 11, Day: 6}, Text: "![pasted](" + url + ")"})
	})
	out, err = run(t, "--file", path, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	res = syncJSON{}
	json.Unmarshal([]byte(out), &res)
	data, _ = os.ReadFile(path)
	if len(res.Attachments.Downloaded) != 1 || strings.Contains(string(data), url) {
		t.Fatalf("download: %s\nfile:\n%s", out, data)
	}
	if out, _ := run(t, "--file", path, "sync"); !strings.Contains(out, "up to date") {
		t.Errorf("not idle after download: %s", out)
	}
}

func TestLinkBringsOverTodomdWebImages(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	id := addOne(t, path, "Task")
	webDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "todomd-web", "attachments", "9c1fa3b2d4e5f607", id)
	os.MkdirAll(webDir, 0o755)
	webImg := filepath.Join(webDir, "header-ios.png")
	os.WriteFile(webImg, pngBytes("todomd-web"), 0o644)
	if _, err := run(t, "--file", path, "update", id, "--desc", "![header-ios.png]("+webImg+")"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--yes", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "todomd-web/attachments") || !strings.Contains(string(data), "/todomd/attachments/") {
		t.Fatalf("link not rewritten:\n%s", data)
	}
	if srv.Uploads != 1 {
		t.Errorf("uploads = %d", srv.Uploads)
	}
}

// Board commands need no new sync ops: a dry run sends (and shows) the
// existing board ops, and a board-only change no longer reads as "up to
// date" in either direction.
func TestSyncBoardCommands(t *testing.T) {
	srv, path := cloudEnv(t)
	login(t)
	addOne(t, path, "one", "--board", "Backlog")
	out, err := run(t, "--file", path, "link", "--create", "--name", "proj", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var linked struct{ Project struct{ ID string } }
	if err := json.Unmarshal([]byte(out), &linked); err != nil {
		t.Fatal(err)
	}

	type opJSON struct {
		Op     string   `json:"op"`
		Board  string   `json:"board"`
		Name   string   `json:"name"`
		After  *string  `json:"after"`
		Boards []string `json:"boards"`
	}
	kinds := func(list []opJSON) string {
		var k []string
		for _, op := range list {
			k = append(k, op.Op)
		}
		return strings.Join(k, ",")
	}
	// dryRun runs `sync --dry-run --json` and returns its result and the ops
	// it sent to the server.
	dryRun := func() (syncJSON, []opJSON) {
		t.Helper()
		out, err := run(t, "--file", path, "sync", "--dry-run", "--json")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		var res syncJSON
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		call := srv.Requests[len(srv.Requests)-1]
		if !res.DryRun || !call.DryRun || call.Form != "ops" {
			t.Fatalf("want a dry-run ops request, got %+v\n%s", call, out)
		}
		data, _ := json.Marshal(call.Raw["ops"])
		var sent []opJSON
		if err := json.Unmarshal(data, &sent); err != nil {
			t.Fatal(err)
		}
		return res, sent
	}
	syncNow := func() string {
		t.Helper()
		out, err := run(t, "--file", path, "sync")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return out
	}
	board := func(args ...string) {
		t.Helper()
		if out, err := run(t, append([]string{"--file", path, "boards"}, args...)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// add → board.add, before Done.
	board("add", "Review")
	res, sent := dryRun()
	if kinds(sent) != "board.add" || sent[0].Board != "Review" || sent[0].After == nil || *sent[0].After != "In Progress" {
		t.Errorf("add sent %+v", sent)
	}
	if len(res.Pushed) != 0 || len(res.PushedBoard) != 1 || res.PushedBoard[0].Op != "board.add" {
		t.Errorf("add dry run: pushed %+v boards %+v", res.Pushed, res.PushedBoard)
	}
	if out, _ := run(t, "--file", path, "sync", "--dry-run"); !strings.HasPrefix(out, "Would sync: pushed 1 board change ·") {
		t.Errorf("dry run text: %q", out)
	}
	if strings.Contains(srv.Head(linked.Project.ID), "Review") {
		t.Fatal("a dry run must not change the project")
	}
	if out := syncNow(); !strings.HasPrefix(out, "Synced: pushed 1 board change ·") {
		t.Errorf("sync text: %q", out)
	}

	// move → board.order with the complete order.
	board("move", "Done", "--to", "1")
	if _, sent := dryRun(); kinds(sent) != "board.order" || strings.Join(sent[0].Boards, ",") != "Done,Backlog,In Progress,Review" {
		t.Errorf("move sent %+v", sent)
	}
	syncNow()

	// rename → board.add, a task.move per task, board.delete.
	board("rename", "Backlog", "Todo")
	res, sent = dryRun()
	if kinds(sent) != "board.add,task.move,board.delete" || sent[0].Board != "Todo" || sent[2].Board != "Backlog" {
		t.Errorf("rename sent %+v", sent)
	}
	if len(res.Pushed) != 1 || res.Pushed[0].Type != "task_moved" || len(res.PushedBoard) != 2 {
		t.Errorf("rename dry run: pushed %+v boards %+v", res.Pushed, res.PushedBoard)
	}
	if out := syncNow(); !strings.HasPrefix(out, "Synced: pushed 1 change, 2 board changes ·") {
		t.Errorf("rename sync text: %q", out)
	}

	// Case only → board.spelling.
	board("rename", "Todo", "TODO")
	if _, sent := dryRun(); kinds(sent) != "board.spelling" || sent[0].Board != "Todo" || sent[0].Name != "TODO" {
		t.Errorf("case-only rename sent %+v", sent)
	}
	syncNow()

	head := srv.Head(linked.Project.ID)
	if !regexp.MustCompile(`(?s)## Done.*## TODO\n\n### one.*## In Progress.*## Review`).MatchString(head) || strings.Contains(head, "Backlog") {
		t.Errorf("project after the board commands:\n%s", head)
	}

	// A reorder made online is reported as pulled, not "up to date".
	srv.Edit(linked.Project.ID, func(f *task.File) {
		f.Boards[0], f.Boards[1] = f.Boards[1], f.Boards[0]
	})
	out, err = run(t, "--file", path, "sync", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var pulled syncJSON
	if err := json.Unmarshal([]byte(out), &pulled); err != nil {
		t.Fatal(err)
	}
	if len(pulled.PulledBoard) != 1 || pulled.PulledBoard[0].Op != "board.order" || len(pulled.Pulled) != 0 {
		t.Errorf("pulled reorder: %s", out)
	}
	if !strings.Contains(out, `"pushed_board_ops": []`) {
		t.Errorf("pushed_board_ops should be an empty list, not absent: %s", out)
	}
}

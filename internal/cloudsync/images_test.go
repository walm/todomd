package cloudsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

func png(seed string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake image body "+seed)...)
}

func setDesc(id, desc string) func(f *task.File) error {
	return func(f *task.File) error {
		for _, t := range f.AllTasks() {
			if t.ID == id {
				t.Description = desc
			}
		}
		return nil
	}
}

func (e *env) fileText() string {
	e.t.Helper()
	data, err := os.ReadFile(e.s.Path)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(data)
}

func TestImageLinksConvertBothWays(t *testing.T) {
	for _, dir := range []string{"plain", "with space"} {
		t.Run(dir, func(t *testing.T) {
			e := setup(t, startDoc)
			state := filepath.Join(t.TempDir(), dir)
			t.Setenv("XDG_STATE_HOME", state)
			e.create()
			im, err := newImages(e.s.Client, e.s.Link, &AttachmentReport{})
			if err != nil {
				t.Fatal(err)
			}
			data := png(dir)
			att, name, link, err := StoreImage(e.s.Link, data, "Screen Shot.PNG")
			if err != nil {
				t.Fatal(err)
			}
			if name != "Screen-Shot.png" {
				t.Errorf("stored as %q", name)
			}
			if strings.Contains(state, " ") != strings.HasPrefix(link, "<") {
				t.Errorf("link %q: angle brackets exactly when the path has a space", link)
			}
			url := im.URL(att, name)
			wire := &task.File{Boards: []*task.Board{{Name: "B", Tasks: []*task.Task{{ID: "aaaa", Title: "a",
				Description: "see ![shot](" + url + ") and " + url,
				Comments:    []task.Comment{{Author: "x", Date: task.Date{Year: 2026, Month: 1, Day: 1}, Text: "![s](" + url + ")"}}}}}}}
			local := im.toLocal(wire)
			if got := local.Boards[0].Tasks[0].Description; strings.Contains(got, url) || !strings.Contains(got, im.path(att, name)) {
				t.Fatalf("toLocal: %q", got)
			}
			back := im.toWire(context.Background(), local, im.refs(wire), wireOffline)
			if string(markdown.Write(back)) != string(markdown.Write(wire)) {
				t.Fatalf("round trip changed the text:\n%s\n%s", markdown.Write(back), markdown.Write(wire))
			}
		})
	}
}

func TestAttachedImageIsUploaded(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	data := png("attach")
	att, _, link, err := StoreImage(e.s.Link, data, "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	e.mutate(setDesc("aaaa", "![shot]("+link+")"))
	r := e.round()
	if len(r.Attachments.Uploaded) != 1 || !e.srv.HasImage(e.s.Link.ProjectID, att) {
		t.Fatalf("not uploaded: %+v", r.Attachments)
	}
	head := e.srv.Head(e.s.Link.ProjectID)
	if !strings.Contains(head, e.srv.URL+"/a/"+e.s.Link.ProjectID+"/"+att+"/shot.png") || strings.Contains(head, "todomd/attachments") {
		t.Fatalf("head has no URL (or still the path):\n%s", head)
	}
	if !strings.Contains(e.fileText(), link) {
		t.Error("the local file lost its path")
	}
	if r := e.round(); !r.Unchanged || e.srv.Uploads != 1 {
		t.Errorf("second round not idle: %+v, uploads %d", r, e.srv.Uploads)
	}
}

func TestWebImageIsDownloadedAndStable(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	data := png("web")
	url := e.srv.AddImage(e.s.Link.ProjectID, data, "pasted image.png")
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Description = "![pasted](" + url + ")" })
	r := e.round()
	if len(r.Attachments.Downloaded) != 1 {
		t.Fatalf("not downloaded: %+v", r.Attachments)
	}
	att := cloud.ImageID(data)
	text := e.fileText()
	if strings.Contains(text, url) || !strings.Contains(text, "/todomd/attachments/") {
		t.Fatalf("file:\n%s", text)
	}
	root, _ := AttachmentRoot(e.s.Link.Host)
	got, err := os.ReadFile(filepath.Join(root, e.s.Link.ProjectID, att, "pasted-image.png"))
	if err != nil || string(got) != string(data) {
		t.Fatalf("image on disk: %v", err)
	}
	for i := 0; i < 2; i++ {
		if r := e.round(); !r.Unchanged {
			t.Fatalf("round %d after download not idle: pushed %v", i, r.Pushed)
		}
	}
	if e.srv.Downloads != 1 {
		t.Errorf("downloaded %d times", e.srv.Downloads)
	}
}

func TestForeignPathConvertsAndDownloads(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	data := png("other machine")
	url := e.srv.AddImage(e.s.Link.ProjectID, data, "x.png")
	att := cloud.ImageID(data)
	foreign := "/home/someone/.local/state/todomd/attachments/" + e.s.Link.ProjectID + "/" + att + "/x.png"
	e.mutate(setDesc("bbbb", "![x]("+foreign+")"))
	e.round()
	if head := e.srv.Head(e.s.Link.ProjectID); !strings.Contains(head, url) || strings.Contains(head, "/home/someone") {
		t.Fatalf("head:\n%s", head)
	}
	if text := e.fileText(); strings.Contains(text, "/home/someone") || !strings.Contains(text, att) {
		t.Fatalf("file still has the foreign path:\n%s", text)
	}
}

func TestUploadRuleRefusesEscapes(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	root, _ := AttachmentRoot(e.s.Link.Host)
	dir := filepath.Join(root, e.s.Link.ProjectID)

	// A symlink in the folder pointing at a file outside it.
	secret := png("secret")
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, secret, 0o644)
	att := cloud.ImageID(secret)
	os.MkdirAll(filepath.Join(dir, att), 0o755)
	if err := os.Symlink(outside, filepath.Join(dir, att, "secret.png")); err != nil {
		t.Fatal(err)
	}
	// A file whose content doesn't match its id.
	wrong := "att_00000000000000000000"
	os.MkdirAll(filepath.Join(dir, wrong), 0o755)
	os.WriteFile(filepath.Join(dir, wrong, "w.png"), png("mismatch"), 0o644)

	text := "![a](" + filepath.Join(dir, att, "secret.png") + ")\n![b](" + filepath.Join(dir, wrong, "w.png") + ")\n![c](" + outside + ")"
	e.mutate(setDesc("aaaa", text))
	r := e.round()
	if e.srv.Uploads != 0 || len(r.Attachments.Uploaded) != 0 {
		t.Fatalf("uploaded something: %+v", r.Attachments)
	}
	if len(r.Attachments.Skipped) != 2 {
		t.Errorf("skipped %+v", r.Attachments.Skipped)
	}
	reasons := r.Attachments.Skipped[0].Reason + " | " + r.Attachments.Skipped[1].Reason
	if !strings.Contains(reasons, "outside") || !strings.Contains(reasons, "match") {
		t.Errorf("reasons: %s", reasons)
	}
}

func TestNoAttachmentsMovesNoBytes(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.s.NoAttachments = true
	url := e.srv.AddImage(e.s.Link.ProjectID, png("web"), "w.png")
	e.srv.Edit(e.s.Link.ProjectID, func(f *task.File) { f.Boards[0].Tasks[0].Description = "![w](" + url + ")" })
	_, _, link, _ := StoreImage(e.s.Link, png("local"), "l.png")
	e.mutate(setDesc("bbbb", "![l]("+link+")"))
	r := e.round()
	if e.srv.Uploads != 0 || e.srv.Downloads != 0 || !r.Attachments.empty() {
		t.Fatalf("moved bytes: uploads %d downloads %d %+v", e.srv.Uploads, e.srv.Downloads, r.Attachments)
	}
	if !strings.Contains(e.fileText(), url) {
		t.Error("the web image should stay a URL locally")
	}
}

func TestDryRunUploadsNothing(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	_, _, link, _ := StoreImage(e.s.Link, png("dry"), "d.png")
	e.mutate(setDesc("aaaa", "![d]("+link+")"))
	r, err := e.s.Round(e.ctx, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if e.srv.Uploads != 0 || len(r.Attachments.Uploaded) != 1 {
		t.Fatalf("dry run: uploads %d, report %+v", e.srv.Uploads, r.Attachments)
	}
}

func TestReadOnlyImageStaysLocal(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	e.srv.SetReadOnly(e.s.Link.ProjectID, true)
	_, _, link, _ := StoreImage(e.s.Link, png("ro"), "r.png")
	e.mutate(setDesc("aaaa", "![r]("+link+")"))
	r := e.round()
	if e.srv.Uploads != 0 || len(r.Attachments.Skipped) != 1 || !strings.Contains(r.Attachments.Skipped[0].Reason, "read-only") {
		t.Fatalf("%+v", r.Attachments)
	}
	if !strings.Contains(e.fileText(), link) {
		t.Error("local edit lost")
	}
}

// Files that aren't images (todomd.online takes any file now) stay links:
// never downloaded, never reported. An image is still fetched, even from a
// plain link, and a file that only has an image's name is refused by the
// server (406) and left alone just as quietly.
func TestFileLinksStayLinks(t *testing.T) {
	e := setup(t, startDoc)
	e.create()
	p := e.s.Link.ProjectID
	img := e.srv.AddImage(p, png("plain link"), "shot.png")
	pdf := e.srv.AddFile(p, []byte("%PDF-1.7 report"), "report.pdf", "application/pdf")
	zip := e.srv.AddFile(p, []byte("PK\x03\x04 archive"), "data.zip", "application/zip")
	fake := e.srv.AddFile(p, []byte("not really a png"), "fake.png", "application/octet-stream")
	e.srv.Edit(p, func(f *task.File) {
		f.Boards[0].Tasks[0].Description = "[shot.png](" + img + ")\n\n[report.pdf](" + pdf + ")\n\n![fake](" + fake + ")"
		f.Boards[0].Tasks[0].Comments = []task.Comment{{Author: "x", Date: task.Date{Year: 2026, Month: 11, Day: 7}, Text: "[data.zip](" + zip + ")"}}
	})
	r := e.round()
	if len(r.Attachments.Downloaded) != 1 || len(r.Attachments.Skipped) != 0 {
		t.Fatalf("want one image downloaded and nothing skipped: %+v", r.Attachments)
	}
	text := e.fileText()
	for _, u := range []string{pdf, zip, fake} {
		if !strings.Contains(text, u) {
			t.Errorf("file link %s did not stay a link:\n%s", u, text)
		}
	}
	if strings.Contains(text, img) || !strings.Contains(text, "/todomd/attachments/") {
		t.Errorf("the image should be a local path:\n%s", text)
	}
	if e.srv.Downloads != 1 || e.srv.Refused != 1 {
		t.Errorf("downloads %d (want 1), refused %d (want 1: only the file named .png is asked for)", e.srv.Downloads, e.srv.Refused)
	}
	for i := 0; i < 2; i++ {
		r := e.round()
		if !r.Unchanged || len(r.Attachments.Skipped) != 0 {
			t.Fatalf("round %d not idle and quiet: pushed %v, %+v", i, r.Pushed, r.Attachments)
		}
	}
	if head := e.srv.Head(p); !strings.Contains(head, pdf) || !strings.Contains(head, zip) {
		t.Errorf("file links lost online:\n%s", head)
	}
}

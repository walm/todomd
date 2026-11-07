package cloudsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/statedir"
	"github.com/walm/todomd/internal/task"
)

// Images (plan §3.6, handoff §11). Online, an image is a link to
// <host>/a/<project>/<att>/<name>; in the local file the same image is a path
// to <root>/<project>/<att>/<name>, where <root> is
// $XDG_STATE_HOME/todomd/attachments (plus the host name for hosts other than
// todomd.online). Each form is derived from the other by rule, with no
// table, so moving TODO.md changes nothing, and a path written on another
// machine is recognised by its tail.
//
// The sync base, change lists, checksums and merges all use the URL ("wire")
// form; paths exist only in the file. Before a push, linked images this
// machine holds are uploaded if the server lacks them; after a pull, images
// this machine lacks are downloaded. Bytes are only ever read from, and
// written to, this machine's own folder — never from a path in the text.

// AttachmentReport is what a round did with images.
type AttachmentReport struct {
	Uploaded   []string `json:"uploaded"`   // att ids sent (or that would be, in a dry run)
	Downloaded []string `json:"downloaded"` // att ids fetched
	Skipped    []Skip   `json:"skipped"`
}

// Skip is an image link left as it is, and why.
type Skip struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (r *AttachmentReport) empty() bool {
	return len(r.Uploaded) == 0 && len(r.Downloaded) == 0 && len(r.Skipped) == 0
}

const nameRe = `[A-Za-z0-9_-](?:[A-Za-z0-9._-]*[A-Za-z0-9_-])?`

// AttachmentRoot is this machine's image folder for a host.
func AttachmentRoot(host string) (string, error) {
	g, err := statedir.Global()
	if err != nil {
		return "", err
	}
	root := filepath.Join(g, "attachments")
	if host != cloud.DefaultHost {
		root = filepath.Join(root, cloud.HostName(host))
	}
	return root, nil
}

type images struct {
	client  *cloud.Client
	host    string
	project string
	dir     string // <root>/<project>
	rep     *AttachmentReport
	known   map[string]bool // att ids the server is known to hold
	wire    *regexp.Regexp
	angle   *regexp.Regexp
	bare    *regexp.Regexp
}

// wireRe matches a link's attachment URLs; groups are the att id and name.
func wireRe(l *Link) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(l.Host) + `/a/` + regexp.QuoteMeta(l.ProjectID) + `/(att_[0-9a-f]{20})/(` + nameRe + `)`)
}

// FileLink is an attachment a task links to by URL.
type FileLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// TaskLinks lists the attachments of the linked project that a task's
// description and comments link to by URL — files, and images not (yet)
// held on this machine — once each, in order of appearance.
func TaskLinks(l *Link, t *task.Task) []FileLink {
	re := wireRe(l)
	var out []FileLink
	seen := map[string]bool{}
	for _, s := range append([]string{t.Description}, commentTexts(t)...) {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, FileLink{ID: m[1], Name: m[2], URL: m[0]})
			}
		}
	}
	return out
}

func commentTexts(t *task.Task) []string {
	out := make([]string, len(t.Comments))
	for i, c := range t.Comments {
		out[i] = c.Text
	}
	return out
}

func newImages(c *cloud.Client, l *Link, rep *AttachmentReport) (*images, error) {
	root, err := AttachmentRoot(l.Host)
	if err != nil {
		return nil, err
	}
	p := regexp.QuoteMeta(l.ProjectID)
	tail := `todomd/attachments/(?:[^\s/<>()\[\]]+/)?` + p + `/(att_[0-9a-f]{20})/(` + nameRe + `)`
	return &images{
		client: c, host: l.Host, project: l.ProjectID, dir: filepath.Join(root, l.ProjectID), rep: rep,
		known: map[string]bool{},
		wire:  wireRe(l),
		// A path in angle brackets may hold spaces; a bare one may not.
		angle: regexp.MustCompile(`<(/[^<>\n]*?/` + tail + `)>`),
		bare:  regexp.MustCompile(`(^|[\s(\[<"'=])(/(?:[^\s<>()\[\]"'` + "`" + `]*/)?` + tail + `)`),
	}, nil
}

// URL is the wire form of an image.
func (im *images) URL(att, name string) string {
	return im.host + "/a/" + im.project + "/" + att + "/" + name
}

// path is where this machine keeps an image.
func (im *images) path(att, name string) string { return filepath.Join(im.dir, att, name) }

// link is the local form: the path, in angle brackets when it holds
// characters a markdown link target can't.
func (im *images) link(att, name string) string {
	p := im.path(att, name)
	if strings.ContainsAny(p, " \t()<>[]") {
		return "<" + p + ">"
	}
	return p
}

// mapTexts returns a copy of f with fn applied to every description and
// comment.
func mapTexts(f *task.File, fn func(string) string) *task.File {
	c := f.Clone()
	for _, b := range c.Boards {
		for _, t := range b.Tasks {
			t.Description = fn(t.Description)
			for i := range t.Comments {
				t.Comments[i].Text = fn(t.Comments[i].Text)
			}
		}
	}
	return c
}

// refs lists this project's images a wire-form model links to: att → name.
// Links to other files (a PDF, a zip, …) are not images: they stay links,
// and are never downloaded.
func (im *images) refs(f *task.File) map[string]string {
	out := map[string]string{}
	visit := func(s string) {
		for _, m := range im.wire.FindAllStringSubmatch(s, -1) {
			if !cloud.IsImageName(m[2]) {
				continue
			}
			if _, ok := out[m[1]]; !ok {
				out[m[1]] = m[2]
			}
		}
	}
	for _, t := range f.AllTasks() {
		visit(t.Description)
		for _, c := range t.Comments {
			visit(c.Text)
		}
	}
	return out
}

// findLocal returns the name of the file this machine holds for att, or ""
// — the file named name if there is one, otherwise the folder's only file
// (the server's record of a name can differ from a hand-typed URL).
func (im *images) findLocal(att, name string) string {
	if st, err := os.Stat(im.path(att, name)); err == nil && st.Mode().IsRegular() {
		return name
	}
	entries, err := os.ReadDir(filepath.Join(im.dir, att))
	if err != nil {
		return ""
	}
	var files []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") && e.Type().IsRegular() {
			files = append(files, e.Name())
		}
	}
	if len(files) == 1 {
		return files[0]
	}
	return ""
}

// readUploadable applies the upload rule: a regular file whose real path,
// after resolving .. and symlinks, is exactly <root>/<project>/<att>/<name>,
// at most MaxImageBytes, whose bytes hash to att, of an allowed type, under
// the name the server would store it as. Nothing else is ever uploaded.
func (im *images) readUploadable(att, name string) ([]byte, string, error) {
	dirReal, err := filepath.EvalSymlinks(im.dir)
	if err != nil {
		return nil, "", errors.New("not in this machine's attachment folder")
	}
	real, err := filepath.EvalSymlinks(im.path(att, name))
	if err != nil {
		return nil, "", errors.New("not in this machine's attachment folder")
	}
	if real != filepath.Join(dirReal, att, name) {
		return nil, "", errors.New("resolves outside this machine's attachment folder")
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return nil, "", errors.New("not a regular file")
	}
	if st.Size() > cloud.MaxImageBytes {
		return nil, "", fmt.Errorf("larger than %d MB", cloud.MaxImageBytes>>20)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, "", err
	}
	if cloud.ImageID(data) != att {
		return nil, "", errors.New("its content doesn't match its id")
	}
	typ := cloud.SniffImage(data)
	if typ == "" {
		return nil, "", errors.New("not a PNG, JPEG, GIF or WebP image")
	}
	if cloud.CleanName(name, typ) != name {
		return nil, "", errors.New("its file name isn't in the form todomd.online stores")
	}
	return data, typ, nil
}

// wireMode says how toWire may decide.
type wireMode int

const (
	wireOnline  wireMode = iota // ask the server, upload what it lacks
	wireDryRun                  // ask the server, upload nothing
	wireOffline                 // no network: convert what base/result link to, or what is held here
	wireKnown                   // no network, no local evidence: convert only what base links to (--no-attachments)
)

func apiReason(err error) string {
	var ae *cloud.APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "read_only":
			return "read-only access"
		case "too_large":
			return "larger than the server accepts"
		case "quota":
			return "the project's image storage is full"
		case "unsupported_type":
			return "not a type the server accepts"
		}
		return ae.Error()
	}
	return err.Error()
}

// toWire turns this project's local image paths in f into URLs where the
// image is (or, uploading now, will be) on the server. known lists images a
// trusted model — the base, or a result — already links to, by name.
func (im *images) toWire(ctx context.Context, f *task.File, known map[string]string, mode wireMode) *task.File {
	decided := map[string]string{} // att → replacement URL, or "" to keep
	decide := func(att, name string) string {
		if v, ok := decided[att]; ok {
			return v
		}
		url := ""
		if kn, ok := known[att]; ok {
			url = im.URL(att, kn) // keep the name the trusted model uses
		}
		data, typ, verr := im.readUploadable(att, name)
		switch {
		case mode == wireKnown:
		case mode == wireOffline:
			if url == "" && verr == nil {
				url = im.URL(att, name)
			}
		case url != "":
		default:
			if im.known[att] {
				url = im.URL(att, name)
				break
			}
			has, err := im.client.HasAttachment(ctx, im.project, att)
			switch {
			case err != nil:
				im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, "couldn't ask the server: " + apiReason(err)})
			case has:
				im.known[att] = true
				url = im.URL(att, name)
			case verr != nil:
				im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, "not on the server, and " + verr.Error()})
			case mode == wireDryRun:
				im.rep.Uploaded = append(im.rep.Uploaded, att)
				url = im.URL(att, name)
			default:
				if _, err := im.client.UploadAttachment(ctx, im.project, data, typ, name); err != nil {
					im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, "upload failed: " + apiReason(err)})
				} else {
					im.known[att] = true
					im.rep.Uploaded = append(im.rep.Uploaded, att)
					url = im.URL(att, name)
				}
			}
		}
		decided[att] = url
		return url
	}
	return mapTexts(f, func(s string) string {
		s = im.angle.ReplaceAllStringFunc(s, func(m string) string {
			sm := im.angle.FindStringSubmatch(m)
			if u := decide(sm[2], sm[3]); u != "" {
				return u
			}
			return m
		})
		return im.bare.ReplaceAllStringFunc(s, func(m string) string {
			sm := im.bare.FindStringSubmatch(m)
			if u := decide(sm[3], sm[4]); u != "" {
				return sm[1] + u
			}
			return m
		})
	})
}

// toLocal turns this project's image URLs into paths, for the images this
// machine holds; the rest, and links to files that aren't images, stay URLs.
func (im *images) toLocal(f *task.File) *task.File {
	return mapTexts(f, func(s string) string {
		return im.wire.ReplaceAllStringFunc(s, func(m string) string {
			sm := im.wire.FindStringSubmatch(m)
			if !cloud.IsImageName(sm[2]) {
				return m
			}
			if name := im.findLocal(sm[1], sm[2]); name != "" {
				return im.link(sm[1], name)
			}
			return m
		})
	})
}

// download fetches the images f links to that this machine lacks: only from
// the linked host, only under this project, verified by hash and type, and
// stored under the name the server records (cleaned), never one taken from
// the link text. Links to files that aren't images stay links, quietly.
func (im *images) download(ctx context.Context, f *task.File) {
	refs := im.refs(f)
	atts := make([]string, 0, len(refs))
	for att := range refs {
		atts = append(atts, att)
	}
	slices.Sort(atts)
	for _, att := range atts {
		name := refs[att]
		if im.findLocal(att, name) != "" {
			continue
		}
		data, served, err := im.client.DownloadAttachment(ctx, im.project, att, name)
		if cloud.IsNotAnImage(err) {
			continue // a file with an image's name: it stays a link
		}
		if err != nil {
			im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, "download failed: " + apiReason(err)})
			continue
		}
		typ := cloud.SniffImage(data)
		if cloud.ImageID(data) != att || typ == "" || len(data) > cloud.MaxImageBytes {
			im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, "the server sent something other than that image"})
			continue
		}
		if served == "" {
			served = name
		}
		file := cloud.CleanName(served, typ)
		if err := writeImage(im.path(att, file), data); err != nil {
			im.rep.Skipped = append(im.rep.Skipped, Skip{att, name, err.Error()})
			continue
		}
		im.known[att] = true
		im.rep.Downloaded = append(im.rep.Downloaded, att)
	}
}

func writeImage(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".img-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// StoreImage copies an image into this machine's folder for the project and
// returns its id, stored name and local link — what `todomd attach` inserts.
// Only images the server accepts are taken.
func StoreImage(l *Link, data []byte, name string) (att, stored, link string, err error) {
	typ := cloud.SniffImage(data)
	if typ == "" {
		return "", "", "", errors.New("only PNG, JPEG, GIF and WebP images can be synced")
	}
	if len(data) > cloud.MaxImageBytes {
		return "", "", "", fmt.Errorf("images can be at most %d MB", cloud.MaxImageBytes>>20)
	}
	im, err := newImages(nil, l, &AttachmentReport{})
	if err != nil {
		return "", "", "", err
	}
	att, stored = cloud.ImageID(data), cloud.CleanName(name, typ)
	if existing := im.findLocal(att, stored); existing != "" {
		stored = existing
	} else if err := writeImage(im.path(att, stored), data); err != nil {
		return "", "", "", err
	}
	return att, stored, im.link(att, stored), nil
}

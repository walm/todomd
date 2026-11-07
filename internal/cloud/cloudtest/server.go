// Package cloudtest is an in-process stand-in for todomd.online's CLI API
// (/api/v1), for tests: device login, projects, documents, sync (both forms,
// ported from the server's worker/project/sync.ts decision logic) and live
// events. Web edits are simulated with Server.Edit.
package cloudtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
)

// Token is the bearer token the fake hands out and accepts.
const Token = "tdm_testtoken0000000000000000000000000000"

// Project is one fake project with its full revision history.
type Project struct {
	ID, Slug, Name, Repo string
	Revs                 []string // Revs[i] is revision i+1, canonical
	ReadOnly             bool     // the caller only has read access
}

func (p *Project) head() string { return p.Revs[len(p.Revs)-1] }

// Server is the fake.
type Server struct {
	*httptest.Server
	mu        sync.Mutex
	projects  map[string]*Project
	order     []string
	replies   map[string][]byte // request_id → response
	subs      map[string][]chan int
	Date      task.Date
	Requests  []SyncCall
	Handle    string
	MinCLI    string
	Down      bool                     // answer 503 to everything
	Forget    map[int]bool             // revisions treated as pruned (410 base_gone)
	BadSHA    bool                     // send a wrong content_sha256 once
	OnSync    func(req map[string]any) // observe raw sync requests
	revoked   bool
	deviceHit int
	images    map[string]map[string]image // project → att → image
	Uploads   int                         // image uploads accepted
	Downloads int                         // image downloads served
	Refused   int                         // file downloads refused with 406
}

type image struct {
	name, typ string
	data      []byte
	file      bool // a file attachment (a PDF, a zip, …), not an image
}

// SyncCall records one /sync request.
type SyncCall struct {
	Form    string // "ops" or "content"
	BaseRev *int
	Import  bool
	DryRun  bool
	Mode    string
	Raw     map[string]any
}

// New starts a fake server.
func New() *Server {
	s := &Server{projects: map[string]*Project{}, replies: map[string][]byte{}, subs: map[string][]chan int{}, images: map[string]map[string]image{},
		Date: task.Date{Year: 2026, Month: 11, Day: 6}, Handle: "walm", MinCLI: "0.0.1", Forget: map[int]bool{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string, extra ...any) {
	m := map[string]any{"error": msg, "code": code}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i].(string)] = extra[i+1]
	}
	writeJSON(w, status, m)
}

// AddProject creates a project directly (as if made on the web).
func (s *Server) AddProject(name, content string) *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(name, "", content)
}

func (s *Server) addLocked(name, repo, content string) *Project {
	f, err := markdown.Parse([]byte(content))
	if err != nil {
		panic(err)
	}
	p := &Project{ID: fmt.Sprintf("prj_%04d", len(s.order)+1), Slug: s.Handle + "/" + name, Name: name, Repo: repo,
		Revs: []string{string(markdown.Write(f))}}
	s.projects[p.ID] = p
	s.order = append(s.order, p.ID)
	return p
}

// Head returns a project's head document.
func (s *Server) Head(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projects[id].head()
}

// Rev returns a project's head revision number.
func (s *Server) Rev(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.projects[id].Revs)
}

// Edit applies a web edit to head and notifies watchers.
func (s *Server) Edit(id string, fn func(f *task.File)) {
	s.mu.Lock()
	p := s.projects[id]
	f, err := markdown.Parse([]byte(p.head()))
	if err != nil {
		panic(err)
	}
	fn(f)
	s.commitLocked(p, f)
	s.mu.Unlock()
}

// SetReadOnly makes the caller a viewer of a project.
func (s *Server) SetReadOnly(id string, ro bool) {
	s.mu.Lock()
	s.projects[id].ReadOnly = ro
	s.mu.Unlock()
}

// Revoke invalidates the token.
func (s *Server) Revoke() {
	s.mu.Lock()
	s.revoked = true
	s.mu.Unlock()
}

// Calls returns the sync requests seen so far.
func (s *Server) Calls() []SyncCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SyncCall(nil), s.Requests...)
}

func (s *Server) commitLocked(p *Project, f *task.File) bool {
	text := string(markdown.Write(f))
	if text == p.head() {
		return false
	}
	p.Revs = append(p.Revs, text)
	rev := len(p.Revs)
	for _, ch := range s.subs[p.ID] {
		select {
		case ch <- rev:
		default:
		}
	}
	return true
}

var versionRe = regexp.MustCompile(`^todomd/(\d+\.\d+\.\d+)`)

func older(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return false
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	down := s.Down
	s.mu.Unlock()
	if down {
		w.Header().Set("Retry-After", "0")
		fail(w, 503, "unavailable", "down for maintenance")
		return
	}
	path := r.URL.EscapedPath()
	if strings.HasPrefix(path, "/a/") {
		s.serveImage(w, r, strings.Split(strings.TrimPrefix(path, "/a/"), "/"))
		return
	}
	if path == "/.well-known/todomd" {
		writeJSON(w, 200, map[string]any{"api": s.URL + "/api/v1", "min_cli_version": s.MinCLI, "name": "fake", "format_version": 1})
		return
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		http.NotFound(w, r)
		return
	}
	if m := versionRe.FindStringSubmatch(r.UserAgent()); m != nil && older(m[1], s.MinCLI) {
		fail(w, 426, "upgrade_required", "this todomd is too old; run todomd upgrade", "min_version", s.MinCLI)
		return
	}
	rest := strings.TrimPrefix(path, "/api/v1")
	switch {
	case rest == "/auth/device/code" && r.Method == http.MethodPost:
		writeJSON(w, 200, map[string]any{"device_code": "dev-code", "user_code": "WDJB-MJHT",
			"verification_uri": s.URL + "/device", "verification_uri_complete": s.URL + "/device?code=WDJB-MJHT",
			"expires_in": 600, "interval": 1})
		return
	case rest == "/auth/device/token" && r.Method == http.MethodPost:
		s.mu.Lock()
		s.deviceHit++
		hit := s.deviceHit
		s.mu.Unlock()
		if hit == 1 {
			writeJSON(w, 400, map[string]any{"error": "authorization_pending"})
			return
		}
		writeJSON(w, 200, map[string]any{"token": Token, "token_type": "Bearer", "token_id": "tok_1",
			"user": map[string]any{"id": "usr_1", "handle": s.Handle, "email": "walm@example.com", "name": "Walm"}})
		return
	}
	s.mu.Lock()
	revoked := s.revoked
	s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+Token || revoked {
		fail(w, 401, "unauthenticated", "missing, revoked or expired token")
		return
	}
	switch {
	case rest == "/me":
		writeJSON(w, 200, map[string]any{"user": map[string]any{"id": "usr_1", "handle": s.Handle, "email": "walm@example.com", "name": "Walm"},
			"token": map[string]any{"id": "tok_1", "kind": "device", "scopes": nil}})
	case rest == "/auth/token" && r.Method == http.MethodDelete:
		w.WriteHeader(204)
	case strings.HasPrefix(rest, "/projects") && !strings.Contains(strings.TrimPrefix(rest, "/projects/"), "/") && rest != "/projects" && r.Method == http.MethodGet:
		s.withProject(w, strings.TrimPrefix(rest, "/projects/"), func(p *Project) { writeJSON(w, 200, s.projectJSON(p)) })
	case rest == "/projects" && r.Method == http.MethodGet:
		s.listProjects(w, r.URL.Query().Get("repo"))
	case rest == "/projects" && r.Method == http.MethodPost:
		s.createProject(w, r)
	case strings.HasSuffix(rest, "/document"):
		s.withProject(w, strings.TrimSuffix(strings.TrimPrefix(rest, "/projects/"), "/document"), func(p *Project) {
			rev := len(p.Revs)
			if r.Header.Get("If-None-Match") == strconv.Quote(strconv.Itoa(rev)) {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", strconv.Quote(strconv.Itoa(rev)))
			writeJSON(w, 200, map[string]any{"rev": rev, "content": p.head(), "updated_at": time.Now().UTC().Format(time.RFC3339), "updated_by": s.Handle, "content_sha256": sha(p.head())})
		})
	case strings.HasSuffix(rest, "/sync") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "/projects/"), "/sync")
		s.sync(w, r, id)
	case strings.Contains(rest, "/attachments/"):
		ref, att, _ := strings.Cut(strings.TrimPrefix(rest, "/projects/"), "/attachments/")
		s.attachment(w, r, ref, att)
	case strings.HasSuffix(rest, "/events"):
		s.events(w, r, strings.TrimSuffix(strings.TrimPrefix(rest, "/projects/"), "/events"))
	default:
		http.NotFound(w, r)
	}
}

// project resolves an id or (escaped) slug. Caller holds mu.
func (s *Server) project(ref string) *Project {
	ref, _ = url.PathUnescape(ref)
	if p := s.projects[ref]; p != nil {
		return p
	}
	for _, p := range s.projects {
		if p.Slug == ref {
			return p
		}
	}
	return nil
}

func (s *Server) withProject(w http.ResponseWriter, ref string, fn func(p *Project)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(ref)
	if p == nil {
		fail(w, 404, "not_found", "no such project")
		return
	}
	fn(p)
}

func (s *Server) projectJSON(p *Project) map[string]any {
	var repo any
	if p.Repo != "" {
		repo = p.Repo
	}
	role := "owner"
	if p.ReadOnly {
		role = "viewer"
	}
	return map[string]any{"id": p.ID, "slug": p.Slug, "name": p.Name, "repo": repo, "url": s.URL + "/" + p.Slug,
		"head_rev": len(p.Revs), "updated_at": time.Now().UTC().Format(time.RFC3339), "role": role, "jurisdiction": "earth"}
}

func (s *Server) listProjects(w http.ResponseWriter, repo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	for _, id := range s.order {
		p := s.projects[id]
		if repo == "" || p.Repo == repo {
			out = append(out, s.projectJSON(p))
		}
	}
	writeJSON(w, 200, map[string]any{"projects": out})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Repo, Content string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 422, "invalid_request", err.Error())
		return
	}
	if _, err := markdown.ParseStrict([]byte(body.Content)); err != nil {
		code, line := "parse_error", 0
		if pe, ok := err.(*markdown.ParseError); ok {
			code, line = pe.ErrCode(), pe.Line
		}
		fail(w, 422, code, err.Error(), "line", line)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projects {
		if p.Name == body.Name {
			fail(w, 409, "name_taken", "a project with that name exists")
			return
		}
	}
	p := s.addLocked(body.Name, body.Repo, body.Content)
	out := s.projectJSON(p)
	out["rev"] = 1
	out["content"] = p.head()
	writeJSON(w, 201, out)
}

type syncErr struct {
	status int
	code   string
	msg    string
	extra  []any
}

func parseDoc(text string) (*task.File, *syncErr) {
	if line := markdown.ConflictMarkerLine(text); line > 0 {
		return nil, &syncErr{422, "conflict_markers", fmt.Sprintf("line %d: git conflict markers", line), []any{"line", line}}
	}
	f, err := markdown.ParseStrict([]byte(text))
	if err != nil {
		code, line := "parse_error", 0
		if pe, ok := err.(*markdown.ParseError); ok {
			code, line = pe.ErrCode(), pe.Line
		}
		return nil, &syncErr{422, code, err.Error(), []any{"line", line}}
	}
	return f, nil
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request, ref string) {
	body, _ := io.ReadAll(r.Body)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		fail(w, 422, "invalid_request", "bad json")
		return
	}
	var any map[string]any
	_ = json.Unmarshal(body, &any)
	if s.OnSync != nil {
		s.OnSync(any)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(ref)
	if p == nil {
		fail(w, 404, "not_found", "no such project")
		return
	}
	var reqID string
	_ = json.Unmarshal(raw["request_id"], &reqID)
	dry := string(raw["dry_run"]) == "true"
	if reqID != "" && !dry {
		if prev, ok := s.replies[reqID]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.Write(prev)
			return
		}
	}
	_, hasOps := raw["ops"]
	_, hasContent := raw["content"]
	if hasOps && hasContent {
		fail(w, 422, "invalid_request", "send ops or content, not both")
		return
	}
	call := SyncCall{Form: "content", Import: string(raw["import"]) == "true", DryRun: dry, Raw: any}
	if hasOps {
		call.Form = "ops"
	}
	_ = json.Unmarshal(raw["mode"], &call.Mode)
	if b, ok := raw["base_rev"]; ok && string(b) != "null" {
		n, _ := strconv.Atoi(string(b))
		call.BaseRev = &n
	}
	s.Requests = append(s.Requests, call)

	resp, serr := s.decide(p, raw, call)
	if serr != nil {
		fail(w, serr.status, serr.code, serr.msg, serr.extra...)
		return
	}
	data, _ := json.Marshal(resp)
	if reqID != "" && !dry {
		s.replies[reqID] = data
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// decide follows worker/project/sync.ts. Caller holds mu.
func (s *Server) decide(p *Project, raw map[string]json.RawMessage, call SyncCall) (map[string]any, *syncErr) {
	headText := p.head()
	head, _ := markdown.Parse([]byte(headText))
	headRev := len(p.Revs)
	if call.BaseRev != nil && *call.BaseRev > headRev {
		return nil, &syncErr{409, "base_ahead", "base revision is ahead of the server", nil}
	}
	var base *task.File
	if call.BaseRev != nil {
		br := *call.BaseRev
		if br >= 1 && !s.Forget[br] {
			base, _ = markdown.Parse([]byte(p.Revs[br-1]))
		}
		var bc string
		if base == nil && json.Unmarshal(raw["base_content"], &bc) == nil && raw["base_content"] != nil {
			var e *syncErr
			if base, e = parseDoc(bc); e != nil {
				return nil, e
			}
		}
		if base == nil {
			return nil, &syncErr{410, "base_gone", "revision is no longer kept; retry with base_content", nil}
		}
		var bsha string
		if json.Unmarshal(raw["base_sha256"], &bsha) == nil && bsha != "" && bsha != sha(string(markdown.Write(base))) {
			return nil, &syncErr{409, "base_mismatch", "the base on this machine differs from the server", nil}
		}
	}
	var ours *task.File
	pushed := false
	full := call.Form == "content"
	if !full && string(raw["ops"]) != "null" {
		if base == nil {
			return nil, &syncErr{422, "invalid_request", "ops need a base_rev", nil}
		}
		list, err := ops.Decode(raw["ops"])
		if err != nil {
			return nil, &syncErr{422, "invalid_ops", err.Error(), nil}
		}
		if ours, err = ops.Apply(base, list); err != nil {
			return nil, &syncErr{422, "invalid_ops", err.Error(), nil}
		}
		if err := merge.CheckFile(ours); err != nil {
			return nil, &syncErr{422, "invalid_ops", err.Error(), nil}
		}
		pushed = true
	} else if full && string(raw["content"]) != "null" && raw["content"] != nil {
		var c string
		_ = json.Unmarshal(raw["content"], &c)
		var e *syncErr
		if ours, e = parseDoc(c); e != nil {
			return nil, e
		}
		pushed = true
	} else {
		if base == nil {
			return nil, &syncErr{422, "invalid_request", "a first link must send content", nil}
		}
		ours = base
	}
	if !pushed && call.BaseRev != nil && *call.BaseRev == headRev {
		return map[string]any{"rev": headRev, "unchanged": true, "created_rev": false, "conflicts": []any{}, "renamed_ids": []any{}}, nil
	}

	merged := head
	var conflicts []merge.Conflict
	var renamed []merge.Rename
	pristine := headText == string(markdown.Write(&task.File{Title: head.Title, Boards: emptied(head)})) && head.Preamble == ""
	mergeWith := func(b *task.File, opts merge.Options) {
		opts.Date = s.Date
		res, err := merge.Merge(b, ours, head, opts)
		if err != nil {
			panic(err)
		}
		merged, conflicts, renamed = res.Merged, res.Conflicts, res.Renamed
	}
	switch {
	case call.Mode == "replace_remote":
		if !pushed {
			return nil, &syncErr{422, "invalid_request", "replace_remote needs content", nil}
		}
		merged = ours
	case call.Import && pushed:
		b := base
		if b == nil {
			b = &task.File{}
		}
		mergeWith(b, merge.Options{NoDeletesFromOurs: true, Baseless: base == nil})
	case call.BaseRev == nil:
		if pristine {
			merged = ours
		} else {
			mergeWith(&task.File{}, merge.Options{Baseless: true})
		}
	case !pushed:
		merged = head
	case *call.BaseRev == headRev:
		merged = ours
	default:
		mergeWith(base, merge.Options{})
	}
	mergedText := string(markdown.Write(merged))
	changed := mergedText != headText
	if changed && p.ReadOnly && !call.DryRun {
		return nil, &syncErr{403, "read_only", "you can read this project but not change it; local edits stay in your file", nil}
	}
	rev := headRev
	created := false
	if changed && !call.DryRun {
		s.commitLocked(p, merged)
		rev, created = len(p.Revs), true
	}
	if conflicts == nil {
		conflicts = []merge.Conflict{}
	}
	if renamed == nil {
		renamed = []merge.Rename{}
	}
	out := map[string]any{"rev": rev, "unchanged": false, "created_rev": created, "conflicts": conflicts,
		"renamed_ids": renamed, "content_sha256": sha(mergedText)}
	if s.BadSHA {
		s.BadSHA = false
		out["content_sha256"] = sha("wrong")
	}
	if full {
		out["content"] = mergedText
	} else {
		list := ops.Delta(ours, merged)
		if list == nil {
			list = []ops.Op{}
		}
		out["ops"] = list
	}
	return out, nil
}

func emptied(f *task.File) []*task.Board {
	out := make([]*task.Board, len(f.Boards))
	for i, b := range f.Boards {
		out[i] = &task.Board{Name: b.Name}
	}
	return out
}

func (s *Server) events(w http.ResponseWriter, r *http.Request, ref string) {
	s.mu.Lock()
	p := s.project(ref)
	if p == nil {
		s.mu.Unlock()
		fail(w, 404, "not_found", "no such project")
		return
	}
	ch := make(chan int, 16)
	s.subs[p.ID] = append(s.subs[p.ID], ch)
	head := len(p.Revs)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		subs := s.subs[p.ID]
		for i, c := range subs {
			if c == ch {
				s.subs[p.ID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	fl := w.(http.Flusher)
	fmt.Fprint(w, ": connected\n\n")
	last, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	if last < head {
		fmt.Fprintf(w, "id: %d\nevent: revision\ndata: {\"rev\": %d, \"source\": \"web\", \"actor\": \"walm\", \"at\": \"now\"}\n\n", head, head)
	}
	fl.Flush()
	ping := time.NewTicker(200 * time.Millisecond)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case rev := <-ch:
			fmt.Fprintf(w, "id: %d\nevent: revision\ndata: {\"rev\": %d, \"source\": \"web\", \"actor\": \"walm\", \"at\": \"now\"}\n\n", rev, rev)
			fl.Flush()
		case <-ping.C:
			s.mu.Lock()
			revoked := s.revoked
			s.mu.Unlock()
			if revoked {
				fmt.Fprint(w, "event: access_revoked\ndata: {}\n\n")
				fl.Flush()
				return
			}
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// AddImage stores an image as a web paste would and returns its URL.
func (s *Server) AddImage(projectID string, data []byte, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	typ := cloud.SniffImage(data)
	att := cloud.ImageID(data)
	clean := cloud.CleanName(name, typ)
	if s.images[projectID] == nil {
		s.images[projectID] = map[string]image{}
	}
	s.images[projectID][att] = image{name: clean, typ: typ, data: data}
	return s.URL + "/a/" + projectID + "/" + att + "/" + clean
}

// AddFile stores a file that isn't an image (a PDF, a zip, …), as the web
// does since files can be attached, and returns its URL. The name is kept
// as given; the caller passes one the server would store.
func (s *Server) AddFile(projectID string, data []byte, name, typ string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	att := cloud.ImageID(data)
	if s.images[projectID] == nil {
		s.images[projectID] = map[string]image{}
	}
	s.images[projectID][att] = image{name: name, typ: typ, data: data, file: true}
	return s.URL + "/a/" + projectID + "/" + att + "/" + name
}

// HasImage reports whether the project holds an image.
func (s *Server) HasImage(projectID, att string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.images[projectID][att]
	return ok
}

// DropImage forgets an image (as the server's cleanup would).
func (s *Server) DropImage(projectID, att string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.images[projectID], att)
}

func (s *Server) attachment(w http.ResponseWriter, r *http.Request, ref, att string) {
	s.mu.Lock()
	p := s.project(ref)
	s.mu.Unlock()
	if p == nil {
		fail(w, 404, "not_found", "no such project")
		return
	}
	switch r.Method {
	case http.MethodHead:
		if s.HasImage(p.ID, att) {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(404)
		}
	case http.MethodPost:
		if p.ReadOnly {
			fail(w, 403, "read_only", "read-only access")
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
			fail(w, 415, "unsupported_type", "upload an image")
			return
		}
		data, _ := io.ReadAll(r.Body)
		if len(data) > cloud.MaxImageBytes {
			fail(w, 413, "too_large", "images can be at most 5 MB")
			return
		}
		typ := cloud.SniffImage(data)
		if typ == "" {
			fail(w, 415, "unsupported_type", "only PNG, JPEG, GIF and WebP images can be uploaded")
			return
		}
		if id := cloud.ImageID(data); id != att {
			fail(w, 422, "hash_mismatch", "the image hashes to "+id, "id", id)
			return
		}
		name, _ := url.PathUnescape(r.Header.Get("X-Filename"))
		u := s.AddImage(p.ID, data, name)
		s.mu.Lock()
		s.Uploads++
		img := s.images[p.ID][att]
		s.mu.Unlock()
		writeJSON(w, 201, map[string]any{"id": att, "name": img.name, "type": img.typ, "size": len(data), "url": u})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, parts []string) {
	s.mu.Lock()
	revoked := s.revoked
	s.mu.Unlock()
	if len(parts) != 3 || r.Header.Get("Authorization") != "Bearer "+Token || revoked {
		fail(w, 404, "not_found", "not found")
		return
	}
	s.mu.Lock()
	img, ok := s.images[parts[0]][parts[1]]
	if ok && img.file && !accepts(r.Header.Get("Accept"), img.typ) {
		s.Refused++
		s.mu.Unlock()
		fail(w, 406, "not_an_image", "this attachment is a file, not an image")
		return
	}
	if ok {
		s.Downloads++
	}
	s.mu.Unlock()
	if !ok {
		fail(w, 404, "not_found", "not found")
		return
	}
	w.Header().Set("Content-Type", img.typ)
	disp := "inline"
	if img.file {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", disp+`; filename="`+img.name+`"`)
	w.Write(img.data)
}

// accepts mirrors the server: whether an Accept header admits a type. No
// header admits everything.
func accepts(header, typ string) bool {
	if header == "" {
		return true
	}
	major, _, _ := strings.Cut(typ, "/")
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		rng := strings.ToLower(strings.TrimSpace(fields[0]))
		zero := false
		for _, p := range fields[1:] {
			p = strings.ToLower(strings.TrimSpace(p))
			if v, ok := strings.CutPrefix(p, "q="); ok && strings.Trim(v, "0.") == "" {
				zero = true
			}
		}
		if !zero && (rng == "*/*" || rng == major+"/*" || rng == typ) {
			return true
		}
	}
	return false
}

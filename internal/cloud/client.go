package cloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/ops"
)

// RequestTimeout bounds every request except the event stream.
const RequestTimeout = 10 * time.Second

// maxRetries is how often a 429/503 is retried, honoring Retry-After.
const maxRetries = 3

// ErrNotLoggedIn: no token for the host.
var ErrNotLoggedIn = errors.New("not logged in")

// APIError is an error response: {"error": "<message>", "code": "<code>", …}.
type APIError struct {
	Status  int
	Code    string
	Message string
	Extra   map[string]any
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// Line is the "line" a 422 parse/conflict-marker error points at, or 0.
func (e *APIError) Line() int {
	if v, ok := e.Extra["line"].(float64); ok {
		return int(v)
	}
	return 0
}

// UnavailableError: the service couldn't be reached or answered with a
// server error. Retrying later is safe.
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return "todomd.online unreachable: " + e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

// IsStatus reports whether err is an APIError with the given status (and,
// when code is non-empty, that code).
func IsStatus(err error, status int, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status && (code == "" || ae.Code == code)
}

// Client is one host, optionally with a token.
type Client struct {
	Host      string // scheme://host
	API       string // API base, from discovery
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// UserAgent is "todomd/<semver> <hostname>", which the server checks against
// its min_cli_version and records with each revision.
func UserAgent(version string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return "todomd/" + strings.TrimPrefix(version, "v") + " " + host
}

// New returns a client for host. api may be empty until Discover fills it.
func New(host, api, token, version string) *Client {
	if api == "" {
		api = host + "/api/v1"
	}
	return &Client{Host: host, API: strings.TrimRight(api, "/"), Token: token, UserAgent: UserAgent(version), HTTP: &http.Client{
		// The bearer token must never follow a redirect off the host.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme+"://"+strings.ToLower(req.URL.Host) != host {
				return fmt.Errorf("refusing to follow a redirect to %s", req.URL.Host)
			}
			return nil
		},
	}}
}

// Discovery is GET /.well-known/todomd.
type Discovery struct {
	API           string `json:"api"`
	MinCLIVersion string `json:"min_cli_version"`
	Name          string `json:"name"`
	FormatVersion int    `json:"format_version"`
}

// Discover reads the host's discovery document and points the client at its
// API base.
func (c *Client) Discover(ctx context.Context) (*Discovery, error) {
	var d Discovery
	if err := c.doURL(ctx, http.MethodGet, c.Host+"/.well-known/todomd", nil, &d, nil); err != nil {
		return nil, err
	}
	if d.API != "" {
		// The token goes wherever the API is, so it must be the host itself:
		// never another origin, and never plain http under an https host.
		u, err := url.Parse(d.API)
		if err != nil || u.Scheme+"://"+strings.ToLower(u.Host) != c.Host {
			return nil, fmt.Errorf("%s announces its API at %q, which isn't on the same address; refusing to send credentials there", HostName(c.Host), d.API)
		}
		c.API = strings.TrimRight(d.API, "/")
	}
	return &d, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doURL(ctx, method, c.API+path, body, out, nil)
}

func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s >= 0 {
		return min(time.Duration(s)*time.Second, 30*time.Second)
	}
	return 2 * time.Second
}

// doURL sends one request (JSON in, JSON out) with the per-request timeout,
// retrying 429 and 503 up to maxRetries times as Retry-After asks.
func (c *Client) doURL(ctx context.Context, method, u string, body, out any, hdr http.Header) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	status, _, data, err := c.send(ctx, method, u, payload, hdr)
	if err != nil {
		return err
	}
	if status == http.StatusNotModified {
		return errNotModified
	}
	if status >= 400 {
		return apiError(status, data)
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return &UnavailableError{Err: fmt.Errorf("bad response from %s: %w", HostName(c.Host), err)}
		}
	}
	return nil
}

// send is one request with raw bytes, retrying 429 and 503 up to maxRetries
// times as Retry-After asks.
func (c *Client) send(ctx context.Context, method, u string, payload []byte, hdr http.Header) (int, http.Header, []byte, error) {
	for attempt := 0; ; attempt++ {
		status, header, data, err := c.once(ctx, method, u, payload, hdr)
		if err != nil {
			return 0, nil, nil, err
		}
		if (status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable) && attempt < maxRetries {
			select {
			case <-time.After(retryAfter(header.Get("Retry-After"))):
				continue
			case <-ctx.Done():
				return 0, nil, nil, ctx.Err()
			}
		}
		return status, header, data, nil
	}
}

var errNotModified = errors.New("not modified")

func (c *Client) once(ctx context.Context, method, u string, payload []byte, hdr http.Header) (int, http.Header, []byte, error) {
	rctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(rctx, method, u, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if payload != nil && hdr.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for k, vs := range hdr {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, &UnavailableError{Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return 0, nil, nil, &UnavailableError{Err: err}
	}
	return resp.StatusCode, resp.Header, data, nil
}

func apiError(status int, data []byte) error {
	e := &APIError{Status: status}
	var m map[string]any
	if json.Unmarshal(data, &m) == nil {
		e.Extra = m
		if s, ok := m["error"].(string); ok {
			e.Message = s
		}
		if s, ok := m["code"].(string); ok {
			e.Code = s
		}
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return &UnavailableError{Err: e}
	}
	return e
}

// --- device login ------------------------------------------------------------------

// DeviceCode is the first step of the device flow.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// StartDevice asks for a device code.
func (c *Client) StartDevice(ctx context.Context, deviceName, version string) (*DeviceCode, error) {
	var dc DeviceCode
	err := c.do(ctx, http.MethodPost, "/auth/device/code", map[string]string{
		"device_name":    deviceName,
		"platform":       runtime.GOOS + "/" + runtime.GOARCH,
		"client_version": strings.TrimPrefix(version, "v"),
	}, &dc)
	if err != nil {
		return nil, err
	}
	return &dc, nil
}

// Token is a minted credential.
type Token struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	TokenID   string `json:"token_id"`
	User      User   `json:"user"`
}

// DeviceError is a pending or failed device poll; Code is the RFC 8628
// error: authorization_pending, slow_down, access_denied or expired_token.
type DeviceError struct{ Code string }

func (e *DeviceError) Error() string { return e.Code }

// PollDevice asks once whether the device was approved.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (*Token, error) {
	var t Token
	err := c.do(ctx, http.MethodPost, "/auth/device/token", map[string]string{"device_code": deviceCode}, &t)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusBadRequest {
		if s, ok := ae.Extra["error"].(string); ok {
			return nil, &DeviceError{Code: s}
		}
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// --- account ---------------------------------------------------------------------

// Scope is one project a scoped token may reach.
type Scope struct {
	Project    string `json:"project"`
	Slug       string `json:"slug"`
	Permission string `json:"permission"`
}

// Me is GET /me.
type Me struct {
	User  User `json:"user"`
	Token struct {
		ID     string  `json:"id"`
		Kind   string  `json:"kind"`
		Scopes []Scope `json:"scopes"`
	} `json:"token"`
}

// Me returns who the token belongs to.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var m Me
	if err := c.do(ctx, http.MethodGet, "/me", nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// RevokeToken revokes the calling token (logout).
func (c *Client) RevokeToken(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/auth/token", nil, nil)
}

// --- projects --------------------------------------------------------------------

// Project as the API lists it.
type Project struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Repo         string `json:"repo"`
	URL          string `json:"url"`
	HeadRev      int    `json:"head_rev"`
	UpdatedAt    string `json:"updated_at"`
	Role         string `json:"role"`
	Jurisdiction string `json:"jurisdiction"`
}

// pathID escapes a project id or slug for a path ("owner/name" keeps its
// slash escaped, which the server accepts).
func pathID(id string) string { return url.PathEscape(id) }

// Projects lists the caller's projects, optionally only those for a repo.
func (c *Client) Projects(ctx context.Context, repo string) ([]Project, error) {
	path := "/projects"
	if repo != "" {
		path += "?repo=" + url.QueryEscape(repo)
	}
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// Project fetches one project by id or slug.
func (c *Client) Project(ctx context.Context, id string) (*Project, error) {
	var p Project
	if err := c.do(ctx, http.MethodGet, "/projects/"+pathID(id), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// NewProject is POST /projects.
type NewProject struct {
	Name         string `json:"name"`
	Repo         string `json:"repo,omitempty"`
	Content      string `json:"content"`
	Jurisdiction string `json:"jurisdiction,omitempty"`
}

// CreatedProject is a project plus its first revision.
type CreatedProject struct {
	Project
	Rev     int    `json:"rev"`
	Content string `json:"content"`
}

// CreateProject creates a project from a document. A retry of a create the
// server already did answers 200 with the same body, which this treats as
// success like the 201.
func (c *Client) CreateProject(ctx context.Context, p NewProject) (*CreatedProject, error) {
	var out CreatedProject
	if err := c.do(ctx, http.MethodPost, "/projects", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Document is a project's head.
type Document struct {
	Rev           int    `json:"rev"`
	Content       string `json:"content"`
	UpdatedAt     string `json:"updated_at"`
	UpdatedBy     string `json:"updated_by"`
	ContentSHA256 string `json:"content_sha256"`
}

// Document fetches head. With ifNoneMatch > 0, a head still at that rev
// returns (nil, nil).
func (c *Client) Document(ctx context.Context, id string, ifNoneMatch int) (*Document, error) {
	var hdr http.Header
	if ifNoneMatch > 0 {
		hdr = http.Header{"If-None-Match": {strconv.Quote(strconv.Itoa(ifNoneMatch))}}
	}
	var d Document
	err := c.doURL(ctx, http.MethodGet, c.API+"/projects/"+pathID(id)+"/document", nil, &d, hdr)
	if errors.Is(err, errNotModified) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// --- sync ------------------------------------------------------------------------

// SyncRequest is POST /projects/{id}/sync. Exactly one of the two forms is
// sent: the ops form (Full false: "ops", nil meaning no local changes) or the
// full form (Full true: "content", nil meaning unchanged since base).
type SyncRequest struct {
	RequestID   string
	BaseRev     *int
	BaseSHA256  string
	Full        bool
	Ops         []ops.Op
	Content     *string
	BaseContent *string
	Mode        string // "" or "merge", "replace_remote"
	DryRun      bool
	Import      bool
}

// MarshalJSON writes the wire form; the server tells the forms apart by
// which key is present.
func (r SyncRequest) MarshalJSON() ([]byte, error) {
	m := map[string]any{"base_rev": r.BaseRev}
	if r.RequestID != "" {
		m["request_id"] = r.RequestID
	}
	if r.BaseSHA256 != "" {
		m["base_sha256"] = r.BaseSHA256
	}
	if r.Full {
		m["content"] = r.Content
		if r.BaseContent != nil {
			m["base_content"] = *r.BaseContent
		}
	} else if r.Ops == nil {
		m["ops"] = nil
	} else {
		m["ops"] = r.Ops
	}
	if r.Mode != "" {
		m["mode"] = r.Mode
	}
	if r.DryRun {
		m["dry_run"] = true
	}
	if r.Import {
		m["import"] = true
	}
	return json.Marshal(m)
}

// SyncResponse is the answer to a sync. Ops (ops form) apply to what was
// sent; Content (full form) is the merged document. Unchanged responses carry
// neither.
type SyncResponse struct {
	Rev           int              `json:"rev"`
	Unchanged     bool             `json:"unchanged"`
	CreatedRev    bool             `json:"created_rev"`
	Conflicts     []merge.Conflict `json:"conflicts"`
	RenamedIDs    []merge.Rename   `json:"renamed_ids"`
	ContentSHA256 string           `json:"content_sha256"`
	RawOps        json.RawMessage  `json:"ops"`
	Content       *string          `json:"content"`
}

// Ops decodes the response ops strictly.
func (r *SyncResponse) Ops() ([]ops.Op, error) {
	if len(r.RawOps) == 0 || string(bytes.TrimSpace(r.RawOps)) == "null" {
		return nil, nil
	}
	return ops.Decode(r.RawOps)
}

// NewRequestID is a random id for one sync round; a retry reuses it.
func NewRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Sync sends one sync request. A timeout or connection error is retried
// once with the same request_id, which the server answers from its record of
// the first attempt if that one got through.
func (c *Client) Sync(ctx context.Context, projectID string, req SyncRequest) (*SyncResponse, error) {
	var out SyncResponse
	path := "/projects/" + pathID(projectID) + "/sync"
	err := c.do(ctx, http.MethodPost, path, req, &out)
	var ue *UnavailableError
	if errors.As(err, &ue) && !isAPIError(ue.Err) && req.RequestID != "" {
		out = SyncResponse{}
		err = c.do(ctx, http.MethodPost, path, req, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func isAPIError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae)
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

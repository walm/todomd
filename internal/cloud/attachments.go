package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// MaxImageBytes is the largest image todomd.online stores.
const MaxImageBytes = 5 * 1024 * 1024

// SniffImage returns the image type from the file signature (never from a
// name or a header), or "" for anything but PNG, JPEG, GIF and WebP — the
// types todomd.online accepts. No SVG: it can carry script.
func SniffImage(b []byte) string {
	at := func(i int, v ...byte) bool {
		return len(b) >= i+len(v) && bytes.Equal(b[i:i+len(v)], v)
	}
	switch {
	case at(0, 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a):
		return "image/png"
	case at(0, 0xff, 0xd8, 0xff):
		return "image/jpeg"
	case at(0, 0x47, 0x49, 0x46, 0x38) && len(b) > 5 && (b[4] == 0x37 || b[4] == 0x39) && b[5] == 0x61:
		return "image/gif"
	case at(0, 0x52, 0x49, 0x46, 0x46) && at(8, 0x57, 0x45, 0x42, 0x50):
		return "image/webp"
	}
	return ""
}

var imageExt = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}

var (
	extRe     = regexp.MustCompile(`\.[^.]*$`)
	unsafeRe  = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	edgeDotRe = regexp.MustCompile(`^[-.]+|[-.]+$`)
)

// CleanName is the file name todomd.online stores an image under: safe for a
// URL path segment and a file system, with the extension its type implies.
// It mirrors the server's cleanName, so a name cleaned here comes back
// unchanged from an upload.
func CleanName(name, typ string) string {
	base := extRe.ReplaceAllString(name, "")
	base = norm.NFKD.String(base)
	base = unsafeRe.ReplaceAllString(base, "-")
	base = edgeDotRe.ReplaceAllString(base, "")
	if len(base) > 60 {
		base = base[:60] // ASCII only by now
	}
	if base == "" {
		base = "image"
	}
	return base + "." + imageExt[typ]
}

// ImageID is an image's content-derived id: att_ plus the first 20 hex
// characters of its SHA-256.
func ImageID(b []byte) string {
	h := sha256.Sum256(b)
	return "att_" + hex.EncodeToString(h[:])[:20]
}

func (c *Client) attachmentPath(projectID, att string) string {
	return c.API + "/projects/" + pathID(projectID) + "/attachments/" + url.PathEscape(att)
}

// HasAttachment asks whether the project already holds an image.
func (c *Client) HasAttachment(ctx context.Context, projectID, att string) (bool, error) {
	status, _, data, err := c.send(ctx, http.MethodHead, c.attachmentPath(projectID, att), nil, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, apiError(status, data)
}

// Attachment is an uploaded image as the server records it.
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// UploadAttachment stores an image under its content-derived id. The server
// hashes the bytes itself; a mismatch is 422 hash_mismatch.
func (c *Client) UploadAttachment(ctx context.Context, projectID string, data []byte, typ, name string) (*Attachment, error) {
	hdr := http.Header{"Content-Type": {typ}, "X-Filename": {url.PathEscape(name)}}
	status, _, body, err := c.send(ctx, http.MethodPost, c.attachmentPath(projectID, ImageID(data)), data, hdr)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, apiError(status, body)
	}
	var a Attachment
	if err := jsonUnmarshal(body, &a); err != nil {
		return nil, &UnavailableError{Err: fmt.Errorf("bad upload response: %w", err)}
	}
	return &a, nil
}

// DownloadAttachment fetches /a/{project}/{att}/{name} from the client's
// own host — the only place the token is sent — and returns the bytes and
// the name the server records for the image.
func (c *Client) DownloadAttachment(ctx context.Context, projectID, att, name string) ([]byte, string, error) {
	u := c.Host + "/a/" + url.PathEscape(projectID) + "/" + url.PathEscape(att) + "/" + url.PathEscape(name)
	status, header, body, err := c.send(ctx, http.MethodGet, u, nil, http.Header{"Accept": {"image/*"}})
	if err != nil {
		return nil, "", err
	}
	if status >= 400 {
		return nil, "", apiError(status, body)
	}
	served := ""
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		served = params["filename"]
	}
	return body, strings.TrimSpace(served), nil
}

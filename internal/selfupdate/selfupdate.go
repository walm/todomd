// Package selfupdate finds newer todomd releases and replaces the running
// binary with one. It consumes exactly what the release workflow publishes and
// what install.sh consumes — the same asset names and the same sha256
// verification against the release's checksums.txt — so both install paths
// offer the same guarantees.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Repo is the GitHub project releases are pulled from.
const Repo = "walm/todomd"

// Default endpoints; tests point these at an httptest server.
const (
	DefaultAPIURL      = "https://api.github.com"
	DefaultDownloadURL = "https://github.com"
)

// maxAsset caps how much we'll read from a release asset (~64 MiB), so a
// malformed or hostile response can't exhaust memory.
const maxAsset = 64 << 20

// Client fetches release metadata and assets.
type Client struct {
	HTTP        *http.Client
	APIURL      string
	DownloadURL string
}

// NewClient returns a Client with sensible timeouts pointed at GitHub.
func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		APIURL:      DefaultAPIURL,
		DownloadURL: DefaultDownloadURL,
	}
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "todomd-selfupdate")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAsset))
}

// Latest returns the newest published release tag, e.g. "v0.2.0".
func (c *Client) Latest(ctx context.Context) (string, error) {
	data, err := c.get(ctx, c.APIURL+"/repos/"+Repo+"/releases/latest")
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", fmt.Errorf("decoding release: %w", err)
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("release has no tag_name")
	}
	return rel.TagName, nil
}

// AssetName is the archive published for a version and platform, matching the
// name_template in .goreleaser.yaml.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("todomd_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// Download fetches the release archive for this platform, verifies its sha256
// against the release's checksums.txt, and returns the todomd binary from it.
func (c *Client) Download(ctx context.Context, version string) ([]byte, error) {
	asset := AssetName(version, runtime.GOOS, runtime.GOARCH)
	base := c.DownloadURL + "/" + Repo + "/releases/download/" + version

	archive, err := c.get(ctx, base+"/"+asset)
	if err != nil {
		return nil, err
	}
	sums, err := c.get(ctx, base+"/checksums.txt")
	if err != nil {
		return nil, err
	}
	want, err := checksumFor(string(sums), asset)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for %s: refusing to install", asset)
	}
	return extractBinary(archive)
}

// checksumFor finds asset's expected digest in a "checksums.txt" body.
func checksumFor(sums, asset string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		sum, name, ok := strings.Cut(strings.TrimSpace(line), "  ")
		if ok && name == asset {
			return sum, nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", asset)
}

// extractBinary pulls the todomd executable out of a release tarball.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == "todomd" && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxAsset))
		}
	}
	return nil, fmt.Errorf("archive contains no todomd binary")
}

// Replace swaps the binary at target for bin. The new file is written beside
// the target (same filesystem) and renamed over it, which is atomic and — on
// unix — legal even while that binary is the running process: it keeps its
// inode. Windows would need a different dance; we only ship darwin and linux.
func Replace(bin []byte, target string) error {
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	tmp, err := os.CreateTemp(dir, ".todomd-upgrade-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%s is not writable: re-run with sudo, or reinstall with the install script", dir)
		}
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), resolved); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%s is not writable: re-run with sudo, or reinstall with the install script", resolved)
		}
		return err
	}
	return nil
}

// IsRelease reports whether v looks like a published release (v1.2.3), as
// opposed to "dev" or a go-install pseudo-version like
// v0.2.1-0.20260725151828-ad0f4898b977 — those carry a prerelease part, so
// they are already newer than the release they were built from and must not be
// "upgraded" backwards into it.
func IsRelease(v string) bool {
	return semver.IsValid(v) && semver.Prerelease(v) == ""
}

// Newer reports whether remote is a strictly newer version than current.
func Newer(current, remote string) bool {
	if !semver.IsValid(current) || !semver.IsValid(remote) {
		return false
	}
	return semver.Compare(remote, current) > 0
}

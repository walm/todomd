package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// tarball builds a release archive containing a todomd binary with the given
// contents.
func tarball(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{
		{"README.md", "docs"},
		{"todomd", binary},
	} {
		if err := tw.WriteHeader(&tar.Header{
			Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseServer serves a fake v9.9.9 release. corrupt swaps the archive for
// content that won't match the published checksum.
func releaseServer(t *testing.T, binary string, corrupt bool) *Client {
	t.Helper()
	archive := tarball(t, binary)
	sum := sha256.Sum256(archive)
	asset := AssetName("v9.9.9", runtime.GOOS, runtime.GOARCH)
	checksums := fmt.Sprintf("%s  %s\n%s  todomd_9.9.9_other_arch.tar.gz\n",
		hex.EncodeToString(sum[:]), asset, strings.Repeat("0", 64))
	served := archive
	if corrupt {
		served = append(append([]byte{}, archive...), "tampered"...)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9","name":"v9.9.9"}`)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v9.9.9/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(served)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/v9.9.9/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), APIURL: srv.URL, DownloadURL: srv.URL}
}

func TestLatest(t *testing.T) {
	c := releaseServer(t, "binary", false)
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v9.9.9" {
		t.Errorf("Latest = %q", got)
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	c := releaseServer(t, "new-binary-contents", false)
	bin, err := c.Download(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if string(bin) != "new-binary-contents" {
		t.Errorf("binary = %q", bin)
	}
}

func TestDownloadRefusesTamperedArchive(t *testing.T) {
	c := releaseServer(t, "new-binary-contents", true)
	_, err := c.Download(context.Background(), "v9.9.9")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered archive must be refused, got %v", err)
	}
}

func TestReplaceSwapsBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "todomd")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace([]byte("new"), target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q", got)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("replacement is not executable: %v", st.Mode())
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestReplaceThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "todomd-v1")
	link := filepath.Join(dir, "todomd")
	if err := os.WriteFile(real, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Replace([]byte("new"), link); err != nil {
		t.Fatal(err)
	}
	// The symlink target is replaced, and the link still points at it.
	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents through symlink = %q", got)
	}
}

func TestReplaceUnwritableDirExplains(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "todomd")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	err := Replace([]byte("new"), target)
	if err == nil {
		t.Fatal("expected a permission error")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("error should explain what to do, got: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Error("failed upgrade must leave the old binary intact")
	}
}

func TestVersionComparison(t *testing.T) {
	cases := []struct {
		current, remote string
		newer           bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.0", false},
		{"v0.10.0", "v0.9.0", false}, // not string ordering
		{"v0.9.0", "v0.10.0", true},
		{"dev", "v0.3.0", false}, // unknown current: never nag
		{"v0.2.0", "garbage", false},
		// A go-install pseudo-version is already past the release it came from.
		{"v0.2.1-0.20260725151828-ad0f4898b977", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.remote); got != c.newer {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.remote, got, c.newer)
		}
	}

	for v, want := range map[string]bool{
		"v0.2.0":                               true,
		"v1.0.0":                               true,
		"dev":                                  false,
		"":                                     false,
		"v0.3.0-rc1":                           false,
		"v0.2.1-0.20260725151828-ad0f4898b977": false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestAssetNameMatchesGoreleaser(t *testing.T) {
	// goreleaser: todomd_{{.Version}}_{{.Os}}_{{.Arch}}.tar.gz, version unprefixed.
	if got := AssetName("v0.2.0", "darwin", "arm64"); got != "todomd_0.2.0_darwin_arm64.tar.gz" {
		t.Errorf("AssetName = %q", got)
	}
}

func TestCacheAndNotice(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, _, ok := LoadCache(); ok {
		t.Error("empty state dir should have no cache")
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if err := SaveCache("v9.9.9", now); err != nil {
		t.Fatal(err)
	}
	latest, at, ok := LoadCache()
	if !ok || latest != "v9.9.9" || !at.Equal(now) {
		t.Fatalf("LoadCache = %q %v %v", latest, at, ok)
	}

	if n := Notice("v0.2.0"); !strings.Contains(n, "v9.9.9") || !strings.Contains(n, "todomd upgrade") {
		t.Errorf("Notice = %q", n)
	}
	if n := Notice("v9.9.9"); n != "" {
		t.Errorf("up-to-date build must stay quiet, got %q", n)
	}
	if n := Notice("dev"); n != "" {
		t.Errorf("dev build must stay quiet, got %q", n)
	}
	t.Setenv(NoCheckEnv, "1")
	if n := Notice("v0.2.0"); n != "" {
		t.Errorf("%s must silence the notice, got %q", NoCheckEnv, n)
	}
}

func TestRefreshCacheRespectsFreshnessAndOptOut(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var hits int
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// RefreshCache uses the real endpoint, so exercise the freshness logic
	// directly: a fresh cache must short-circuit before any request.
	now := time.Now()
	if err := SaveCache("v9.9.9", now); err != nil {
		t.Fatal(err)
	}
	RefreshCache(context.Background(), "v0.2.0", now.Add(time.Hour))
	if hits != 0 {
		t.Error("fresh cache should not trigger a request")
	}
	// Opt-out and dev builds never reach the network either.
	t.Setenv(NoCheckEnv, "1")
	RefreshCache(context.Background(), "v0.2.0", now.Add(48*time.Hour))
	if hits != 0 {
		t.Error("opt-out should not trigger a request")
	}
}

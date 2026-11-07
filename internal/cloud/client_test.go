package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	for in, want := range map[string]string{
		"todomd.online":          "https://todomd.online",
		"https://TodoMD.online/": "https://todomd.online",
		"http://localhost:5173":  "http://localhost:5173",
		"http://127.0.0.1:8080":  "http://127.0.0.1:8080",
	} {
		if got, err := NormalizeHost(in); err != nil || got != want {
			t.Errorf("NormalizeHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://todomd.online", "ftp://x", "https://x/path", ""} {
		if _, err := NormalizeHost(bad); err == nil {
			t.Errorf("NormalizeHost(%q) accepted", bad)
		}
	}
}

func TestDiscoverRefusesForeignAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"api": "https://evil.example/api/v1"})
	}))
	defer srv.Close()
	c := New(srv.URL, "", "tdm_x", "0.10.0")
	if _, err := c.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(c.API, "evil") {
		t.Error("client adopted the foreign API base")
	}
}

func TestSyncRequestForms(t *testing.T) {
	rev := 3
	b, _ := json.Marshal(SyncRequest{BaseRev: &rev})
	if !strings.Contains(string(b), `"ops":null`) || strings.Contains(string(b), "content") {
		t.Errorf("ops-form pull: %s", b)
	}
	b, _ = json.Marshal(SyncRequest{Full: true})
	if !strings.Contains(string(b), `"content":null`) || !strings.Contains(string(b), `"base_rev":null`) || strings.Contains(string(b), `"ops"`) {
		t.Errorf("full-form first link: %s", b)
	}
}

func TestUserAgent(t *testing.T) {
	if ua := UserAgent("v0.10.0"); !strings.HasPrefix(ua, "todomd/0.10.0 ") {
		t.Errorf("UserAgent = %q", ua)
	}
}

func TestIsImageName(t *testing.T) {
	for name, want := range map[string]bool{
		"shot.png": true, "a.JPG": true, "b.jpeg": true, "c.gif": true, "d.WebP": true,
		"report.pdf": false, "data.zip": false, "png": false, "file": false, "x.svg": false, "x.png.txt": false,
	} {
		if got := IsImageName(name); got != want {
			t.Errorf("IsImageName(%q) = %v", name, got)
		}
	}
}

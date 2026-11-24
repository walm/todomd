package cloud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHostIsTheApp(t *testing.T) {
	t.Setenv("TODOMD_HOST", "")
	got, err := ResolveHost("")
	if err != nil || got != "https://app.todomd.online" {
		t.Fatalf("ResolveHost = %q, %v", got, err)
	}
}

func TestRootHostsAreTheAppHosts(t *testing.T) {
	t.Setenv("TODOMD_HOST", "")
	for in, want := range map[string]string{
		"todomd.online":                     "https://app.todomd.online",
		"https://todomd.online/":            "https://app.todomd.online",
		"https://app.todomd.online":         "https://app.todomd.online",
		"https://staging.todomd.online":     "https://app-staging.todomd.online",
		"https://app-staging.todomd.online": "https://app-staging.todomd.online",
		"http://localhost:5173":             "http://localhost:5173",
		"https://self.example":              "https://self.example",
	} {
		got, err := ResolveHost(in)
		if err != nil || got != want {
			t.Errorf("ResolveHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	t.Setenv("TODOMD_HOST", "https://staging.todomd.online")
	if got, _ := ResolveHost(""); got != "https://app-staging.todomd.online" {
		t.Errorf("TODOMD_HOST=staging root resolved to %q", got)
	}
}

func TestRootHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://app.todomd.online":         "https://todomd.online",
		"https://todomd.online":             "https://todomd.online",
		"https://app-staging.todomd.online": "https://staging.todomd.online",
		"http://localhost:5173":             "http://localhost:5173",
	} {
		if got := RootHost(in); got != want {
			t.Errorf("RootHost(%q) = %q; want %q", in, got, want)
		}
	}
}

func writeHosts(t *testing.T, body string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if err := os.MkdirAll(filepath.Join(cfg, "todomd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "todomd", "hosts.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOlderLoginUnderTheRootHostIsTheAppLogin(t *testing.T) {
	writeHosts(t, `{"hosts":{"https://todomd.online":{"api":"https://todomd.online/api/v1","token":"old","user":{"id":"u1","handle":"walm"}},
		"http://localhost:1":{"api":"http://localhost:1/api/v1","token":"dev"}}}`)
	h, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	c := h.Hosts["https://app.todomd.online"]
	if c == nil || c.Token != "old" || c.User.Handle != "walm" || c.API != "https://app.todomd.online/api/v1" {
		t.Fatalf("app entry: %+v", c)
	}
	if h.Hosts["https://todomd.online"] != nil {
		t.Error("the root entry is still there: that would be a second login")
	}
	if h.Hosts["http://localhost:1"] == nil {
		t.Error("another host's login was dropped")
	}
	// Saved quietly in the new form.
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadHosts()
	if len(again.Hosts) != 2 || again.Hosts["https://app.todomd.online"].Token != "old" {
		t.Fatalf("after save: %+v", again.Hosts)
	}
}

func TestAppLoginWinsOverRootLogin(t *testing.T) {
	writeHosts(t, `{"hosts":{"https://todomd.online":{"token":"root"},"https://app.todomd.online":{"token":"app"}}}`)
	h, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Hosts) != 1 || h.Hosts["https://app.todomd.online"].Token != "app" {
		t.Fatalf("%+v", h.Hosts)
	}
}

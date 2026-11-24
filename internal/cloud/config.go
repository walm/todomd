// Package cloud talks to app.todomd.online (or another host serving the same
// API): discovery, device login, credentials, projects, documents, sync and
// live events. It knows nothing about local files; internal/cloudsync builds
// the sync rounds on top of it.
package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultHost is used when neither --host nor TODOMD_HOST says otherwise.
const DefaultHost = "https://app.todomd.online"

// LegacyHost is where the app lived before it moved to DefaultHost. It still
// answers the API for old clients, and attachment links stay on it for good.
const LegacyHost = "https://todomd.online"

// hostAliases maps a root host to the app host that serves the same account
// and projects. The root keeps answering, so logins, links and attachment
// URLs made against either are one and the same.
var hostAliases = map[string]string{
	LegacyHost:                      DefaultHost,
	"https://staging.todomd.online": "https://app-staging.todomd.online",
}

// CanonicalHost maps a root host to its app host and leaves any other host
// (already the app's, localhost, a self-hosted one) as it is.
func CanonicalHost(host string) string {
	if app, ok := hostAliases[host]; ok {
		return app
	}
	return host
}

// RootHost is the host attachment links are written on: the root for an app
// host, the host itself otherwise.
func RootHost(host string) string {
	host = CanonicalHost(host)
	for root, app := range hostAliases {
		if app == host {
			return root
		}
	}
	return host
}

// User is an account as the API returns it. Handle is empty until the person
// picks one on the web.
type User struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// HostConfig is what `todomd login` stores for one host.
type HostConfig struct {
	API           string    `json:"api"`
	Token         string    `json:"token"`
	TokenID       string    `json:"token_id,omitempty"`
	Kind          string    `json:"kind,omitempty"` // device or scoped
	User          User      `json:"user"`
	CreatedAt     time.Time `json:"created_at"`
	MinCLIVersion string    `json:"min_cli_version,omitempty"`
	FormatVersion int       `json:"format_version,omitempty"`
}

// Hosts is the credentials file: one entry per host.
type Hosts struct {
	Hosts map[string]*HostConfig `json:"hosts"`
}

// ConfigDir is $XDG_CONFIG_HOME/todomd (default ~/.config/todomd).
func ConfigDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "todomd"), nil
}

func hostsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hosts.json"), nil
}

// LoadHosts reads the credentials file; a missing file is an empty one.
func LoadHosts() (*Hosts, error) {
	p, err := hostsPath()
	if err != nil {
		return nil, err
	}
	h := &Hosts{Hosts: map[string]*HostConfig{}}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, h); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if h.Hosts == nil {
		h.Hosts = map[string]*HostConfig{}
	}
	h.migrate()
	return h, nil
}

// migrate folds a login stored under a root host into its app host, so the
// two are one account. An existing app entry wins. The change reaches the file
// the next time it is saved.
func (h *Hosts) migrate() {
	for key, c := range h.Hosts {
		app := CanonicalHost(key)
		if app == key {
			continue
		}
		delete(h.Hosts, key)
		if c == nil || h.Hosts[app] != nil {
			continue
		}
		if c.API != "" {
			if rest, ok := strings.CutPrefix(c.API, key); ok {
				c.API = app + rest
			}
		}
		h.Hosts[app] = c
	}
}

// Save writes the credentials file: directory 0700, file 0600, atomically.
func (h *Hosts) Save() error {
	p, err := hostsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".hosts-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// NormalizeHost turns a host as typed (todomd.online, https://x/,
// http://localhost:5173) into scheme://host[:port]. Only https is accepted,
// except plain http to localhost for development, because the bearer token
// travels with every request.
func NormalizeHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty host")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid host %q", s)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("invalid host %q: give just the address, without a path", s)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return "", fmt.Errorf("refusing plain http for %s: only https is allowed (http only for localhost)", u.Host)
		}
	default:
		return "", fmt.Errorf("invalid host %q: want https://", s)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func isLoopback(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// ResolveHost picks the host: the flag, else $TODOMD_HOST, else DefaultHost.
// A root host comes back as its app host.
func ResolveHost(flag string) (string, error) {
	h := flag
	if h == "" {
		h = os.Getenv("TODOMD_HOST")
	}
	if h == "" {
		h = DefaultHost
	}
	h, err := NormalizeHost(h)
	if err != nil {
		return "", err
	}
	return CanonicalHost(h), nil
}

// HostName is the host without its scheme, for messages ("todomd.online").
func HostName(host string) string {
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		return u.Host
	}
	return host
}

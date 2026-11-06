package cloudsync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// gitRun runs git in dir and returns trimmed stdout. Any failure (no git, not
// a repo) is an error.
func gitRun(dir string, args ...string) (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	out, err := exec.Command(bin, append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// RepoRoot is the work tree holding file, or "" when it isn't in one.
func RepoRoot(file string) string {
	root, err := gitRun(filepath.Dir(file), "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return root
}

// GitHead is the commit HEAD points at, or "" (no repo, no commits).
func GitHead(file string) string {
	head, err := gitRun(filepath.Dir(file), "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return head
}

// OriginRepo is the normalized origin remote of file's repo, or "".
func OriginRepo(file string) string {
	u, err := gitRun(filepath.Dir(file), "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return NormalizeRemote(u)
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// NormalizeRemote turns a git remote into host/owner/repo: no scheme, user,
// port or .git suffix, lowercased. "git@github.com:walm/todomd.git" and
// "https://github.com/walm/todomd" both give "github.com/walm/todomd".
func NormalizeRemote(u string) string {
	u = strings.TrimSpace(u)
	var host, path string
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 && at < strings.Index(rest+"/", "/") {
			rest = rest[at+1:]
		}
		host, path, _ = strings.Cut(rest, "/")
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
	} else if m := scpLike.FindStringSubmatch(u); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host + "/" + path)
}

// IsTracked reports whether git has file in its index.
func IsTracked(file string) bool {
	_, err := gitRun(filepath.Dir(file), "ls-files", "--error-unmatch", "--", filepath.Base(file))
	return err == nil
}

// Untrack adds file to the repo's .gitignore (as /path/from/root) and, if
// git tracks it, removes it from the index only (git rm --cached). The file
// itself stays. It returns whether the index changed, which needs a commit.
func Untrack(file string) (indexChanged bool, err error) {
	root := RepoRoot(file)
	if root == "" {
		return false, errors.New("not in a git repository")
	}
	abs, err := filepath.EvalSymlinks(file)
	if err != nil {
		abs = file
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	rel, err := filepath.Rel(rootReal, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false, errors.New("the todo file is outside its git work tree")
	}
	entry := "/" + filepath.ToSlash(rel)
	gi := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	present := false
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == entry {
			present = true
		}
	}
	if !present {
		text := string(data)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += entry + "\n"
		if err := os.WriteFile(gi, []byte(text), 0o644); err != nil {
			return false, err
		}
	}
	if IsTracked(file) {
		if _, err := gitRun(filepath.Dir(file), "rm", "--cached", "--quiet", "--", filepath.Base(file)); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

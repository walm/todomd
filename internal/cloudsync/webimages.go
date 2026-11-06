package cloudsync

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/statedir"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// todomd-web keeps attachments in its own state folder,
// $XDG_STATE_HOME/todomd-web/attachments/<file key>/<task>/<name>, and links
// them by absolute path. Linking a file to todomd.online can bring the images
// among them over into todomd's folder, after which they sync like any other.

var webImageRe = regexp.MustCompile(`/(?:[^\s<>()\[\]"'` + "`" + `]*/)?todomd-web/attachments/[^\s/<>()\[\]]+/[^\s/<>()\[\]]+/[^\s/<>()\[\]]+`)

func webAttachmentRoot() (string, error) {
	g, err := statedir.Global()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(g), "todomd-web", "attachments"), nil
}

// webImage reads a todomd-web attachment if it really is one: a regular file
// under todomd-web's folder (after resolving symlinks) holding an image
// todomd.online accepts.
func webImage(path, rootReal string) ([]byte, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(real, rootReal+string(filepath.Separator)) {
		return nil, false
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() || st.Size() > cloud.MaxImageBytes {
		return nil, false
	}
	data, err := os.ReadFile(real)
	if err != nil || cloud.SniffImage(data) == "" {
		return nil, false
	}
	return data, true
}

// WebImages counts the todomd-web images the file links to that can be
// brought over.
func WebImages(f *task.File) int {
	root, err := webAttachmentRoot()
	if err != nil {
		return 0
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	mapTexts(f, func(s string) string {
		for _, p := range webImageRe.FindAllString(s, -1) {
			if _, ok := webImage(p, rootReal); ok {
				seen[p] = true
			}
		}
		return s
	})
	return len(seen)
}

// AdoptWebImages copies the todomd-web images the file links to into this
// machine's folder for the linked project and rewrites the links, under the
// store lock. It returns how many links it rewrote.
func AdoptWebImages(st *store.Store, l *Link) (int, error) {
	root, err := webAttachmentRoot()
	if err != nil {
		return 0, err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return 0, nil // no todomd-web attachments on this machine
	}
	n := 0
	err = st.Mutate(func(f *task.File) error {
		var ferr error
		out := mapTexts(f, func(s string) string {
			return webImageRe.ReplaceAllStringFunc(s, func(p string) string {
				data, ok := webImage(p, rootReal)
				if !ok || ferr != nil {
					return p
				}
				_, _, link, err := StoreImage(l, data, filepath.Base(p))
				if err != nil {
					ferr = err
					return p
				}
				n++
				return link
			})
		})
		*f = *out
		return ferr
	})
	return n, err
}

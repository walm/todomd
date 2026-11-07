package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/cloudsync"
	"github.com/walm/todomd/internal/store"
)

func newDownload() *cobra.Command {
	var outDir string
	cmd := &cobra.Command{
		Use:   "download <id> [file]",
		Short: "Download a file attached to a task on todomd.online",
		Long: `Fetch a file a task links to on todomd.online (a PDF, a zip, any type) and
save it in the current directory, or in --output. Sync downloads only
images; other files stay links in TODO.md, and this fetches one on request.

[file] is the file's name or attachment id (att_…); it can be left out when
the task links to only one. Uses the stored login, and refuses to overwrite
an existing file.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := syncStore()
			if err != nil {
				return err
			}
			l, err := cloudsync.LoadLink(st.Path)
			if err != nil {
				return err
			}
			f, err := st.Load()
			if err != nil {
				return err
			}
			b, i, err := store.FindTask(f, args[0])
			if err != nil {
				return err
			}
			t := b.Tasks[i]
			want := ""
			if len(args) == 2 {
				want = args[1]
			}
			fl, err := pickFileLink(cloudsync.TaskLinks(l, t), t.ID, want)
			if err != nil {
				return err
			}
			a, err := openAccount(l.Host, true)
			if err != nil {
				return err
			}
			if outDir == "" {
				outDir = "."
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			tmp, err := os.CreateTemp(outDir, ".todomd-download-*")
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			ctx, cancel := cmdContext()
			defer cancel()
			served, err := a.client.DownloadFile(ctx, l.ProjectID, fl.ID, fl.Name, tmp)
			if cerr := tmp.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return fmt.Errorf("%s: %w", fl.Name, err)
			}
			name := safeFileName(served)
			if name == "" {
				name = safeFileName(fl.Name)
			}
			if name == "" {
				name = fl.ID
			}
			path := filepath.Join(outDir, name)
			if _, err := os.Lstat(path); err == nil {
				return fmt.Errorf("%s already exists (use --output for another folder)", path)
			}
			if err := os.Chmod(tmp.Name(), 0o644); err != nil {
				return err
			}
			if err := os.Rename(tmp.Name(), path); err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(map[string]any{"task": t.ID, "id": fl.ID, "name": name, "path": path, "size": info.Size(), "url": fl.URL})
			}
			fmt.Printf("downloaded %s from %s to %s (%d bytes)\n", name, t.ID, path, info.Size())
			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "output", "o", "", "folder to save the file in (default: the current directory)")
	jsonFlag(cmd)
	return cmd
}

// pickFileLink chooses the attachment a download means: the one named by
// file name or att id, or the only one the task links to.
func pickFileLink(links []cloudsync.FileLink, taskID, want string) (cloudsync.FileLink, error) {
	if len(links) == 0 {
		return cloudsync.FileLink{}, fmt.Errorf("task %s links to no todomd.online attachments", taskID)
	}
	names := make([]string, len(links))
	for i, fl := range links {
		names[i] = fl.Name
		if want != "" && (strings.EqualFold(fl.Name, want) || strings.EqualFold(fl.ID, want)) {
			return fl, nil
		}
	}
	if want == "" && len(links) == 1 {
		return links[0], nil
	}
	if want == "" {
		return cloudsync.FileLink{}, fmt.Errorf("task %s links to several attachments; say which: %s", taskID, strings.Join(names, ", "))
	}
	return cloudsync.FileLink{}, fmt.Errorf("task %s links to no attachment %s (it has: %s)", taskID, want, strings.Join(names, ", "))
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeFileName makes a name from the server or a link safe to create in
// the output folder: no path, no hidden or dot-only names.
func safeFileName(name string) string {
	name = unsafeFileChars.ReplaceAllString(filepath.Base(strings.ReplaceAll(name, `\`, "/")), "-")
	name = strings.TrimLeft(name, ".-")
	if strings.Trim(name, ".") == "" {
		return ""
	}
	return name
}

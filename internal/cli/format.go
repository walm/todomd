package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/task"
)

// The hidden format commands let todomd.online's differential fuzzing (and
// anyone debugging a sync) run documents through exactly the Go code the CLI
// syncs with: canonicalize (parse → write), delta and merge. Inputs are
// files, "-" meaning stdin; output is JSON or the canonical text.

func readDoc(path string, strict bool) (*task.File, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if strict {
		return markdown.ParseStrict(data)
	}
	return markdown.Parse(data)
}

// formatError prints a parse error as {"error", "code", "line"} so a fuzzer
// can compare error classes, and returns it for the exit code.
func formatError(err error) error {
	var pe *markdown.ParseError
	if errors.As(err, &pe) {
		printJSON(map[string]any{"error": err.Error(), "code": pe.ErrCode(), "line": pe.Line})
	}
	return err
}

func newCanonicalize() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:    "canonicalize [file]",
		Short:  "Parse a todo file and write it back canonically",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "-"
			if len(args) == 1 {
				path = args[0]
			}
			f, err := readDoc(path, strict)
			if err != nil {
				return formatError(err)
			}
			_, err = os.Stdout.Write(markdown.Write(f))
			return err
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "reject what the parser would otherwise repair (todomd.online's rules)")
	return cmd
}

func newDelta() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "delta <a> <b>",
		Short:  "Print the ops that take todo file a to b",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := readDoc(args[0], false)
			if err != nil {
				return formatError(err)
			}
			b, err := readDoc(args[1], false)
			if err != nil {
				return formatError(err)
			}
			list := ops.Delta(a, b)
			if list == nil {
				list = []ops.Op{}
			}
			return json.NewEncoder(os.Stdout).Encode(list)
		},
	}
	return cmd
}

func newMergeCmd() *cobra.Command {
	var base, ours, theirs, date string
	var baseless, noDeletes bool
	cmd := &cobra.Command{
		Use:    "merge --base B --ours O --theirs T",
		Short:  "Three-way merge todo files as sync does; prints JSON",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var files [3]*task.File
			for i, p := range []string{base, ours, theirs} {
				if p == "" {
					files[i] = &task.File{}
					continue
				}
				f, err := readDoc(p, false)
				if err != nil {
					return formatError(err)
				}
				files[i] = f
			}
			opts := merge.Options{Baseless: baseless, NoDeletesFromOurs: noDeletes}
			if date != "" {
				d, err := task.ParseDate(date)
				if err != nil {
					return err
				}
				opts.Date = d
			}
			r, err := merge.Merge(files[0], files[1], files[2], opts)
			if err != nil {
				return err
			}
			conflicts, renamed := r.Conflicts, r.Renamed
			if conflicts == nil {
				conflicts = []merge.Conflict{}
			}
			if renamed == nil {
				renamed = []merge.Rename{}
			}
			return json.NewEncoder(os.Stdout).Encode(struct {
				Merged    string           `json:"merged"`
				Conflicts []merge.Conflict `json:"conflicts"`
				Renamed   []merge.Rename   `json:"renamed"`
			}{string(markdown.Write(r.Merged)), conflicts, renamed})
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base file (empty: an empty board, as for a first link)")
	cmd.Flags().StringVar(&ours, "ours", "", "ours: the pushed / local file")
	cmd.Flags().StringVar(&theirs, "theirs", "", "theirs: head / the server's result")
	cmd.Flags().StringVar(&date, "date", "", "date for conflict comments (YYYY-MM-DD, default today)")
	cmd.Flags().BoolVar(&baseless, "baseless", false, "first link: no common base")
	cmd.Flags().BoolVar(&noDeletes, "no-deletes-from-ours", false, "git import: nothing missing from ours is deleted")
	return cmd
}

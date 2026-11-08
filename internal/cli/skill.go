package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// skillText is the agent skill printed by 'todomd skill'.
//
//go:embed skill/SKILL.md
var skillText string

const defaultSkillDir = ".claude/skills/todomd"

func newSkill() *cobra.Command {
	var install, force bool
	var dir string
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Print (or install) a skill that teaches an agent to use todomd",
		Long: `Prints SKILL.md: how an agent reads the board, writes as itself, waits
for changes with 'todomd watch', and the default board rules (which are
yours to change). It reads the same for local and todomd.online boards.

--install writes it to <dir>/SKILL.md (default ` + defaultSkillDir + `,
for Claude Code; agents that read AGENTS.md can be pointed at the file). It
won't overwrite an existing SKILL.md without --force.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !install {
				if force || cmd.Flags().Changed("dir") {
					return errors.New("--dir and --force go with --install")
				}
				_, err := fmt.Print(skillText)
				return err
			}
			path := filepath.Join(dir, "SKILL.md")
			_, err := os.Stat(path)
			existed := err == nil
			switch {
			case existed && !force:
				return fmt.Errorf("%s already exists (--force to overwrite it)", path)
			case err != nil && !errors.Is(err, fs.ErrNotExist):
				return err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(skillText), 0o644); err != nil {
				return err
			}
			if flagJSON {
				return printJSON(map[string]any{"path": path, "overwritten": existed})
			}
			fmt.Printf("wrote %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "write SKILL.md instead of printing it")
	cmd.Flags().StringVar(&dir, "dir", defaultSkillDir, "with --install, the skill's folder")
	cmd.Flags().BoolVar(&force, "force", false, "with --install, overwrite an existing SKILL.md")
	jsonFlag(cmd)
	return cmd
}

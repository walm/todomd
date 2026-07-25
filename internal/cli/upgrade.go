package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/walm/todomd/internal/selfupdate"
)

func newUpgrade() *cobra.Command {
	var checkOnly, force bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade todomd to the latest release",
		Long: `Download the latest release for this platform and replace the running
binary with it, verifying the published sha256 checksum first.

--check reports what is available without writing anything.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			current := resolveVersion()
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			client := selfupdate.NewClient()
			latest, err := client.Latest(ctx)
			if err != nil {
				return fmt.Errorf("checking for releases: %w", err)
			}
			_ = selfupdate.SaveCache(latest, time.Now())

			upToDate := !selfupdate.Newer(current, latest)
			report := func(upgraded bool, path string) error {
				if flagJSON {
					return printJSON(struct {
						Current  string `json:"current"`
						Latest   string `json:"latest"`
						UpToDate bool   `json:"up_to_date"`
						Upgraded bool   `json:"upgraded"`
						Path     string `json:"path,omitempty"`
					}{current, latest, upToDate, upgraded, path})
				}
				switch {
				case upgraded:
					fmt.Printf("upgraded to todomd %s (%s)\n", latest, path)
				case !selfupdate.IsRelease(current):
					fmt.Printf("todomd %s is a development build; latest release is %s\n", current, latest)
				case upToDate:
					fmt.Printf("todomd %s is the latest release\n", current)
				default:
					fmt.Printf("todomd %s is available (you have %s) — run 'todomd upgrade'\n", latest, current)
				}
				return nil
			}

			if checkOnly || (upToDate && !force) {
				return report(false, "")
			}
			// A source build has no release it corresponds to, so upgrading
			// would silently swap it for a published binary.
			if !selfupdate.IsRelease(current) && !force {
				return fmt.Errorf("this is a development build (%s), not a release; "+
					"use 'git pull', or pass --force to install %s anyway", current, latest)
			}

			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if !flagJSON {
				fmt.Printf("downloading todomd %s…\n", latest)
			}
			bin, err := client.Download(ctx, latest)
			if err != nil {
				return err
			}
			if err := selfupdate.Replace(bin, exe); err != nil {
				return err
			}
			return report(true, exe)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report the latest release without installing it")
	cmd.Flags().BoolVar(&force, "force", false, "install even when already up to date or running a source build")
	jsonFlag(cmd)
	return cmd
}

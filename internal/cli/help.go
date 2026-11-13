package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// commandGroup is one heading of `todomd --help`.
type commandGroup struct {
	id, title string
	cmds      []*cobra.Command
}

// addCommandGroups adds the command tree under its help headings, in this
// order (cobra's alphabetical sorting is off, so the order here is the
// order shown).
func addCommandGroups(root *cobra.Command) {
	cobra.EnableCommandSorting = false
	groups := []commandGroup{
		{"tasks", "Tasks:", []*cobra.Command{newList(), newShow(), newAdd(), newUpdate(), newMove(), newDone(),
			newComment(), newReact(), newSticker(), newDelete(), newLinks(), newSplit()}},
		{"boards", "Boards:", []*cobra.Command{newBoards(), newArchive()}},
		{"online", "todomd.online:", []*cobra.Command{newLogin(), newLogout(), newWhoami(), newLink(), newUnlink(),
			newSync(), newOpen(), newAttach(), newDownload()}},
		{"agents", "Agents:", []*cobra.Command{newWatch(), newChanges(), newSkill()}},
		{"other", "Other:", []*cobra.Command{newInit(), newUpgrade(),
			newCanonicalize(), newDelta(), newMergeCmd()}}, // the last three are hidden
	}
	for _, g := range groups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, c := range g.cmds {
			c.GroupID = g.id
			root.AddCommand(c)
		}
	}
	root.SetHelpCommandGroupID("other")
	root.SetCompletionCommandGroupID("other")
	root.SetUsageTemplate(usageTemplate)
}

// helpColor reports whether help output gets colour: only on a terminal,
// and never with NO_COLOR set or TERM=dumb (https://no-color.org).
var helpColor = func() bool {
	return colorAllowed(term.IsTerminal(int(os.Stdout.Fd())))
}

func colorAllowed(tty bool) bool {
	return tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func ansi(code, s string) string {
	if !helpColor() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func init() {
	cobra.AddTemplateFunc("heading", func(s string) string { return ansi("1", s) })
	// cmdName pads like cobra's rpad, colouring the name only.
	cobra.AddTemplateFunc("cmdName", func(s string, width int) string {
		return ansi("36", s) + strings.Repeat(" ", max(width-len(s), 0))
	})
}

// usageTemplate is cobra's default, with headings and command names passed
// through heading and cmdName for colour.
const usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{heading .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{cmdName .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{cmdName .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

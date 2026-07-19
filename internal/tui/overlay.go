package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// compose draws fg centered on top of bg within a width×height canvas,
// splicing fg's lines into bg's at the right visual columns. SGR resets are
// inserted at the seams so background styling can't bleed into the overlay;
// a background segment right of the overlay may lose its opening color for
// that line, which is imperceptible for our subtle card borders.
func compose(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	fgW := lipgloss.Width(fg)
	x := max(0, (width-fgW)/2)
	y := max(0, (height-len(fgLines))/2)
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		left := ansi.Truncate(bgLines[row], x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(bgLines[row], x+fgW, "")
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

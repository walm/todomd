package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var detailBox = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(accent).
	Padding(0, 1)

// smallScreen reports whether the terminal is too cramped for the modal
// overlay, in which case the detail view takes the whole screen.
func (m *model) smallScreen() bool {
	return m.width < 60 || m.height < 16
}

// detailSize returns the inner content width and the maximum content height
// for the current mode (modal or full-screen).
func (m *model) detailSize() (w, maxH int) {
	if m.smallScreen() {
		return m.width - 2, m.height - 1
	}
	w = min(m.width-10, 92)
	maxH = m.height - 6
	return w - 4, maxH - 3 // border + padding; border + hint line
}

// openDetail (re)builds the detail viewport for the selected task.
func (m *model) openDetail() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	m.unread.markRead(m.file, t.ID)
	board := m.file.Boards[m.boardIdx].Name

	var md strings.Builder
	fmt.Fprintf(&md, "# %s\n\n", t.Title)
	fmt.Fprintf(&md, "`%s` · **%s**", t.ID, board)
	if len(t.Tags) > 0 {
		fmt.Fprintf(&md, " · #%s", strings.Join(t.Tags, " #"))
	}
	if t.Due != nil {
		fmt.Fprintf(&md, " · due **%s**", t.Due)
	}
	md.WriteString("\n")
	if t.Description != "" {
		fmt.Fprintf(&md, "\n%s\n", t.Description)
	}
	if len(t.Comments) > 0 {
		fmt.Fprintf(&md, "\n---\n\n## Comments (%d)\n\n", len(t.Comments))
		for _, c := range t.Comments {
			fmt.Fprintf(&md, "**%s** · %s\n\n%s\n\n", c.Author, c.Date, c.Text)
		}
	}

	w, maxH := m.detailSize()
	content := md.String()
	style := m.glamourStyle
	if style == "" {
		style = "notty" // never WithAutoStyle here: it queries the tty mid-run
	}
	if r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(w),
	); err == nil {
		if out, err := r.Render(content); err == nil {
			content = out
		}
	}
	content = strings.Trim(content, "\n")
	// Glamour pads every line to the wrap width with trailing spaces (often
	// followed by ANSI resets), so trim ansi-aware: measure the line without
	// trailing spaces, cut there, and re-terminate the styling.
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		tw := lipgloss.Width(strings.TrimRight(ansi.Strip(l), " "))
		if tw < lipgloss.Width(l) {
			l = ansi.Truncate(l, tw, "")
			if strings.Contains(l, "\x1b") {
				l += "\x1b[0m"
			}
			lines[i] = l
		}
	}
	content = strings.Join(lines, "\n")

	// The modal shrinks to its content (with a readable floor); only long
	// tasks fill maxH and scroll.
	if !m.smallScreen() {
		floor := min(44, w)
		w = max(min(lipgloss.Width(content), w), floor)
	}
	h := min(lipgloss.Height(content), maxH)
	m.vp = viewport.New(w, max(1, h))
	m.vp.SetContent(content)
}

func (m *model) detailHint() string {
	hint := "e edit · E editor · c comment · q/esc back"
	if m.vp.TotalLineCount() > m.vp.Height {
		hint = fmt.Sprintf("%3.0f%% · j/k scroll · %s", m.vp.ScrollPercent()*100, hint)
	}
	return lipgloss.NewStyle().Foreground(subtle).Render(hint)
}

func (m *model) viewDetail() string {
	if m.smallScreen() {
		return m.vp.View() + "\n " + m.detailHint()
	}
	box := detailBox.Render(m.vp.View() + "\n" + m.detailHint())
	return compose(m.viewBoard(), box, m.width, m.height)
}

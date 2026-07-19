package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// openDetail (re)builds the detail viewport for the selected task.
func (m *model) openDetail() {
	t := m.selectedTask()
	if t == nil {
		return
	}
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

	w := min(m.width-2, 100)
	content := md.String()
	if r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(w),
	); err == nil {
		if out, err := r.Render(content); err == nil {
			content = out
		}
	}

	m.vp = viewport.New(m.width, m.height-1)
	m.vp.SetContent(content)
}

func (m *model) viewDetail() string {
	hint := lipgloss.NewStyle().Foreground(subtle).
		Render(fmt.Sprintf("  %3.0f%% · j/k scroll · q/esc back", m.vp.ScrollPercent()*100))
	return m.vp.View() + "\n" + hint
}

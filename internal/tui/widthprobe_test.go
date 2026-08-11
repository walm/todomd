package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/walm/todomd/internal/task"
)

func TestWidthProbe(t *testing.T) {
	m := newTestModel(t, 1, 0)
	m.width, m.height = 100, 24
	m.glamourStyle = "notty"
	m.file.Boards[0].Tasks = []*task.Task{{
		ID: "51jp", Title: "Fix the parser bug in metadata handling",
		Tags: []string{"parser", "core"}, Priority: task.PriorityHigh,
		Description: strings.Repeat("Paragraph of a long description that needs scrolling to read.\n\n", 6),
	}}
	m.openDetail()
	head := strings.Split(m.detailHead, "\n")
	t.Logf("detailSize w=%d", func() int { w, _ := m.detailSize(); return w }())
	t.Logf("vp.Width=%d", m.vp.Width)
	for i, l := range head {
		t.Logf("head[%d] width=%d", i, lipgloss.Width(l))
	}
	t.Logf("vp content max line width=%d", lipgloss.Width(m.vp.View()))
}

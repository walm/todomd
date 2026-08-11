package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/walm/todomd/internal/task"
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

// detailHeader renders the part of the task that stays put while the body
// scrolls: what it is, where it lives, and how it's marked up.
func (m *model) detailHeader(t *task.Task, board string, w int) string {
	title := titleStyle.Foreground(accent).Width(w).Render(t.Title)
	if tl := strings.Split(title, "\n"); len(tl) > 2 {
		tl = tl[:2]
		tl[1] = ansi.Truncate(tl[1], w-1, "") + "…"
		title = strings.Join(tl, "\n")
	}

	meta := []string{countStyle.Render(t.ID), lipgloss.NewStyle().Bold(true).Render(board)}
	if len(t.Tags) > 0 {
		meta = append(meta, tagStyle.Render("#"+strings.Join(t.Tags, " #")))
	}
	switch t.Priority {
	case task.PriorityHigh:
		meta = append(meta, prioHigh.Render("▲ high"))
	case task.PriorityLow:
		meta = append(meta, prioLow.Render("▼ low"))
	}
	if t.Due != nil {
		style := dueStyle
		switch d := t.Due.DaysUntil(task.Today()); {
		case d < 0:
			style = overdueStyle
		case d <= 3:
			style = dueSoonStyle
		}
		meta = append(meta, style.Render("due "+t.Due.String()))
	}
	line := ansi.Truncate(strings.Join(meta, hintStyle.Render(" · ")), w, "…")
	return title + "\n" + line
}

// headerWidth is how wide the header would like to be, so the modal can size
// itself around the header as well as the body.
func (m *model) headerWidth(t *task.Task, board string) int {
	meta := t.ID + " · " + board
	if len(t.Tags) > 0 {
		meta += " · #" + strings.Join(t.Tags, " #")
	}
	if t.Priority != task.PriorityNormal {
		meta += " · ▲ high"
	}
	if t.Due != nil {
		meta += " · due " + t.Due.String()
	}
	return max(lipgloss.Width(t.Title), lipgloss.Width(meta))
}

// openDetail (re)builds the detail viewport for the selected task.
func (m *model) openDetail() {
	t := m.selectedTask()
	if t == nil {
		return
	}
	m.unread.markRead(m.file, t.ID)
	board := m.file.Boards[m.boardIdx].Name

	// The title and metadata live in the sticky header, so the scrolling
	// body is just the task's content.
	var md strings.Builder
	if t.Description != "" {
		fmt.Fprintf(&md, "%s\n", t.Description)
	}
	if len(t.Comments) > 0 {
		if t.Description != "" {
			md.WriteString("\n---\n")
		}
		fmt.Fprintf(&md, "\n## Comments (%d)\n\n", len(t.Comments))
		for _, c := range t.Comments {
			fmt.Fprintf(&md, "**%s** · %s\n\n%s\n\n", c.Author, c.Date, c.Text)
		}
	}
	if md.Len() == 0 {
		md.WriteString("*No description yet — press e to add one.*\n")
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

	// The modal shrinks to its content (with a readable floor), sized for the
	// header as well as the body; only long tasks fill maxH and scroll.
	if !m.smallScreen() {
		floor := min(44, w)
		want := max(lipgloss.Width(content), m.headerWidth(t, board))
		w = max(min(want, w), floor)
	}
	m.detailHead = m.detailHeader(t, board, w)
	bodyH := maxH - lipgloss.Height(m.detailHead) - 1 // -1 for the rule
	h := min(lipgloss.Height(content), bodyH)
	m.vp = viewport.New(w, max(1, h))
	m.vp.SetContent(content)
}

// detailHint renders the footer actions, highlighting the hovered one, and
// records the plain text for mouse hit-testing.
func (m *model) detailHint() string {
	prefix := ""
	if m.vp.TotalLineCount() > m.vp.Height {
		prefix = fmt.Sprintf("%3.0f%% · j/k g/G scroll · ", m.vp.ScrollPercent()*100)
	}
	plain, styled := prefix, hintStyle.Render(prefix)
	for i, a := range hintActions {
		if i > 0 {
			plain += " · "
			styled += hintStyle.Render(" · ")
		}
		plain += a.label
		if i == m.hintHover {
			styled += hintHoverStyle.Render(a.label)
		} else {
			styled += hintStyle.Render(a.label)
		}
	}
	m.plainHint = plain
	return styled
}

// detailPane stacks the sticky header, a rule spanning the whole pane, the
// scrolling body and the hint. The rule is sized last, once every part's width
// is known — the hint is usually the widest of them.
func (m *model) detailPane(minWidth int) string {
	body, hint := m.vp.View(), m.detailHint()
	w := max(minWidth, max(lipgloss.Width(m.detailHead),
		max(lipgloss.Width(body), lipgloss.Width(hint))))
	rule := hintStyle.Render(strings.Repeat("─", w))
	return m.detailHead + "\n" + rule + "\n" + body + "\n" + hint
}

func (m *model) viewDetail() string {
	if m.smallScreen() {
		out := m.detailPane(m.width - 1)
		m.plainHint = "" // no hint buttons; full-screen, so no tap-outside either
		m.detailRect = rect{0, 0, m.width, m.height}
		return out
	}
	box := detailBox.Render(m.detailPane(0))
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	m.detailRect = rect{max(0, (m.width-w)/2), max(0, (m.height-h)/2), w, h}
	return compose(m.viewBoard(), box, m.width, m.height)
}

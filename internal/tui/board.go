package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/walm/todomd/internal/task"
)

const minColWidth = 26

// layout returns how many columns fit and their width.
func (m *model) layout() (nVis, colW int) {
	n := len(m.file.Boards)
	if n == 0 {
		return 0, m.width
	}
	nVis = m.width / minColWidth
	if nVis < 1 {
		nVis = 1
	}
	if nVis > n {
		nVis = n
	}
	return nVis, m.width / nVis
}

func (m *model) viewBoard() string {
	footer := m.viewFooter()
	bodyH := m.height - lipgloss.Height(footer)

	if len(m.file.Boards) == 0 {
		empty := lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center,
			statusStyle.Render("no boards yet — press q and run: todomd add --board Backlog \"my first task\""))
		return empty + "\n" + footer
	}

	nVis, colW := m.layout()
	// Keep the selected column on screen.
	if m.boardIdx < m.colOffset {
		m.colOffset = m.boardIdx
	}
	if m.boardIdx >= m.colOffset+nVis {
		m.colOffset = m.boardIdx - nVis + 1
	}
	if m.colOffset > len(m.file.Boards)-nVis {
		m.colOffset = len(m.file.Boards) - nVis
	}
	if m.colOffset < 0 {
		m.colOffset = 0
	}

	cols := make([]string, 0, nVis)
	for i := m.colOffset; i < m.colOffset+nVis; i++ {
		b := m.file.Boards[i]
		active := i == m.boardIdx
		sel := -1
		if active {
			sel = m.cardIdx
		}
		var overflow string
		if i == m.colOffset && m.colOffset > 0 {
			overflow = "‹"
		}
		if i == m.colOffset+nVis-1 && m.colOffset+nVis < len(m.file.Boards) {
			overflow = "›"
		}
		cols = append(cols, m.renderColumn(b, colW, bodyH, active, sel, overflow))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return body + "\n" + footer
}

func (m *model) renderColumn(b *task.Board, w, h int, active bool, sel int, overflow string) string {
	hdrStyle := colHeader
	if active {
		hdrStyle = colHeaderActive
	}
	hdrText := fmt.Sprintf("%s %s", b.Name, countStyle.Render(fmt.Sprintf("(%d)", len(b.Tasks))))
	hdr := hdrStyle.Render(ansi.Truncate(hdrText, w-3, "…"))
	if overflow != "" {
		pad := w - lipgloss.Width(hdr) - 2
		if pad > 0 {
			hdr += strings.Repeat(" ", pad)
		}
		hdr += pagerStyle.Render(overflow)
	}

	cardH := h - 1 // header line
	var cards []string
	selStart, selEnd := 0, 0
	lineCount := 0
	for i, t := range b.Tasks {
		c := renderCard(t, w-2, active && i == sel)
		ch := lipgloss.Height(c)
		if i == sel {
			selStart, selEnd = lineCount, lineCount+ch
		}
		lineCount += ch
		cards = append(cards, c)
	}
	stack := lipgloss.JoinVertical(lipgloss.Left, cards...)
	lines := []string{}
	if len(cards) > 0 {
		lines = strings.Split(stack, "\n")
	}

	// Scroll the selected card into view.
	top := 0
	if sel >= 0 {
		if selEnd > cardH {
			top = selEnd - cardH
		}
		if selStart < top {
			top = selStart
		}
	}
	if top > 0 && top+cardH > len(lines) {
		top = max(0, len(lines)-cardH)
	}
	end := min(len(lines), top+cardH)
	visible := strings.Join(lines[top:end], "\n")

	col := hdr
	if visible != "" {
		col += "\n" + visible
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(col)
}

func renderCard(t *task.Task, w int, selected bool) string {
	inner := w - 4 // border + padding
	if inner < 4 {
		inner = 4
	}
	var parts []string

	title := titleStyle.Width(inner).Render(t.Title)
	tl := strings.Split(title, "\n")
	if len(tl) > 2 {
		tl = tl[:2]
		tl[1] = ansi.Truncate(tl[1], inner-1, "") + "…"
	}
	parts = append(parts, strings.Join(tl, "\n"))

	if len(t.Tags) > 0 {
		// Show the first two tags in full; a +N counter beats truncating
		// tag names mid-word.
		shown := t.Tags
		if len(shown) > 2 {
			shown = shown[:2]
		}
		tags := "#" + strings.Join(shown, " #")
		if extra := len(t.Tags) - len(shown); extra > 0 {
			tags += fmt.Sprintf(" +%d", extra)
		}
		parts = append(parts, tagStyle.Render(ansi.Truncate(tags, inner, "…")))
	}

	var meta []string
	if t.Due != nil {
		style := dueStyle
		switch d := t.Due.DaysUntil(task.Today()); {
		case d < 0:
			style = overdueStyle
		case d <= 3:
			style = dueSoonStyle
		}
		meta = append(meta, style.Render(t.Due.String()))
	}
	if n := len(t.Comments); n > 0 {
		meta = append(meta, countStyle.Render(fmt.Sprintf("(%d)", n)))
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " "))
	}

	style := card
	if selected {
		style = cardSelected
	}
	return style.Width(w - 2).Render(strings.Join(parts, "\n"))
}

func (m *model) viewFooter() string {
	var status string
	switch {
	case m.mode == modeConfirm:
		t := m.selectedTask()
		title := ""
		if t != nil {
			title = t.Title
		}
		status = errorStyle.Render(fmt.Sprintf("delete %q? (y/n)", ansi.Truncate(title, 40, "…")))
	case m.status != "":
		if m.isError {
			status = errorStyle.Render(m.status)
		} else {
			status = statusStyle.Render(m.status)
		}
	}
	helpView := m.help.View(m.keys)
	if status == "" {
		return helpView
	}
	return status + "\n" + helpView
}

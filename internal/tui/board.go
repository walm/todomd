package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/walm/todomd/internal/selfupdate"
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
	if shown, _ := m.matchCount(); shown == 0 && m.filter.active() {
		empty := lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center,
			hintStyle.Render("no cards match "+m.filter.describe()+" — esc to clear"))
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

	m.hits = m.hits[:0]
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
		cols = append(cols, m.renderColumn(i, b, colW, bodyH, active, sel, overflow, (i-m.colOffset)*colW))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return body + "\n" + footer
}

func (m *model) renderColumn(bi int, b *task.Board, w, h int, active bool, sel int, overflow string, x0 int) string {
	hdrStyle := colHeader
	if active {
		hdrStyle = colHeaderActive
	}
	tasks := m.visibleTasks(bi)
	count := fmt.Sprintf("(%d)", len(tasks))
	if m.filter.active() {
		count = fmt.Sprintf("(%d/%d)", len(tasks), len(b.Tasks))
	}
	hdrText := fmt.Sprintf("%s %s", b.Name, countStyle.Render(count))
	hdr := hdrStyle.Render(ansi.Truncate(hdrText, w-3, "…"))
	if overflow != "" {
		pad := w - lipgloss.Width(hdr) - 2
		if pad > 0 {
			hdr += strings.Repeat(" ", pad)
		}
		hdr += pagerStyle.Render(overflow)
	}
	frame := lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h)
	if len(tasks) == 0 {
		return frame.Render(hdr)
	}

	cardH := h - 1 // the header takes the first line
	// Render each card at most once per frame, and only the ones we actually
	// need: this loop is what keeps a board with thousands of cards as cheap
	// to draw as a board with ten.
	cache := map[int]string{}
	render := func(i int) string {
		if c, ok := cache[i]; ok {
			return c
		}
		t := tasks[i]
		c := renderCard(t, w-2, active && i == sel, m.unread.marks[t.ID])
		cache[i] = c
		return c
	}

	// Scroll position is a card index, so we never have to know the height of
	// the cards above the window — only of the ones we draw.
	top := 0
	if active {
		top = min(max(m.cardTop, 0), len(tasks)-1)
	}
	if sel >= 0 {
		// Walk back from the selection to find the earliest card that still
		// leaves it on screen, then keep the current scroll if it already
		// does, so the view doesn't jump around while moving one card at a
		// time.
		minTop, used := sel, lipgloss.Height(render(sel))
		for i := sel - 1; i >= 0; i-- {
			ch := lipgloss.Height(render(i))
			if used+ch > cardH {
				break
			}
			used += ch
			minTop = i
		}
		top = min(max(top, minTop), sel)
		m.cardTop = top
	}

	// Collect the window forward from top, then — if we ran out of cards
	// before filling the column — extend upwards with the tail of earlier
	// cards, so the bottom of a long column doesn't sit under dead space.
	type slot struct {
		card  int
		lines []string
	}
	var window []slot
	used := 0
	for i := top; i < len(tasks) && used < cardH; i++ {
		cl := strings.Split(render(i), "\n")
		window = append(window, slot{i, cl})
		used += len(cl)
	}
	for i := top - 1; i >= 0 && used < cardH; i-- {
		cl := strings.Split(render(i), "\n")
		if need := cardH - used; need < len(cl) {
			cl = cl[len(cl)-need:] // show this card's tail
		}
		window = append([]slot{{i, cl}}, window...)
		used += len(cl)
	}

	var lines []string
	for _, sl := range window {
		// Clickable rectangle (display row 0 is the header).
		m.hits = append(m.hits, hit{
			board: bi, card: sl.card,
			x0: x0, x1: x0 + w,
			y0: 1 + len(lines), y1: 1 + min(cardH, len(lines)+len(sl.lines)),
		})
		lines = append(lines, sl.lines...)
	}
	if len(lines) > cardH {
		lines = lines[:cardH] // a card taller than the column is clipped
	}

	col := hdr
	if len(lines) > 0 {
		col += "\n" + strings.Join(lines, "\n")
	}
	return frame.Render(col)
}

func renderCard(t *task.Task, w int, selected bool, mark markKind) string {
	inner := w - 4 // border + padding
	if inner < 4 {
		inner = 4
	}
	var parts []string

	titleText := t.Title
	switch mark {
	case markNew:
		titleText = "● " + titleText
	case markUpdated:
		titleText = "○ " + titleText
	}
	title := titleStyle.Width(inner).Render(titleText)
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
		meta = append(meta, style.Render(t.Due.String()))
	}
	if n := len(t.Comments); n > 0 {
		meta = append(meta, countStyle.Render(fmt.Sprintf("(%d)", n)))
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " "))
	}

	style := card
	switch {
	case selected:
		style = cardSelected
	case mark == markNew:
		style = cardNew
	case mark == markUpdated:
		style = cardUpdated
	}
	return style.Width(w - 2).Render(strings.Join(parts, "\n"))
}

// footerActions is the board's short help line; entries with a key are
// clickable and highlight on hover.
var footerActions = []struct{ label, key string }{
	{"h/l column", ""}, {"j/k card", ""}, {"enter open", ""}, {"a add", "a"},
	{"/ search", "/"}, {"u only changed", "u"}, {"? help", "?"}, {"q quit", "q"},
}

// footerHelp renders the short help with hover highlighting and records its
// plain text for hit-testing; the expanded (?) help stays bubbles/help.
func (m *model) footerHelp(budget int) string {
	if m.help.ShowAll {
		m.plainFooter = ""
		return m.help.View(m.keys)
	}
	plain, styled := "", ""
	for i, a := range footerActions {
		if i > 0 {
			plain += " • "
			styled += hintStyle.Render(" • ")
		}
		plain += a.label
		if a.key != "" && i == m.footHover {
			styled += hintHoverStyle.Render(a.label)
		} else {
			styled += hintStyle.Render(a.label)
		}
	}
	// Keep the line inside the terminal: a wrapped footer would throw the
	// board's height calculation off.
	if lipgloss.Width(styled) > budget {
		styled = ansi.Truncate(styled, budget, "…")
		plain = ansi.Truncate(plain, budget, "…")
	}
	m.plainFooter = plain
	return styled
}

// versionLabel is the build shown in the corner. A pseudo-version is far too
// long for a footer, so anything that isn't a release just reads "dev" —
// todomd --version still gives the full string.
func (m *model) versionLabel() string {
	if m.version == "" {
		return ""
	}
	if selfupdate.IsRelease(m.version) {
		return m.version
	}
	return "dev"
}

// withVersion right-aligns the version on the footer's last line, when there
// is room to spare after the help.
func (m *model) withVersion(help string) string {
	v := m.versionLabel()
	if v == "" {
		return help
	}
	lines := strings.Split(help, "\n")
	last := len(lines) - 1
	gap := m.width - lipgloss.Width(lines[last]) - lipgloss.Width(v)
	if gap < 1 {
		return help // the help matters more than the version
	}
	lines[last] += strings.Repeat(" ", gap) + hintStyle.Render(v)
	return strings.Join(lines, "\n")
}

func (m *model) viewFooter() string {
	var status string
	switch {
	case m.mode == modeConfirm && m.confirm != nil:
		status = errorStyle.Render(m.confirm.prompt + " (y/n)")
	case m.status != "":
		if m.isError {
			status = errorStyle.Render(m.status)
		} else {
			status = statusStyle.Render(m.status)
		}
	}
	lines := []string{}
	if m.mode == modeSearch {
		lines = append(lines, m.search.View())
	}
	if m.updateNotice != "" {
		lines = append(lines, hintStyle.Render(m.updateNotice))
	}
	if status != "" {
		lines = append(lines, status)
	}
	// Reserve the corner for the version before laying out the help.
	budget := m.width
	if v := m.versionLabel(); v != "" && m.width-lipgloss.Width(v)-2 >= 20 {
		budget = m.width - lipgloss.Width(v) - 2
	}
	lines = append(lines, m.withVersion(m.footerHelp(budget)))
	return strings.Join(lines, "\n")
}

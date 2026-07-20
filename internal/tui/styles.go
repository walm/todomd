package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent  = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#AD8EE6"}
	subtle  = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}
	warnCol = lipgloss.AdaptiveColor{Light: "#B58900", Dark: "#E5C07B"}
	dangCol = lipgloss.AdaptiveColor{Light: "#D70000", Dark: "#E06C75"}
	okCol   = lipgloss.AdaptiveColor{Light: "#005F5F", Dark: "#56B6C2"}

	colHeader       = lipgloss.NewStyle().Bold(true).Foreground(subtle).Padding(0, 1)
	colHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1)

	card = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(subtle).
		Padding(0, 1)
	cardSelected = card.BorderForeground(accent)
	cardNew      = card.BorderForeground(okCol)
	cardUpdated  = card.BorderForeground(warnCol)

	titleStyle   = lipgloss.NewStyle().Bold(true)
	tagStyle     = lipgloss.NewStyle().Foreground(okCol)
	dueStyle     = lipgloss.NewStyle().Foreground(subtle)
	dueSoonStyle = lipgloss.NewStyle().Foreground(warnCol)
	overdueStyle = lipgloss.NewStyle().Foreground(dangCol).Bold(true)
	countStyle   = lipgloss.NewStyle().Foreground(subtle)

	statusStyle = lipgloss.NewStyle().Foreground(accent)
	errorStyle  = lipgloss.NewStyle().Foreground(dangCol)
	pagerStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)

	formBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)
	formLabel = lipgloss.NewStyle().Foreground(subtle)
	formTitle = lipgloss.NewStyle().Bold(true).Foreground(accent)

	btnStyle      = lipgloss.NewStyle().Foreground(accent)
	btnHoverStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"}).
			Background(accent).Bold(true)
	hintStyle      = lipgloss.NewStyle().Foreground(subtle)
	hintHoverStyle = lipgloss.NewStyle().Foreground(accent).Underline(true)
)

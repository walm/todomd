package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// ribbonColors are the ribbon stickers' colours.
var ribbonColors = map[string]lipgloss.AdaptiveColor{
	"red":    {Light: "#D70000", Dark: "#E06C75"},
	"amber":  {Light: "#C77C00", Dark: "#E5A50A"},
	"green":  {Light: "#2E8B2E", Dark: "#7EC27E"},
	"blue":   {Light: "#1F6FD1", Dark: "#61AFEF"},
	"violet": {Light: "#7B3FC4", Dark: "#C678DD"},
}

func ribbonStyle(name string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ribbonColors[name]).Bold(true)
}

// stickerLabel is a sticker in running text: "🔥 fire" or a coloured
// "◥ red"; "" for none.
func stickerLabel(name string) string {
	switch {
	case name == "":
		return ""
	case task.IsRibbon(name):
		return ribbonStyle(name).Render("◥ " + name)
	default:
		return task.StickerEmoji(name) + " " + name
	}
}

// withStickerCorner puts a card's sticker on the right end of its rendered
// top border: an emoji overlapping the border just inside the corner, or a
// ribbon colouring the corner itself. border is the card's border colour.
func withStickerCorner(card, name string, border lipgloss.TerminalColor) string {
	if name == "" {
		return card
	}
	lines := strings.SplitN(card, "\n", 2)
	top := lines[0]
	w := lipgloss.Width(top)
	bs := lipgloss.NewStyle().Foreground(border)
	var mark string
	if task.IsRibbon(name) {
		mark = ribbonStyle(name).Render("━━◥")
	} else {
		mark = task.StickerEmoji(name) + bs.Render("─╮")
	}
	keep := w - lipgloss.Width(mark)
	if keep < 2 {
		return card
	}
	lines[0] = ansi.Truncate(top, keep, "") + "\x1b[0m" + mark
	return strings.Join(lines, "\n")
}

// stickerChoices is the picker's grid: every sticker, then "none".
var stickerChoices = func() []string {
	out := make([]string, 0, len(task.Stickers)+1)
	for _, s := range task.Stickers {
		out = append(out, s.Name)
	}
	return append(out, "")
}()

const stickerCols = 3

// picker is the open sticker picker (the s key).
type picker struct {
	id, title string
	sel       int  // index into stickerChoices
	from      mode // view to return to
}

func (m *model) openStickerPicker(from mode) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	sel := 0 // the first emoji, for a card without a sticker
	for i, n := range stickerChoices {
		if n != "" && n == t.Sticker {
			sel = i
		}
	}
	m.picker = &picker{id: t.ID, title: t.Title, sel: sel, from: from}
	m.mode = modeSticker
}

func (m *model) closeStickerPicker() {
	m.mode = m.picker.from
	m.picker = nil
}

func (m *model) applySticker(name string) {
	p := m.picker
	m.closeStickerPicker()
	msg := "sticker " + task.StickerLabel(name)
	if name == "" {
		msg = "sticker removed"
	}
	m.mutate(func(f *task.File) error {
		_, err := store.Update(f, p.id, store.UpdateOpts{Sticker: &name})
		return err
	}, p.id, msg)
	if p.from == modeDetail {
		if t := m.selectedTask(); t == nil || t.ID != p.id {
			m.mode = modeBoard // the search no longer shows the card
		} else {
			m.openDetail() // re-render the header with the new sticker
		}
	}
}

// updateSticker: arrows or hjkl move, enter picks, 1–9 pick the first nine
// emoji, 0 or x removes the sticker, esc closes.
func (m *model) updateSticker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	n := len(stickerChoices)
	switch s := msg.String(); s {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "s":
		m.closeStickerPicker()
	case "enter", " ":
		m.applySticker(stickerChoices[p.sel])
	case "0", "x":
		m.applySticker("")
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		m.applySticker(stickerChoices[int(s[0]-'1')])
	case "left", "h":
		p.sel = (p.sel + n - 1) % n
	case "right", "l", "tab":
		p.sel = (p.sel + 1) % n
	case "up", "k":
		if p.sel >= stickerCols {
			p.sel -= stickerCols
		}
	case "down", "j":
		p.sel = min(p.sel+stickerCols, n-1)
	}
	return m, nil
}

func (m *model) viewSticker() string {
	p := m.picker
	var rows []string
	var row []string
	for i, name := range stickerChoices {
		key := " "
		if i < 9 {
			key = fmt.Sprint(i + 1)
		}
		label := "none"
		if name != "" {
			label = stickerLabel(name)
		} else {
			key = "0"
		}
		cell := hintStyle.Render(key) + " " + label
		cell += strings.Repeat(" ", max(0, 14-lipgloss.Width(cell)))
		if i == p.sel {
			cell = optionSelectedFocus.Render(ansi.Strip(cell))
		}
		row = append(row, cell)
		if len(row) == stickerCols || i == len(stickerChoices)-1 {
			rows = append(rows, strings.Join(row, " "))
			row = nil
		}
	}
	title := formTitle.Render("Sticker for " + ansi.Truncate("“"+p.title+"”", 34, "…"))
	hint := hintStyle.Render("←→↑↓ move · enter pick · 1–9 · 0 none · esc")
	box := formBox.Render(title + "\n\n" + strings.Join(rows, "\n") + "\n\n" + hint)
	base := m.viewBoard()
	if p.from == modeDetail {
		base = m.viewDetail()
	}
	return compose(base, box, m.width, m.height)
}

// stickerQuery takes the sticker terms out of a search: `has:sticker` or
// `sticker:any` (any sticker), `sticker:none`, and `sticker:NAME` or
// `sticker:EMOJI`. It returns the rest of the query and the sticker test,
// nil when there is none.
func stickerQuery(q string) (string, func(*task.Task) bool) {
	words := strings.Fields(q)
	var rest []string
	var tests []func(*task.Task) bool
	for _, w := range words {
		lw := strings.ToLower(w)
		switch {
		case lw == "has:sticker" || lw == "sticker:any":
			tests = append(tests, func(t *task.Task) bool { return t.Sticker != "" })
		case lw == "sticker:none":
			tests = append(tests, func(t *task.Task) bool { return t.Sticker == "" })
		case strings.HasPrefix(lw, "sticker:"):
			name, err := task.ParseSticker(w[len("sticker:"):])
			if err != nil || name == "" {
				rest = append(rest, w) // not a sticker: plain text
				continue
			}
			tests = append(tests, func(t *task.Task) bool { return t.Sticker == name })
		default:
			rest = append(rest, w)
		}
	}
	if len(tests) == 0 {
		return q, nil
	}
	return strings.Join(rest, " "), func(t *task.Task) bool {
		for _, ok := range tests {
			if !ok(t) {
				return false
			}
		}
		return true
	}
}

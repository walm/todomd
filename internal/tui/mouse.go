package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// hit is a clickable card rectangle, rebuilt on every board render.
type hit struct {
	board, card    int
	x0, x1, y0, y1 int
}

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// hintActions are the clickable segments of the detail footer, dispatched
// by replaying the corresponding key through the normal handler.
var hintActions = []struct{ label, key string }{
	{"e edit", "e"}, {"E editor", "E"}, {"c comment", "c"}, {"q/esc back", "q"},
}

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		switch m.mode {
		case modeDetail:
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		case modeBoard:
			k := "j"
			if msg.Button == tea.MouseButtonWheelUp {
				k = "k"
			}
			return m.updateBoard(keyRunes(k))
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	switch m.mode {
	case modeBoard:
		if msg.Y == 0 { // column header selects the column
			if _, colW := m.layout(); colW > 0 {
				if i := m.colOffset + msg.X/colW; i < len(m.file.Boards) {
					m.boardIdx, m.cardIdx = i, 0
				}
			}
			return m, nil
		}
		for _, h := range m.hits {
			if msg.X >= h.x0 && msg.X < h.x1 && msg.Y >= h.y0 && msg.Y < h.y1 {
				if h.board == m.boardIdx && h.card == m.cardIdx {
					m.openDetail() // clicking the selected card opens it
					m.mode = modeDetail
				} else {
					m.boardIdx, m.cardIdx = h.board, h.card
				}
				return m, nil
			}
		}
	case modeDetail:
		if !m.detailRect.contains(msg.X, msg.Y) {
			m.mode = modeBoard // tap outside the card closes it
			return m, nil
		}
		if msg.Y == m.detailRect.y+m.detailRect.h-2 { // footer hint line
			rel := msg.X - (m.detailRect.x + 2)
			for _, a := range hintActions {
				if i := strings.Index(m.plainHint, a.label); i >= 0 && rel >= i && rel < i+len(a.label) {
					return m.updateDetail(keyRunes(a.key))
				}
			}
		}
	}
	return m, nil
}

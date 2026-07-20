// Package tui is the Kanban terminal UI. It renders from a display model
// and funnels every mutation through the store (lock → fresh load → apply →
// write), then reloads — so it never edits a stale model behind the file's
// back.
package tui

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// Run starts the TUI over the given store.
func Run(s *store.Store) error {
	f, err := s.Load()
	if err != nil {
		return err
	}
	// Resolve light/dark now, while we still own the tty. Querying later
	// (e.g. glamour's WithAutoStyle inside openDetail) deadlocks: bubbletea's
	// input reader swallows the OSC response, the query blocks until its
	// timeout, and the response bytes leak in as phantom keypresses.
	// GLAMOUR_STYLE skips the terminal query entirely (useful for terminals
	// that never answer OSC 11, where the query stalls startup).
	style := os.Getenv("GLAMOUR_STYLE")
	if style == "" {
		dark := lipgloss.HasDarkBackground()
		lipgloss.SetHasDarkBackground(dark) // pin: never re-query mid-run
		style = "light"
		if dark {
			style = "dark"
		}
	} else {
		lipgloss.SetHasDarkBackground(style != "light")
	}
	m := newModel(s, f)
	m.glamourStyle = style
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err = p.Run()
	return err
}

type mode int

const (
	modeBoard mode = iota
	modeDetail
	modeForm
	modeConfirm
)

type keyMap struct {
	Left, Right, Up, Down    key.Binding
	First, Last              key.Binding
	MoveLeft, MoveRight      key.Binding
	MoveDown, MoveUp         key.Binding
	Open, Add, Edit, Comment key.Binding
	Editor                   key.Binding
	Delete, Done, Reload     key.Binding
	Help, Quit               key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Left:      key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/l", "column")),
		Right:     key.NewBinding(key.WithKeys("l", "right")),
		Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "card")),
		Down:      key.NewBinding(key.WithKeys("j", "down")),
		First:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g/G", "top/bottom")),
		Last:      key.NewBinding(key.WithKeys("G")),
		MoveLeft:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H/L", "move task")),
		MoveRight: key.NewBinding(key.WithKeys("L")),
		MoveDown:  key.NewBinding(key.WithKeys("J"), key.WithHelp("J/K", "reorder")),
		MoveUp:    key.NewBinding(key.WithKeys("K")),
		Open:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Editor:    key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "$EDITOR")),
		Comment:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
		Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Done:      key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "done")),
		Reload:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Left, k.Up, k.Open, k.Add, k.MoveLeft, k.Done, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Left, k.Up, k.First, k.Open},
		{k.MoveLeft, k.MoveDown, k.Done, k.Delete},
		{k.Add, k.Edit, k.Editor, k.Comment},
		{k.Reload, k.Help, k.Quit},
	}
}

type model struct {
	store *store.Store
	file  *task.File

	width, height int
	boardIdx      int
	cardIdx       int
	colOffset     int

	mode      mode
	vp        viewport.Model
	form      *form
	formFrom  mode // view to return to when the form closes
	confirmID string

	status       string
	isError      bool
	help         help.Model
	keys         keyMap
	glamourStyle string // resolved before the program starts; never query mid-run
	unread       *unread

	lastMod  time.Time // file stat as of the last load, gates auto-reload
	lastSize int64

	hits       []hit  // card rectangles, rebuilt by viewBoard for mouse hits
	detailRect rect   // modal box position, set by viewDetail
	plainHint  string // unstyled detail footer, for hint-button hit-testing
	hintHover  int    // hovered detail-footer action index, -1 none
}

// autoReloadEvery is the stat-poll interval: the board picks up external
// changes within this window while idle. Reloads happen only when the
// file's mtime/size actually changed, so the steady-state cost is one
// stat() per tick.
const autoReloadEvery = 2 * time.Second

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(autoReloadEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) recordStat() {
	if st, err := os.Stat(m.store.Path); err == nil {
		m.lastMod, m.lastSize = st.ModTime(), st.Size()
	}
}

func (m *model) fileChanged() bool {
	st, err := os.Stat(m.store.Path)
	if err != nil {
		return false
	}
	return !st.ModTime().Equal(m.lastMod) || st.Size() != m.lastSize
}

func newModel(s *store.Store, f *task.File) *model {
	m := &model{store: s, file: f, help: help.New(), keys: newKeyMap(), hintHover: -1}
	m.unread = loadUnread(s.Path, f)
	m.recordStat()
	if n := len(m.unread.marks); n > 0 {
		m.setStatus(fmt.Sprintf("%s changed since your last visit", plural(n, "card")), false)
	}
	return m
}

func (m *model) Init() tea.Cmd { return tickCmd() }

func (m *model) selectedTask() *task.Task {
	if m.boardIdx >= len(m.file.Boards) {
		return nil
	}
	b := m.file.Boards[m.boardIdx]
	if m.cardIdx >= len(b.Tasks) {
		return nil
	}
	return b.Tasks[m.cardIdx]
}

func (m *model) clamp() {
	if len(m.file.Boards) == 0 {
		m.boardIdx, m.cardIdx, m.colOffset = 0, 0, 0
		return
	}
	m.boardIdx = min(m.boardIdx, len(m.file.Boards)-1)
	if m.boardIdx < 0 {
		m.boardIdx = 0
	}
	n := len(m.file.Boards[m.boardIdx].Tasks)
	if m.cardIdx >= n {
		m.cardIdx = n - 1
	}
	if m.cardIdx < 0 {
		m.cardIdx = 0
	}
}

// selectByID points the selection at the task with the given ID, if present.
func (m *model) selectByID(id string) {
	for bi, b := range m.file.Boards {
		for ti, t := range b.Tasks {
			if t.ID == id {
				m.boardIdx, m.cardIdx = bi, ti
				return
			}
		}
	}
	m.clamp()
}

func (m *model) setStatus(s string, isErr bool) {
	m.status, m.isError = s, isErr
}

// mutate runs fn through the store, reloads, and re-selects followID.
// The user's own change never shows an unread badge.
func (m *model) mutate(fn func(*task.File) error, followID, okMsg string) {
	if err := m.store.Mutate(fn); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.reload()
	if followID != "" {
		m.selectByID(followID)
		m.unread.markRead(m.file, followID)
	}
	m.setStatus(okMsg, false)
}

func (m *model) reload() {
	f, err := m.store.Load()
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	var selID string
	if t := m.selectedTask(); t != nil {
		selID = t.ID
	}
	m.file = f
	m.clamp()
	if selID != "" {
		m.selectByID(selID) // keep the selection on the same card if it survived
	}
	m.unread.recompute(m.file)
	m.recordStat()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		if m.mode == modeDetail {
			m.openDetail() // re-render for the new size
		}
		return m, nil
	case tea.KeyMsg:
		// Keys arriving faster than the input loop reads them coalesce into
		// one multi-rune KeyMsg that matches no binding; replay them one by
		// one. Forms keep the batch — there it's real text (typing, paste).
		if msg.Type == tea.KeyRunes && !msg.Paste && len(msg.Runes) > 1 && m.mode != modeForm {
			var res tea.Model = m
			var cmd tea.Cmd
			for _, r := range msg.Runes {
				res, cmd = res.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
			return res, cmd
		}
		switch m.mode {
		case modeBoard:
			return m.updateBoard(msg)
		case modeDetail:
			return m.updateDetail(msg)
		case modeForm:
			return m.updateForm(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		}
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case editorFinishedMsg:
		m.applyEditor(msg)
	case tickMsg:
		// Auto-reload only while idling on the board — never yank the file
		// out from under an open task, form, or confirm prompt.
		if m.mode == modeBoard && m.fileChanged() {
			before := len(m.unread.marks)
			m.reload()
			if after := len(m.unread.marks); after != before && after > 0 {
				m.setStatus(fmt.Sprintf("%s with unseen changes", plural(after, "card")), false)
			}
		}
		return m, tickCmd()
	}
	return m, nil
}

func (m *model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	m.status = ""
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Left):
		if m.boardIdx > 0 {
			m.boardIdx--
			m.cardIdx = 0
		}
	case key.Matches(msg, k.Right):
		if m.boardIdx < len(m.file.Boards)-1 {
			m.boardIdx++
			m.cardIdx = 0
		}
	case key.Matches(msg, k.Down):
		if t := m.file; m.boardIdx < len(t.Boards) && m.cardIdx < len(t.Boards[m.boardIdx].Tasks)-1 {
			m.cardIdx++
		}
	case key.Matches(msg, k.Up):
		if m.cardIdx > 0 {
			m.cardIdx--
		}
	case key.Matches(msg, k.First):
		m.cardIdx = 0
	case key.Matches(msg, k.Last):
		if m.boardIdx < len(m.file.Boards) {
			m.cardIdx = max(0, len(m.file.Boards[m.boardIdx].Tasks)-1)
		}
	case key.Matches(msg, k.MoveLeft):
		m.moveToAdjacent(-1)
	case key.Matches(msg, k.MoveRight):
		m.moveToAdjacent(+1)
	case key.Matches(msg, k.MoveDown):
		m.reorder(+1)
	case key.Matches(msg, k.MoveUp):
		m.reorder(-1)
	case key.Matches(msg, k.Open):
		if m.selectedTask() != nil {
			m.openDetail()
			m.mode = modeDetail
		}
	case key.Matches(msg, k.Add):
		if len(m.file.Boards) == 0 {
			m.setStatus("no boards — add one with: todomd add --board <name> <title>", true)
			break
		}
		m.form = newTaskForm(m.width, m.height, nil, m.file.Boards[m.boardIdx].Name)
		m.formFrom = modeBoard
		m.mode = modeForm
	case key.Matches(msg, k.Edit):
		if t := m.selectedTask(); t != nil {
			m.form = newTaskForm(m.width, m.height, t, m.file.Boards[m.boardIdx].Name)
			m.formFrom = modeBoard
			m.mode = modeForm
		}
	case key.Matches(msg, k.Editor):
		return m, m.openEditor(modeBoard)
	case key.Matches(msg, k.Comment):
		if t := m.selectedTask(); t != nil {
			m.form = newCommentForm(m.width, m.height, t.ID, defaultAuthor())
			m.formFrom = modeBoard
			m.mode = modeForm
		}
	case key.Matches(msg, k.Delete):
		if t := m.selectedTask(); t != nil {
			m.confirmID = t.ID
			m.mode = modeConfirm
		}
	case key.Matches(msg, k.Done):
		if t := m.selectedTask(); t != nil {
			id := t.ID
			m.mutate(func(f *task.File) error {
				_, err := store.Move(f, id, "Done", 0)
				return err
			}, id, "moved to Done")
		}
	case key.Matches(msg, k.Reload):
		m.reload()
		m.setStatus("reloaded", false)
	case key.Matches(msg, k.Help):
		m.help.ShowAll = !m.help.ShowAll
	}
	return m, nil
}

func (m *model) moveToAdjacent(dir int) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	ni := m.boardIdx + dir
	if ni < 0 || ni >= len(m.file.Boards) {
		return
	}
	id, target := t.ID, m.file.Boards[ni].Name
	m.mutate(func(f *task.File) error {
		_, err := store.Move(f, id, target, 0)
		return err
	}, id, "moved to "+target)
}

func (m *model) reorder(dir int) {
	t := m.selectedTask()
	if t == nil {
		return
	}
	b := m.file.Boards[m.boardIdx]
	newIdx := m.cardIdx + dir
	if newIdx < 0 || newIdx >= len(b.Tasks) {
		return
	}
	id, pos := t.ID, newIdx+1
	m.mutate(func(f *task.File) error {
		_, err := store.Move(f, id, "", pos)
		return err
	}, id, "")
}

func (m *model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.mode = modeBoard
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "c":
		if t := m.selectedTask(); t != nil {
			m.form = newCommentForm(m.width, m.height, t.ID, defaultAuthor())
			m.formFrom = modeDetail
			m.mode = modeForm
		}
		return m, nil
	case "e":
		if t := m.selectedTask(); t != nil {
			m.form = newTaskForm(m.width, m.height, t, m.file.Boards[m.boardIdx].Name)
			m.formFrom = modeDetail
			m.mode = modeForm
		}
		return m, nil
	case "E":
		return m, m.openEditor(modeDetail)
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, canceled, cmd := m.form.update(msg)
	if canceled {
		m.mode = m.formFrom
		m.form = nil
		return m, nil
	}
	if done {
		m.submitForm()
		return m, nil
	}
	return m, cmd
}

func (m *model) submitForm() {
	f := m.form
	// Validate before closing, so a failed save keeps the typed content on
	// screen with the error inside the form.
	var vals taskValues
	if f.kind == formAdd || f.kind == formEdit {
		var err error
		if vals, err = f.taskValues(); err != nil {
			f.err = err.Error()
			return
		}
	}
	from := m.formFrom
	m.mode = modeBoard
	m.form = nil
	m.formFrom = modeBoard
	switch f.kind {
	case formComment:
		id := f.targetID
		text := f.desc.Value()
		author := f.title.Value()
		m.mutate(func(file *task.File) error {
			_, err := store.AddComment(file, id, author, text)
			return err
		}, id, "comment added")
		// Commenting from the open task returns to it, scrolled to the
		// new comment.
		if from == modeDetail && !m.isError {
			m.openDetail()
			m.vp.GotoBottom()
			m.mode = modeDetail
		}
	case formAdd:
		board := f.board
		var newID string
		m.mutate(func(file *task.File) error {
			t := &task.Task{Title: vals.title, Description: vals.desc, Tags: vals.tags, Due: vals.due}
			added, err := store.Add(file, board, t)
			if err == nil {
				newID = added.ID
			}
			return err
		}, "", "added")
		if newID != "" {
			m.selectByID(newID)
			m.unread.markRead(m.file, newID)
		}
	case formEdit:
		id := f.targetID
		m.mutate(func(file *task.File) error {
			opts := store.UpdateOpts{
				Title:       &vals.title,
				Description: &vals.desc,
				Tags:        &vals.tags,
				ClearDue:    vals.due == nil,
				Due:         vals.due,
			}
			_, err := store.Update(file, id, opts)
			return err
		}, id, "updated")
		if from == modeDetail && !m.isError {
			m.openDetail()
			m.mode = modeDetail
		}
	}
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		id := m.confirmID
		m.mutate(func(f *task.File) error {
			_, _, err := store.Delete(f, id)
			return err
		}, "", "deleted")
	case "ctrl+c":
		return m, tea.Quit
	}
	m.confirmID = ""
	m.mode = modeBoard
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	switch m.mode {
	case modeDetail:
		return m.viewDetail()
	case modeForm:
		return m.viewForm()
	default:
		return m.viewBoard()
	}
}

func defaultAuthor() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "human"
}

func plural(n int, s string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, s)
	}
	return fmt.Sprintf("%d %ss", n, s)
}

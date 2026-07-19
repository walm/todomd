// Package tui is the Kanban terminal UI. It renders from a display model
// and funnels every mutation through the store (lock → fresh load → apply →
// write), then reloads — so it never edits a stale model behind the file's
// back.
package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// Run starts the TUI over the given store.
func Run(s *store.Store) error {
	f, err := s.Load()
	if err != nil {
		return err
	}
	p := tea.NewProgram(newModel(s, f), tea.WithAltScreen())
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
		{k.Add, k.Edit, k.Comment, k.Reload},
		{k.Help, k.Quit},
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
	confirmID string

	status  string
	isError bool
	help    help.Model
	keys    keyMap
}

func newModel(s *store.Store, f *task.File) *model {
	return &model{store: s, file: f, help: help.New(), keys: newKeyMap()}
}

func (m *model) Init() tea.Cmd { return nil }

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
func (m *model) mutate(fn func(*task.File) error, followID, okMsg string) {
	if err := m.store.Mutate(fn); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.reload()
	if followID != "" {
		m.selectByID(followID)
	}
	m.setStatus(okMsg, false)
}

func (m *model) reload() {
	f, err := m.store.Load()
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.file = f
	m.clamp()
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
		m.mode = modeForm
	case key.Matches(msg, k.Edit):
		if t := m.selectedTask(); t != nil {
			m.form = newTaskForm(m.width, m.height, t, m.file.Boards[m.boardIdx].Name)
			m.mode = modeForm
		}
	case key.Matches(msg, k.Comment):
		if t := m.selectedTask(); t != nil {
			m.form = newCommentForm(m.width, m.height, t.ID, defaultAuthor())
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
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, canceled, cmd := m.form.update(msg)
	if canceled {
		m.mode = modeBoard
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
	m.mode = modeBoard
	m.form = nil
	switch f.kind {
	case formComment:
		id := f.targetID
		text := f.desc.Value()
		author := f.title.Value()
		m.mutate(func(file *task.File) error {
			_, err := store.AddComment(file, id, author, text)
			return err
		}, id, "comment added")
	case formAdd:
		vals, err := f.taskValues()
		if err != nil {
			m.setStatus(err.Error(), true)
			return
		}
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
		}
	case formEdit:
		vals, err := f.taskValues()
		if err != nil {
			m.setStatus(err.Error(), true)
			return
		}
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
		return m.form.view()
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

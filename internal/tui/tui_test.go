package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func newTestModel(t *testing.T, boards int, tasksPer int) *model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "TODO.md")
	f := &task.File{Title: "T"}
	for i := 0; i < boards; i++ {
		b := &task.Board{Name: string(rune('A' + i))}
		for j := 0; j < tasksPer; j++ {
			b.Tasks = append(b.Tasks, &task.Task{Title: "task"})
		}
		f.Boards = append(f.Boards, b)
	}
	f.AssignIDs()
	s := &store.Store{Path: path}
	if err := s.Save(f); err != nil {
		t.Fatal(err)
	}
	m := newModel(s, f)
	m.width, m.height = 100, 30
	return m
}

func keyMsg(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	panic("unknown key " + s)
}

func TestLayoutPaging(t *testing.T) {
	m := newTestModel(t, 5, 1)
	cases := []struct {
		width, wantVis int
	}{
		{200, 5}, // all fit
		{100, 3}, // 100/26 = 3
		{52, 2},  // two columns
		{30, 1},  // one column
		{10, 1},  // never below one
	}
	for _, c := range cases {
		m.width = c.width
		nVis, colW := m.layout()
		if nVis != c.wantVis {
			t.Errorf("width %d: nVis = %d, want %d", c.width, nVis, c.wantVis)
		}
		if colW*nVis > c.width {
			t.Errorf("width %d: columns overflow (%d * %d)", c.width, nVis, colW)
		}
	}
}

func TestPagingFollowsSelection(t *testing.T) {
	m := newTestModel(t, 6, 1)
	m.width = 100 // 3 visible
	for i := 0; i < 5; i++ {
		m.updateBoard(keyMsg("l"))
	}
	m.viewBoard() // triggers offset adjustment
	if m.boardIdx != 5 {
		t.Fatalf("boardIdx = %d", m.boardIdx)
	}
	nVis, _ := m.layout()
	if m.boardIdx < m.colOffset || m.boardIdx >= m.colOffset+nVis {
		t.Errorf("selected column %d not visible at offset %d (+%d)", m.boardIdx, m.colOffset, nVis)
	}
	for i := 0; i < 5; i++ {
		m.updateBoard(keyMsg("h"))
	}
	m.viewBoard()
	if m.colOffset != 0 || m.boardIdx != 0 {
		t.Errorf("offset %d idx %d after paging back", m.colOffset, m.boardIdx)
	}
}

func TestNavigationAndClamp(t *testing.T) {
	m := newTestModel(t, 2, 3)
	m.updateBoard(keyMsg("j"))
	m.updateBoard(keyMsg("j"))
	if m.cardIdx != 2 {
		t.Errorf("cardIdx = %d", m.cardIdx)
	}
	m.updateBoard(keyMsg("j")) // clamped at bottom
	if m.cardIdx != 2 {
		t.Errorf("cardIdx overran: %d", m.cardIdx)
	}
	m.updateBoard(keyMsg("g"))
	if m.cardIdx != 0 {
		t.Errorf("g: cardIdx = %d", m.cardIdx)
	}
	m.updateBoard(keyMsg("G"))
	if m.cardIdx != 2 {
		t.Errorf("G: cardIdx = %d", m.cardIdx)
	}
	m.updateBoard(keyMsg("l"))
	if m.boardIdx != 1 || m.cardIdx != 0 {
		t.Errorf("l: board %d card %d", m.boardIdx, m.cardIdx)
	}
}

func TestMoveAndReorderMutations(t *testing.T) {
	m := newTestModel(t, 2, 2)
	first := m.file.Boards[0].Tasks[0]
	// Move to next board; selection follows.
	m.updateBoard(keyMsg("L"))
	if len(m.file.Boards[1].Tasks) != 3 {
		t.Fatalf("move failed: %d tasks on B", len(m.file.Boards[1].Tasks))
	}
	if got := m.selectedTask(); got == nil || got.ID != first.ID {
		t.Errorf("selection did not follow moved task")
	}
	// Reorder up within the new board.
	m.updateBoard(keyMsg("K"))
	if m.file.Boards[1].Tasks[1].ID != first.ID {
		t.Errorf("reorder up failed: %v", m.file.Boards[1].Tasks)
	}
	// Persisted to disk.
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Boards[1].Tasks) != 3 {
		t.Errorf("mutation not persisted")
	}
}

func TestDetailAndBack(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("enter"))
	if m.mode != modeDetail {
		t.Fatalf("mode = %d", m.mode)
	}
	m.updateDetail(keyMsg("q"))
	if m.mode != modeBoard {
		t.Errorf("q should return to board")
	}
}

func TestEmptyFileView(t *testing.T) {
	m := newTestModel(t, 0, 0)
	// Must not panic and should render something.
	if m.viewBoard() == "" {
		t.Error("empty view")
	}
	m.updateBoard(keyMsg("j"))
	m.updateBoard(keyMsg("enter"))
	m.updateBoard(keyMsg("L"))
	if m.mode != modeBoard {
		t.Errorf("mode changed on empty file")
	}
}

func TestBatchedRunesReplayIndividually(t *testing.T) {
	m := newTestModel(t, 1, 4)
	// Fast input coalesces into one multi-rune KeyMsg; each rune must still
	// act as its own keypress outside forms.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jj")})
	if m.cardIdx != 2 {
		t.Errorf("cardIdx = %d, want 2", m.cardIdx)
	}
}

func TestCommentFromDetail(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("enter"))
	if m.mode != modeDetail {
		t.Fatal("not in detail")
	}
	m.updateDetail(keyMsg("c"))
	if m.mode != modeForm || m.form == nil || m.form.kind != formComment {
		t.Fatalf("c in detail should open comment form (mode=%d)", m.mode)
	}
	// Esc returns to the detail view, not the board.
	m.updateForm(keyMsg("esc"))
	if m.mode != modeDetail {
		t.Errorf("esc from form should return to detail, mode=%d", m.mode)
	}
	// Submit a comment and land back in detail with it persisted.
	m.updateDetail(keyMsg("c"))
	m.form.title.SetValue("tester")
	m.form.desc.SetValue("a comment from the modal")
	m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeDetail {
		t.Errorf("submit should return to detail, mode=%d", m.mode)
	}
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cs := f.Boards[0].Tasks[0].Comments
	if len(cs) != 1 || cs[0].Author != "tester" || cs[0].Text != "a comment from the modal" {
		t.Errorf("comment not persisted: %+v", cs)
	}
}

func TestCardShowsFirstTwoTagsPlusCount(t *testing.T) {
	tk := &task.Task{ID: "aaaa", Title: "T", Tags: []string{"alpha", "beta", "gamma", "delta"}}
	card := renderCard(tk, 40, false, markNone)
	for _, want := range []string{"#alpha", "#beta", "+2"} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "gamma") {
		t.Errorf("card should not name the third tag:\n%s", card)
	}
}

func TestApplyEditorResult(t *testing.T) {
	m := newTestModel(t, 1, 1)
	id := m.file.Boards[0].Tasks[0].ID
	frag := "### Edited in vim\n<!-- id:" + id + " -->\n`#viatag` **due:** 2026-09-01\n\nNew body.\n\n#### Comments\n\n- **walm** (2026-07-19): kept\n"
	tmp := filepath.Join(t.TempDir(), "frag.md")
	if err := os.WriteFile(tmp, []byte(frag), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(editorFinishedMsg{path: tmp, id: id, from: modeDetail})
	if m.mode != modeDetail {
		t.Errorf("should return to detail, mode=%d", m.mode)
	}
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	tk := f.Boards[0].Tasks[0]
	if tk.Title != "Edited in vim" || len(tk.Tags) != 1 || tk.Tags[0] != "viatag" ||
		tk.Due == nil || tk.Description != "New body." || len(tk.Comments) != 1 {
		t.Errorf("edit not applied: %+v", tk)
	}
	if tk.ID != id {
		t.Errorf("id changed: %s -> %s", id, tk.ID)
	}
}

func TestApplyEditorRejectsBadFragment(t *testing.T) {
	m := newTestModel(t, 1, 1)
	id := m.file.Boards[0].Tasks[0].ID
	tmp := filepath.Join(t.TempDir(), "frag.md")
	os.WriteFile(tmp, []byte("### One\n\n### Two\n"), 0o644)
	m.Update(editorFinishedMsg{path: tmp, id: id, from: modeBoard})
	if !m.isError {
		t.Error("bad fragment should set an error status")
	}
	f, _ := m.store.Load()
	if f.Boards[0].Tasks[0].Title == "One" {
		t.Error("bad fragment must not be applied")
	}
}

func TestUnreadMarks(t *testing.T) {
	m := newTestModel(t, 2, 2)
	id := m.file.Boards[0].Tasks[0].ID

	// External activity: an agent comments and adds a task via the store.
	err := m.store.Mutate(func(f *task.File) error {
		_, err := store.AddComment(f, id, "agent", "ping from the agent")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var newID string
	err = m.store.Mutate(func(f *task.File) error {
		tk, err := store.Add(f, "A", &task.Task{Title: "From agent"})
		if err == nil {
			newID = tk.ID
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	m.updateBoard(keyMsg("r")) // reload picks up the external changes
	if m.unread.marks[id] != markUpdated {
		t.Errorf("commented task mark = %d, want updated", m.unread.marks[id])
	}
	if m.unread.marks[newID] != markNew {
		t.Errorf("agent task mark = %d, want new", m.unread.marks[newID])
	}
	if !strings.Contains(m.viewBoard(), "● From agent") {
		t.Error("new badge not rendered on card")
	}

	// Opening the commented card clears only its badge.
	m.selectByID(id)
	m.updateBoard(keyMsg("enter"))
	if m.unread.marks[id] != markNone {
		t.Error("opening a card should clear its badge")
	}
	if m.unread.marks[newID] != markNew {
		t.Error("other badges must survive")
	}

	// A fresh session (new model) sees the same state: read stays read,
	// unseen stays marked.
	f2, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	m2 := newModel(m.store, f2)
	m2.width, m2.height = 100, 30
	if m2.unread.marks[id] != markNone {
		t.Error("read state did not persist across sessions")
	}
	if m2.unread.marks[newID] != markNew {
		t.Error("unseen badge did not persist across sessions")
	}

	// The user's own action (move to Done via D) never leaves a badge.
	m2.selectByID(newID)
	m2.updateBoard(keyMsg("D"))
	if m2.unread.marks[newID] != markNone {
		t.Error("own mutation must not badge the card")
	}
}

func TestAutoReloadOnTick(t *testing.T) {
	m := newTestModel(t, 2, 1)
	// External change while the TUI idles on the board.
	err := m.store.Mutate(func(f *task.File) error {
		_, err := store.Add(f, "A", &task.Task{Title: "Appeared externally"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	if len(m.file.Boards[0].Tasks) != 2 {
		t.Error("tick did not reload the file")
	}
	found := false
	for id, k := range m.unread.marks {
		_ = id
		if k == markNew {
			found = true
		}
	}
	if !found {
		t.Error("auto-reload should badge the new task")
	}

	// No further change: tick must not disturb anything (stat gate).
	before := m.file
	m.Update(tickMsg{})
	if m.file != before {
		t.Error("tick reloaded without a file change")
	}

	// In detail mode the tick must not reload.
	m.updateBoard(keyMsg("enter"))
	err = m.store.Mutate(func(f *task.File) error {
		_, err := store.Add(f, "A", &task.Task{Title: "While reading"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	if len(m.file.Boards[0].Tasks) != 2 {
		t.Error("tick reloaded while a task was open")
	}
	// Back on the board, the next tick catches up.
	m.updateDetail(keyMsg("esc"))
	m.Update(tickMsg{})
	if len(m.file.Boards[0].Tasks) != 3 {
		t.Error("tick after returning to board did not reload")
	}
}

func TestReloadPreservesSelection(t *testing.T) {
	m := newTestModel(t, 1, 3)
	id := m.file.Boards[0].Tasks[2].ID
	m.selectByID(id)
	// External reorder: selected task moves to the top.
	err := m.store.Mutate(func(f *task.File) error {
		_, err := store.Move(f, id, "", 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	if got := m.selectedTask(); got == nil || got.ID != id {
		t.Errorf("selection lost after auto-reload: %+v", got)
	}
	if m.cardIdx != 0 {
		t.Errorf("cardIdx = %d, want 0 (followed the task)", m.cardIdx)
	}
}

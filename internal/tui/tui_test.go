package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
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

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func TestMouseSelectOpenAndClose(t *testing.T) {
	m := newTestModel(t, 2, 2)
	m.viewBoard() // build hit rects
	var target hit
	for _, h := range m.hits {
		if h.board == 0 && h.card == 1 {
			target = h
		}
	}
	if target.x1 == 0 {
		t.Fatalf("no hit rect for card, hits=%+v", m.hits)
	}
	// First click selects, second click (same card) opens.
	m.handleMouse(click(target.x0+2, target.y0))
	if m.boardIdx != 0 || m.cardIdx != 1 || m.mode != modeBoard {
		t.Fatalf("click should select: board=%d card=%d mode=%d", m.boardIdx, m.cardIdx, m.mode)
	}
	m.handleMouse(click(target.x0+2, target.y0))
	if m.mode != modeDetail {
		t.Fatal("second click should open the card")
	}
	m.View() // sets detailRect + plainHint

	// Tap outside the modal closes it.
	m.handleMouse(click(0, m.height-1))
	if m.mode != modeBoard {
		t.Error("tap outside should close the card")
	}

	// Click inside the modal (not the hint line) does nothing.
	m.handleMouse(click(target.x0+2, target.y0)) // reopen (still selected)
	m.View()
	m.handleMouse(click(m.detailRect.x+2, m.detailRect.y+1))
	if m.mode != modeDetail {
		t.Error("click inside the card should not close it")
	}

	// Click the "c comment" hint button.
	hintY := m.detailRect.y + m.detailRect.h - 2
	i := labelCol(m.plainHint, "c comment")
	if i < 0 {
		t.Fatalf("plainHint = %q", m.plainHint)
	}
	m.handleMouse(click(m.detailRect.x+2+i, hintY))
	if m.mode != modeForm || m.form == nil || m.form.kind != formComment {
		t.Errorf("hint click should open comment form, mode=%d", m.mode)
	}
}

func TestMouseHeaderSelectsColumn(t *testing.T) {
	m := newTestModel(t, 3, 1)
	m.viewBoard()
	_, colW := m.layout()
	m.handleMouse(click(colW*2+1, 0))
	if m.boardIdx != 2 {
		t.Errorf("header click: boardIdx = %d, want 2", m.boardIdx)
	}
}

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionMotion, X: x, Y: y}
}

func TestFormButtonsHoverClickAndValidation(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("a"))
	if m.mode != modeForm {
		t.Fatal("form did not open")
	}
	m.View() // records button rects

	// Hover states.
	m.handleMouse(motion(m.form.saveRect.x+1, m.form.saveRect.y))
	if m.form.hover != 0 {
		t.Errorf("hover = %d, want save", m.form.hover)
	}
	m.handleMouse(motion(m.form.cancelRect.x+1, m.form.cancelRect.y))
	if m.form.hover != 1 {
		t.Errorf("hover = %d, want cancel", m.form.hover)
	}
	m.handleMouse(motion(0, 0))
	if m.form.hover != -1 {
		t.Errorf("hover = %d, want none", m.form.hover)
	}

	// Clicking Save with an empty title keeps the form open with the error.
	m.handleMouse(click(m.form.saveRect.x+1, m.form.saveRect.y))
	if m.mode != modeForm || m.form.err == "" {
		t.Fatalf("failed save should stay in form with error, mode=%d err=%q", m.mode, m.form.err)
	}
	if !strings.Contains(m.View(), "must not be empty") {
		t.Error("validation error not rendered in the form")
	}

	// Fill the title and click Save: task is created.
	m.form.title.SetValue("Clicked into existence")
	m.View()
	m.handleMouse(click(m.form.saveRect.x+1, m.form.saveRect.y))
	if m.mode != modeBoard {
		t.Fatalf("save click should close the form, mode=%d", m.mode)
	}
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Boards[0].Tasks) != 2 {
		t.Error("task not created by save click")
	}

	// Cancel click closes without saving.
	m.updateBoard(keyMsg("a"))
	m.View()
	m.form.title.SetValue("Should never exist")
	m.View()
	m.handleMouse(click(m.form.cancelRect.x+1, m.form.cancelRect.y))
	if m.mode != modeBoard {
		t.Fatal("cancel click should close the form")
	}
	f, _ = m.store.Load()
	if len(f.Boards[0].Tasks) != 2 {
		t.Error("cancel click must not save")
	}
}

func TestDetailHintHover(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("enter"))
	m.View()
	i := labelCol(m.plainHint, "c comment")
	hintY := m.detailRect.y + m.detailRect.h - 2
	m.handleMouse(motion(m.detailRect.x+2+i, hintY))
	if m.hintHover != 2 {
		t.Errorf("hintHover = %d, want 2 (comment)", m.hintHover)
	}
	m.handleMouse(motion(0, 0))
	if m.hintHover != -1 {
		t.Errorf("hintHover = %d, want -1", m.hintHover)
	}
}

func TestFooterClickAndHover(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.height = 30
	m.viewBoard() // records plainFooter
	i := labelCol(m.plainFooter, "a add")
	if i < 0 {
		t.Fatalf("plainFooter = %q", m.plainFooter)
	}
	// Hover highlights only clickable segments.
	m.handleMouse(motion(i+1, m.height-1))
	if m.footHover < 0 || footerActions[m.footHover].key != "a" {
		t.Errorf("footHover = %d", m.footHover)
	}
	j := labelCol(m.plainFooter, "h/l column")
	m.handleMouse(motion(j+1, m.height-1))
	if m.footHover != -1 {
		t.Errorf("inert segment should not hover, footHover = %d", m.footHover)
	}
	// Click "a add" opens the add form.
	m.handleMouse(click(i+1, m.height-1))
	if m.mode != modeForm || m.form.kind != formAdd {
		t.Errorf("footer click should open add form, mode=%d", m.mode)
	}
	m.updateForm(keyMsg("esc"))
	// Click "? help" toggles full help.
	m.viewBoard()
	k := labelCol(m.plainFooter, "? help")
	m.handleMouse(click(k+1, m.height-1))
	if !m.help.ShowAll {
		t.Error("footer click should toggle help")
	}
}

// Regression: the footer separators are multi-byte (•), so byte offsets sit
// right of the visible labels — hit-testing must use display columns.
func TestFooterHitAtVisiblePosition(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.height = 30
	m.viewBoard()
	byteIdx := strings.Index(m.plainFooter, "a add")
	col := labelCol(m.plainFooter, "a add")
	if byteIdx <= col {
		t.Fatalf("test premise broken: byteIdx=%d col=%d", byteIdx, col)
	}
	if got := m.footerActionAt(col, m.height-1); got < 0 || footerActions[got].key != "a" {
		t.Errorf("visible position must hit, got %d", got)
	}
	if got := m.footerActionAt(byteIdx, m.height-1); got >= 0 {
		t.Errorf("byte-offset position (right of label) must miss, got %d", got)
	}
}

func TestPriorityCycleAndCard(t *testing.T) {
	m := newTestModel(t, 1, 1)
	id := m.file.Boards[0].Tasks[0].ID

	// p steps normal → high → low → normal, persisting each time.
	for _, want := range []task.Priority{task.PriorityHigh, task.PriorityLow, task.PriorityNormal} {
		m.updateBoard(keyMsg("p"))
		if got := m.file.Boards[0].Tasks[0].Priority; got != want {
			t.Fatalf("after p: priority = %v, want %v", got, want)
		}
		f, err := m.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if f.Boards[0].Tasks[0].Priority != want {
			t.Errorf("priority not persisted: %v", f.Boards[0].Tasks[0].Priority)
		}
	}
	_ = id

	// Cards mark the non-default levels.
	high := &task.Task{ID: "aaaa", Title: "T", Priority: task.PriorityHigh}
	if !strings.Contains(renderCard(high, 40, false, markNone), "high") {
		t.Error("high card should show its priority")
	}
	normal := &task.Task{ID: "bbbb", Title: "T"}
	card := renderCard(normal, 40, false, markNone)
	if strings.Contains(card, "high") || strings.Contains(card, "low") || strings.Contains(card, "normal") {
		t.Errorf("normal card should stay unmarked:\n%s", card)
	}
}

func TestFormCarriesPriority(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("a"))
	m.form.title.SetValue("New task")
	m.form.prio = task.PriorityHigh
	m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var found *task.Task
	for _, tk := range f.Boards[0].Tasks {
		if tk.Title == "New task" {
			found = tk
		}
	}
	if found == nil || found.Priority != task.PriorityHigh {
		t.Fatalf("form priority not applied: %+v", found)
	}
	// The select can't hold an invalid priority at all; a bad due date still
	// keeps the form open with its error rather than discarding the input.
	m.updateBoard(keyMsg("a"))
	m.form.title.SetValue("Bad")
	m.form.due.SetValue("not-a-date")
	m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeForm || m.form.err == "" {
		t.Errorf("invalid due should keep the form open, mode=%d err=%q", m.mode, m.form.err)
	}
}

func TestPrioritySelect(t *testing.T) {
	m := newTestModel(t, 1, 1)
	m.updateBoard(keyMsg("a"))
	f := m.form
	if f.prio != task.PriorityNormal {
		t.Fatalf("new task should start at normal, got %v", f.prio)
	}
	// Tab to the priority field (title → tags → priority).
	m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	m.updateForm(tea.KeyMsg{Type: tea.KeyTab})
	if f.focus != 2 {
		t.Fatalf("focus = %d, want the priority select", f.focus)
	}
	// → raises, ← lowers, and both clamp at the ends.
	m.updateForm(keyMsg("right"))
	if f.prio != task.PriorityHigh {
		t.Errorf("→ gave %v, want high", f.prio)
	}
	m.updateForm(keyMsg("right"))
	if f.prio != task.PriorityHigh {
		t.Errorf("→ past the end should clamp, got %v", f.prio)
	}
	m.updateForm(keyMsg("left"))
	m.updateForm(keyMsg("left"))
	if f.prio != task.PriorityLow {
		t.Errorf("← gave %v, want low", f.prio)
	}
	m.updateForm(keyMsg("left"))
	if f.prio != task.PriorityLow {
		t.Errorf("← past the start should clamp, got %v", f.prio)
	}
	// Stray typing can't corrupt a select.
	m.updateForm(keyMsg("x"))
	if f.prio != task.PriorityLow {
		t.Errorf("typing changed the select: %v", f.prio)
	}

	// Every option is clickable, and clicking focuses the field.
	m.View() // records option rects
	if len(f.prioRects) != 3 {
		t.Fatalf("prioRects = %d, want 3", len(f.prioRects))
	}
	m.setFocus0()
	r := f.prioRects[2] // "high"
	m.handleMouse(click(r.x, r.y))
	if f.prio != task.PriorityHigh {
		t.Errorf("clicking 'high' gave %v", f.prio)
	}
	if f.focus != 2 {
		t.Errorf("clicking an option should focus the select, focus=%d", f.focus)
	}
	// Hover marks the option under the pointer, and only that one.
	m.handleMouse(motion(f.prioRects[0].x, f.prioRects[0].y))
	if f.prioHover != 0 {
		t.Errorf("prioHover = %d, want 0", f.prioHover)
	}
	m.handleMouse(motion(0, 0))
	if f.prioHover != -1 {
		t.Errorf("prioHover = %d, want -1 off the options", f.prioHover)
	}
}

// setFocus0 puts the form back on its first field, for tests that then assert
// a click moves focus.
func (m *model) setFocus0() { m.form.setFocus(0) }

// selVisible reports whether the selected card was actually drawn: every
// rendered card records a hit rect, so presence there means on screen.
func (m *model) selVisible() bool {
	for _, h := range m.hits {
		if h.board == m.boardIdx && h.card == m.cardIdx {
			return true
		}
	}
	return false
}

// The column renders a window of cards, so navigation must keep the selection
// inside it — in both directions and after jumps.
func TestColumnScrollKeepsSelectionVisible(t *testing.T) {
	m := newTestModel(t, 1, 200)
	m.width, m.height = 60, 20

	m.viewBoard()
	if !m.selVisible() {
		t.Fatal("first card not visible at rest")
	}
	// Walk all the way down.
	for i := 1; i < 200; i++ {
		m.updateBoard(keyMsg("j"))
		m.viewBoard()
		if !m.selVisible() {
			t.Fatalf("card %d not visible while scrolling down (cardTop=%d)", m.cardIdx, m.cardTop)
		}
	}
	// And back up.
	for i := 198; i >= 0; i-- {
		m.updateBoard(keyMsg("k"))
		m.viewBoard()
		if !m.selVisible() {
			t.Fatalf("card %d not visible while scrolling up (cardTop=%d)", m.cardIdx, m.cardTop)
		}
	}
	// Jumps to either end.
	m.updateBoard(keyMsg("G"))
	m.viewBoard()
	if m.cardIdx != 199 || !m.selVisible() {
		t.Errorf("G: cardIdx=%d visible=%v", m.cardIdx, m.selVisible())
	}
	m.updateBoard(keyMsg("g"))
	m.viewBoard()
	if m.cardIdx != 0 || !m.selVisible() || m.cardTop != 0 {
		t.Errorf("g: cardIdx=%d top=%d visible=%v", m.cardIdx, m.cardTop, m.selVisible())
	}
}

// Only what's on screen is drawn (and therefore clickable) — that's what makes
// the render independent of how many tasks the board holds.
func TestColumnRendersOnlyVisibleCards(t *testing.T) {
	m := newTestModel(t, 1, 500)
	m.width, m.height = 60, 20
	out := m.viewBoard()
	if len(m.hits) > 12 {
		t.Errorf("rendered %d cards into a 20-row terminal", len(m.hits))
	}
	if len(m.hits) == 0 {
		t.Fatal("nothing rendered")
	}
	if got := lipgloss.Height(out); got != m.height {
		t.Errorf("board height = %d, want %d", got, m.height)
	}
	// The window sits at the top until the selection moves.
	if m.hits[0].card != 0 {
		t.Errorf("window starts at card %d, want 0", m.hits[0].card)
	}
	// Scrolled to the end, the window holds the last card and nothing beyond.
	m.updateBoard(keyMsg("G"))
	m.viewBoard()
	last := m.hits[len(m.hits)-1]
	if last.card != 499 {
		t.Errorf("last rendered card = %d, want 499", last.card)
	}
}

// Switching columns must not carry the previous column's scroll offset over.
func TestColumnScrollResetsAcrossColumns(t *testing.T) {
	m := newTestModel(t, 2, 100)
	m.width, m.height = 60, 20
	m.updateBoard(keyMsg("G")) // scroll the first column to the bottom
	m.viewBoard()
	if m.cardTop == 0 {
		t.Fatal("expected the first column to be scrolled")
	}
	m.updateBoard(keyMsg("l")) // move to the second column, selection at 0
	m.viewBoard()
	if m.cardTop != 0 || !m.selVisible() {
		t.Errorf("switching columns left top=%d visible=%v", m.cardTop, m.selVisible())
	}
}

// A card taller than the column is still shown, clipped, rather than dropping
// the column or overflowing the frame.
func TestOversizedCardIsClipped(t *testing.T) {
	m := newTestModel(t, 1, 3)
	m.width, m.height = 60, 7 // ~5 rows of cards
	m.file.Boards[0].Tasks[0].Title = strings.Repeat("very long title ", 20)
	m.file.Boards[0].Tasks[0].Tags = []string{"a", "b"}
	out := m.viewBoard()
	if lipgloss.Height(out) != m.height {
		t.Errorf("height = %d, want %d", lipgloss.Height(out), m.height)
	}
	if !m.selVisible() {
		t.Error("oversized selected card should still be rendered")
	}
}

func TestMarkAllRead(t *testing.T) {
	m := newTestModel(t, 2, 2)
	ids := []string{m.file.Boards[0].Tasks[0].ID, m.file.Boards[1].Tasks[0].ID}

	// Someone else changes two tasks and adds a third.
	err := m.store.Mutate(func(f *task.File) error {
		for _, id := range ids {
			if _, err := store.AddComment(f, id, "agent", "ping"); err != nil {
				return err
			}
		}
		_, err := store.Add(f, "A", &task.Task{Title: "brand new"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m.updateBoard(keyMsg("r"))
	if len(m.unread.marks) != 3 {
		t.Fatalf("marks = %d, want 3", len(m.unread.marks))
	}

	m.updateBoard(keyMsg("A"))
	if len(m.unread.marks) != 0 {
		t.Errorf("marks after A = %v", m.unread.marks)
	}
	if !strings.Contains(m.status, "3 cards") {
		t.Errorf("status = %q, want the count", m.status)
	}
	if strings.Contains(m.viewBoard(), "●") || strings.Contains(m.viewBoard(), "○") {
		t.Error("badges still drawn after marking all read")
	}

	// It sticks: a fresh session reads the same cursor and stays quiet.
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	m2 := newModel(m.store, f)
	if len(m2.unread.marks) != 0 {
		t.Errorf("marks came back in a new session: %v", m2.unread.marks)
	}
	if m2.status != "" {
		t.Errorf("new session status = %q, want no unread notice", m2.status)
	}

	// And later changes are still noticed — the cursor advanced, not broke.
	if err := m.store.Mutate(func(f *task.File) error {
		_, err := store.Add(f, "A", &task.Task{Title: "after the sweep"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m2.updateBoard(keyMsg("r"))
	if len(m2.unread.marks) != 1 {
		t.Errorf("marks after a later change = %v, want 1", m2.unread.marks)
	}
}

func TestMarkAllReadWithNothingUnread(t *testing.T) {
	m := newTestModel(t, 1, 2)
	m.updateBoard(keyMsg("A"))
	if m.status != "nothing unread" {
		t.Errorf("status = %q", m.status)
	}
	if m.isError {
		t.Error("a no-op should not be an error")
	}
}

// typeInto feeds a string to whatever field currently has focus.
func typeInto(m *model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestSearchFilter(t *testing.T) {
	m := newTestModel(t, 2, 0)
	seed := []struct {
		board int
		t     *task.Task
	}{
		{0, &task.Task{ID: "aaaa", Title: "Fix the parser bug", Tags: []string{"parser"}}},
		{0, &task.Task{ID: "bbbb", Title: "Write release notes", Description: "mention the PARSER work"}},
		{0, &task.Task{ID: "cccc", Title: "Refactor the store"}},
		{1, &task.Task{ID: "dddd", Title: "Benchmarks",
			Comments: []task.Comment{{Author: "ai", Text: "parser is the hot path"}}}},
	}
	for _, s := range seed {
		m.file.Boards[s.board].Tasks = append(m.file.Boards[s.board].Tasks, s.t)
	}

	// / opens the prompt; typing filters live across boards and fields.
	m.updateBoard(keyMsg("/"))
	if m.mode != modeSearch {
		t.Fatalf("/ should open the search prompt, mode=%d", m.mode)
	}
	typeInto(m, "parser")
	got := map[string]bool{}
	for bi := range m.file.Boards {
		for _, tk := range m.visibleTasks(bi) {
			got[tk.ID] = true
		}
	}
	// title, description, tag and comment all count as matches; nothing else does.
	for _, id := range []string{"aaaa", "bbbb", "dddd"} {
		if !got[id] {
			t.Errorf("%s should match", id)
		}
	}
	if got["cccc"] {
		t.Error("cccc should not match")
	}
	if shown, total := m.matchCount(); shown != 3 || total != 4 {
		t.Errorf("counts = %d/%d, want 3/4", shown, total)
	}

	// enter keeps the query and returns to the board.
	m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeBoard || m.filter.query != "parser" {
		t.Errorf("after enter: mode=%d query=%q", m.mode, m.filter.query)
	}
	// esc on the board clears it.
	m.updateBoard(keyMsg("esc"))
	if m.filter.active() {
		t.Errorf("esc should clear the filter, got %q", m.filter.describe())
	}
	if len(m.visibleTasks(0)) != 3 {
		t.Errorf("all cards should be back, got %d", len(m.visibleTasks(0)))
	}
}

// Cancelling a search restores the query it started from, rather than
// clearing whatever was already applied.
func TestSearchCancelRestoresPreviousQuery(t *testing.T) {
	m := newTestModel(t, 1, 0)
	m.file.Boards[0].Tasks = []*task.Task{
		{ID: "aaaa", Title: "alpha"}, {ID: "bbbb", Title: "beta"},
	}
	m.updateBoard(keyMsg("/"))
	typeInto(m, "alpha")
	m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})

	m.updateBoard(keyMsg("/")) // start a second search…
	typeInto(m, "xyz")
	if len(m.visibleTasks(0)) != 0 {
		t.Error("live filter should apply while typing")
	}
	m.updateSearch(tea.KeyMsg{Type: tea.KeyEsc}) // …and abandon it
	if m.filter.query != "alpha" {
		t.Errorf("query = %q, want the previous alpha", m.filter.query)
	}
}

func TestUnreadFilter(t *testing.T) {
	m := newTestModel(t, 1, 3)
	id := m.file.Boards[0].Tasks[1].ID
	if err := m.store.Mutate(func(f *task.File) error {
		_, err := store.AddComment(f, id, "agent", "ping")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m.updateBoard(keyMsg("r"))

	m.updateBoard(keyMsg("u"))
	vis := m.visibleTasks(0)
	if len(vis) != 1 || vis[0].ID != id {
		t.Fatalf("unread filter shows %d cards, want just the changed one", len(vis))
	}
	if !strings.Contains(m.status, "1 of 3") {
		t.Errorf("status = %q", m.status)
	}
	// The selection lands on a card that's actually shown.
	if got := m.selectedTask(); got == nil || got.ID != id {
		t.Errorf("selection = %v, want the visible card", got)
	}
	// Reading it empties the view — that's the point of an unread filter.
	m.updateBoard(keyMsg("enter"))
	m.updateDetail(keyMsg("esc"))
	if len(m.visibleTasks(0)) != 0 {
		t.Errorf("card should leave the unread filter once read")
	}
	if m.selectedTask() != nil {
		t.Error("selection should be empty when nothing is visible")
	}
	if !strings.Contains(m.viewBoard(), "no cards match") {
		t.Error("empty filtered board should say so")
	}
	// Toggling off brings everything back.
	m.updateBoard(keyMsg("u"))
	if len(m.visibleTasks(0)) != 3 {
		t.Errorf("visible = %d, want 3", len(m.visibleTasks(0)))
	}
}

// Both filters at once means both have to pass.
func TestFiltersCompose(t *testing.T) {
	m := newTestModel(t, 1, 0)
	m.file.Boards[0].Tasks = []*task.Task{
		{ID: "aaaa", Title: "parser work"},
		{ID: "bbbb", Title: "parser docs"},
		{ID: "cccc", Title: "unrelated"},
	}
	m.unread.marks = map[string]markKind{"bbbb": markUpdated, "cccc": markNew}
	m.filter = filter{unreadOnly: true, query: "parser"}
	vis := m.visibleTasks(0)
	if len(vis) != 1 || vis[0].ID != "bbbb" {
		t.Errorf("composed filter = %v, want only bbbb", vis)
	}
	if d := m.filter.describe(); !strings.Contains(d, "unread") || !strings.Contains(d, "/parser") {
		t.Errorf("describe = %q", d)
	}
}

// Reordering is relative to the whole board, so it refuses while filtered
// rather than moving a card past cards it can't see.
func TestReorderRefusedWhileFiltering(t *testing.T) {
	m := newTestModel(t, 1, 3)
	before := m.file.Boards[0].Tasks[0].ID
	m.filter = filter{query: "task"} // matches every seeded title
	m.updateBoard(keyMsg("J"))
	if !m.isError || !strings.Contains(m.status, "filter") {
		t.Errorf("expected a refusal, status=%q isErr=%v", m.status, m.isError)
	}
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.Boards[0].Tasks[0].ID != before {
		t.Error("order changed despite the refusal")
	}
}

// Mutating a filtered board still works, because those act by id.
func TestMutationsWorkWhileFiltered(t *testing.T) {
	m := newTestModel(t, 2, 2)
	id := m.file.Boards[0].Tasks[1].ID
	m.filter = filter{query: "task"}
	m.selectByID(id)
	m.updateBoard(keyMsg("L")) // move to the next board
	f, err := m.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tk := range f.Boards[1].Tasks {
		if tk.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("move should work under a filter")
	}
	if got := m.selectedTask(); got == nil || got.ID != id {
		t.Errorf("selection should follow the moved card, got %v", got)
	}
}

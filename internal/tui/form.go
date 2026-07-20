package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/walm/todomd/internal/task"
)

type formKind int

const (
	formAdd formKind = iota
	formEdit
	formComment
)

// form is a modal overlay for add/edit/comment. For formComment the
// title input holds the author and the textarea the comment text.
type form struct {
	kind     formKind
	targetID string
	board    string

	title textinput.Model // or author
	tags  textinput.Model
	due   textinput.Model
	desc  textarea.Model // or comment text

	focus         int
	width, height int
	err           string // validation error, shown inside the box

	hover                         int // -1 none, 0 save, 1 cancel
	boxRect, saveRect, cancelRect rect
}

func newInput(placeholder, value string, w int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.SetValue(value)
	in.Width = w
	in.Prompt = ""
	return in
}

func newTaskForm(width, height int, t *task.Task, board string) *form {
	w := formInnerWidth(width)
	f := &form{kind: formAdd, board: board, width: width, height: height, hover: -1}
	var title, tags, due, desc string
	if t != nil {
		f.kind = formEdit
		f.targetID = t.ID
		title = t.Title
		tags = strings.Join(t.Tags, " ")
		if t.Due != nil {
			due = t.Due.String()
		}
		desc = t.Description
	}
	f.title = newInput("task title", title, w)
	f.tags = newInput("tags: parser core", tags, w)
	f.due = newInput("due: YYYY-MM-DD", due, w)
	f.desc = textarea.New()
	f.desc.Placeholder = "description (markdown)"
	f.desc.SetValue(desc)
	f.desc.SetWidth(w)
	f.desc.SetHeight(min(8, max(3, height-14)))
	f.title.Focus()
	return f
}

func newCommentForm(width, height int, targetID, author string) *form {
	w := formInnerWidth(width)
	f := &form{kind: formComment, targetID: targetID, width: width, height: height, hover: -1}
	f.title = newInput("author", author, w)
	f.desc = textarea.New()
	f.desc.Placeholder = "comment text"
	f.desc.SetWidth(w)
	f.desc.SetHeight(min(6, max(3, height-10)))
	f.desc.Focus()
	f.focus = 1
	return f
}

func formInnerWidth(width int) int {
	return max(20, min(70, width-10))
}

func (f *form) fieldCount() int {
	if f.kind == formComment {
		return 2
	}
	return 4
}

func (f *form) setFocus(i int) {
	f.focus = i
	f.title.Blur()
	f.tags.Blur()
	f.due.Blur()
	f.desc.Blur()
	if f.kind == formComment {
		switch i {
		case 0:
			f.title.Focus()
		default:
			f.desc.Focus()
		}
		return
	}
	switch i {
	case 0:
		f.title.Focus()
	case 1:
		f.tags.Focus()
	case 2:
		f.due.Focus()
	default:
		f.desc.Focus()
	}
}

// update handles a key. done=true means submit, canceled=true means close.
func (f *form) update(msg tea.KeyMsg) (done, canceled bool, cmd tea.Cmd) {
	f.err = ""
	last := f.fieldCount() - 1
	switch msg.String() {
	case "esc":
		return false, true, nil
	case "ctrl+s":
		return true, false, nil
	case "tab", "shift+tab":
		d := 1
		if msg.String() == "shift+tab" {
			d = f.fieldCount() - 1
		}
		f.setFocus((f.focus + d) % f.fieldCount())
		return false, false, nil
	case "enter":
		// Enter advances on single-line fields; in the textarea it types.
		if f.focus < last {
			f.setFocus(f.focus + 1)
			return false, false, nil
		}
	}
	if f.focus == last {
		f.desc, cmd = f.desc.Update(msg)
		return false, false, cmd
	}
	if f.kind == formComment {
		f.title, cmd = f.title.Update(msg)
		return false, false, cmd
	}
	switch f.focus {
	case 0:
		f.title, cmd = f.title.Update(msg)
	case 1:
		f.tags, cmd = f.tags.Update(msg)
	case 2:
		f.due, cmd = f.due.Update(msg)
	}
	return false, false, cmd
}

type taskValues struct {
	title string
	tags  []string
	due   *task.Date
	desc  string
}

// taskValues validates and collects the form fields for add/edit.
func (f *form) taskValues() (taskValues, error) {
	var v taskValues
	var err error
	if v.title, err = task.ValidateTitle(f.title.Value()); err != nil {
		return v, err
	}
	for _, raw := range strings.FieldsFunc(f.tags.Value(), func(r rune) bool {
		return r == ' ' || r == ','
	}) {
		tag, err := task.NormalizeTag(raw)
		if err != nil {
			return v, err
		}
		v.tags = append(v.tags, tag)
	}
	if s := strings.TrimSpace(f.due.Value()); s != "" {
		d, err := task.ParseDate(s)
		if err != nil {
			return v, err
		}
		v.due = &d
	}
	v.desc = f.desc.Value()
	return v, nil
}

// formHint is the footer of a form; save/cancel dispatch like the detail
// footer, "tab next field" is keyboard-only.
const (
	formHintPre    = "tab next field · "
	formHintSave   = "ctrl+s save"
	formHintCancel = "esc cancel"
)

// render returns the form box plus the save/cancel action rectangles
// relative to the box's top-left corner.
func (f *form) render() (box string, saveRel, cancelRel rect) {
	var lines []string
	add := func(s string) { lines = append(lines, strings.Split(s, "\n")...) }

	heading := map[formKind]string{
		formAdd:     "Add task — " + f.board,
		formEdit:    "Edit task",
		formComment: "Comment",
	}[f.kind]
	add(formTitle.Render(heading))
	add("")
	if f.kind == formComment {
		add(formLabel.Render("author"))
		add(f.title.View())
		add("")
		add(formLabel.Render("text"))
		add(f.desc.View())
	} else {
		add(formLabel.Render("title"))
		add(f.title.View())
		add("")
		add(formLabel.Render("tags"))
		add(f.tags.View())
		add("")
		add(formLabel.Render("due"))
		add(f.due.View())
		add("")
		add(formLabel.Render("description"))
		add(f.desc.View())
	}
	if f.err != "" {
		add("")
		add(errorStyle.Render(f.err))
	}
	add("")
	hintLine := len(lines)
	save, cancel := hintStyle, hintStyle
	switch f.hover {
	case 0:
		save = hintHoverStyle
	case 1:
		cancel = hintHoverStyle
	}
	add(hintStyle.Render(formHintPre) + save.Render(formHintSave) +
		hintStyle.Render(" · ") + cancel.Render(formHintCancel))

	box = formBox.Render(strings.Join(lines, "\n"))
	// Content origin within the box: border (1,1) + padding (2,1).
	plain := formHintPre + formHintSave + " · " + formHintCancel
	saveRel = rect{3 + strings.Index(plain, formHintSave), 2 + hintLine, len(formHintSave), 1}
	cancelRel = rect{3 + strings.Index(plain, formHintCancel), 2 + hintLine, len(formHintCancel), 1}
	return box, saveRel, cancelRel
}

// viewForm composes the form box over the board and records the absolute
// button rectangles for mouse hit-testing.
func (m *model) viewForm() string {
	box, saveRel, cancelRel := m.form.render()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	bx, by := max(0, (m.width-w)/2), max(0, (m.height-h)/2)
	m.form.boxRect = rect{bx, by, w, h}
	m.form.saveRect = rect{bx + saveRel.x, by + saveRel.y, saveRel.w, saveRel.h}
	m.form.cancelRect = rect{bx + cancelRel.x, by + cancelRel.y, cancelRel.w, cancelRel.h}
	return compose(m.viewBoard(), box, m.width, m.height)
}

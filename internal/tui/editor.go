package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

type editorFinishedMsg struct {
	err  error
	path string
	id   string
	from mode
}

func editorCommand() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// openEditor suspends the TUI and edits the selected task as a markdown
// fragment in $VISUAL/$EDITOR.
func (m *model) openEditor(from mode) tea.Cmd {
	t := m.selectedTask()
	if t == nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "todomd-*.md")
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	if _, err := tmp.Write(markdown.WriteTask(t)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		m.setStatus(err.Error(), true)
		return nil
	}
	tmp.Close()
	id := t.ID
	// No shell: split "$EDITOR" into argv (supports "code -w" style values)
	// and pass the path as a plain argument.
	argv := strings.Fields(editorCommand())
	if len(argv) == 0 {
		argv = []string{"vi"}
	}
	c := exec.Command(argv[0], append(argv[1:], tmp.Name())...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, path: tmp.Name(), id: id, from: from}
	})
}

// applyEditor parses the edited fragment and applies it to the task by ID.
func (m *model) applyEditor(msg editorFinishedMsg) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.setStatus("editor: "+msg.err.Error(), true)
		return
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	edited, err := markdown.ParseTask(data)
	if err != nil {
		m.setStatus("edit discarded — "+err.Error(), true)
		return
	}
	if markdown.UnclosedFence(edited.Description) {
		m.setStatus("edit discarded — description contains an unclosed code fence", true)
		return
	}
	m.mutate(func(f *task.File) error {
		b, i, err := store.FindTask(f, msg.id)
		if err != nil {
			return err
		}
		t := b.Tasks[i]
		t.Title = edited.Title
		t.Tags = edited.Tags
		t.Due = edited.Due
		t.Description = edited.Description
		t.Comments = edited.Comments
		return nil
	}, msg.id, "saved from editor")
	if msg.from == modeDetail && !m.isError {
		m.openDetail()
		m.mode = modeDetail
	}
}

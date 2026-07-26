package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/walm/todomd/internal/fixture"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

func benchModel(b *testing.B, n int) *model {
	b.Helper()
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	path := filepath.Join(b.TempDir(), "TODO.md")
	if err := os.WriteFile(path, fixture.Generate(n), 0o644); err != nil {
		b.Fatal(err)
	}
	s := &store.Store{Path: path}
	f, err := s.Load()
	if err != nil {
		b.Fatal(err)
	}
	m := newModel(s, f)
	m.width, m.height = 120, 40
	m.glamourStyle = "notty"
	return m
}

// BenchmarkViewBoard is the per-keystroke cost: the board is re-rendered on
// every key and every mouse motion event.
func BenchmarkViewBoard(b *testing.B) {
	for _, n := range []int{100, 1_000, 5_000, 25_000} {
		m := benchModel(b, n)
		b.Run(fmt.Sprintf("%dtasks", n), func(b *testing.B) {
			for b.Loop() {
				m.viewBoard()
			}
		})
	}
}

// BenchmarkStartup is what the user waits for when opening the TUI: load,
// unread diff, first render.
func BenchmarkStartup(b *testing.B) {
	for _, n := range []int{1_000, 5_000, 25_000} {
		b.Run(fmt.Sprintf("%dtasks", n), func(b *testing.B) {
			for b.Loop() {
				m := benchModel(b, n)
				m.viewBoard()
			}
		})
	}
}

// BenchmarkRenderCard shows why the board render scales with total tasks:
// renderColumn styles every card in a visible column to measure its height,
// not just the handful on screen.
func BenchmarkRenderCard(b *testing.B) {
	tk := &fixtureTask
	for b.Loop() {
		renderCard(tk, 30, false, markNone)
	}
}

var fixtureTask = func() task.Task {
	return task.Task{ID: "aaaa", Title: "Task 42: make the widget do the thing",
		Tags: []string{"area", "chore"}, Priority: task.PriorityHigh}
}()

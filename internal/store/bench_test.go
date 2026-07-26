package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/walm/todomd/internal/fixture"
	"github.com/walm/todomd/internal/task"
)

func benchStore(b *testing.B, n int) (*Store, []byte) {
	b.Helper()
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	data := fixture.Generate(n)
	path := filepath.Join(b.TempDir(), "TODO.md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	return &Store{Path: path}, data
}

var sizes = []int{1_000, 5_000, 25_000}

func BenchmarkLoad(b *testing.B) {
	for _, n := range sizes {
		s, data := benchStore(b, n)
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := s.Load(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMutate is what `todomd move` costs without change tracking:
// lock, load, apply, atomic write.
func BenchmarkMutate(b *testing.B) {
	for _, n := range sizes {
		s, data := benchStore(b, n)
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				err := s.Mutate(func(f *task.File) error {
					f.Boards[0].Tasks[0].Title = "touched"
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMutateTracked is the same plus the pre-image copy every CLI
// mutation takes so it can diff what it wrote.
func BenchmarkMutateTracked(b *testing.B) {
	for _, n := range sizes {
		s, data := benchStore(b, n)
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				_, _, err := s.MutateTracked(func(f *task.File) error {
					f.Boards[0].Tasks[0].Title = "touched"
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

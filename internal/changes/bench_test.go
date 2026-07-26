package changes

import (
	"fmt"
	"testing"

	"github.com/walm/todomd/internal/fixture"
	"github.com/walm/todomd/internal/markdown"
)

func BenchmarkDiff(b *testing.B) {
	for _, n := range []int{1_000, 5_000, 25_000} {
		data := fixture.Generate(n)
		old, err := markdown.Parse(data)
		if err != nil {
			b.Fatal(err)
		}
		cur, err := markdown.Parse(data)
		if err != nil {
			b.Fatal(err)
		}
		cur.Boards[0].Tasks[0].Title = "changed"
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if evs := Diff(old, cur); len(evs) != 1 {
					b.Fatalf("events = %d", len(evs))
				}
			}
		})
	}
}

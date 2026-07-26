package markdown

import (
	"fmt"
	"testing"

	"github.com/walm/todomd/internal/fixture"
)

var benchSizes = []int{100, 1_000, 5_000, 25_000}

func BenchmarkParse(b *testing.B) {
	for _, n := range benchSizes {
		data := fixture.Generate(n)
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Parse(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWrite(b *testing.B) {
	for _, n := range benchSizes {
		data := fixture.Generate(n)
		f, err := Parse(data)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				Write(f)
			}
		})
	}
}

// BenchmarkRoundTrip is the shape of every mutation: parse, touch one task,
// write it all back.
func BenchmarkRoundTrip(b *testing.B) {
	for _, n := range benchSizes {
		data := fixture.Generate(n)
		b.Run(fmt.Sprintf("%dtasks_%dKB", n, len(data)/1024), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				f, err := Parse(data)
				if err != nil {
					b.Fatal(err)
				}
				f.Boards[0].Tasks[0].Title = "touched"
				Write(f)
			}
		})
	}
}

func BenchmarkAssignIDs(b *testing.B) {
	data := fixture.Generate(25_000)
	f, err := Parse(data)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(f.AllTasks())), "tasks")
	for b.Loop() {
		f.AssignIDs()
	}
}

// Package fixture generates synthetic TODO.md content for benchmarks and
// tests. It is only imported from _test files, so it never reaches the binary.
package fixture

import (
	"fmt"
	"strings"
)

// Generate builds a TODO.md with n tasks spread over the default boards. Each
// task carries a realistic description, and every other one a comment, which
// works out around 200 bytes per task.
func Generate(n int) []byte {
	var b strings.Builder
	b.WriteString("# Benchmark\n")
	boards := []string{"Backlog", "In Progress", "Done"}
	per := n / len(boards)
	for bi, name := range boards {
		fmt.Fprintf(&b, "\n## %s\n", name)
		for i := 0; i < per; i++ {
			fmt.Fprintf(&b, "\n### Task %d: make the widget do the thing\n", bi*per+i)
			fmt.Fprintf(&b, "<!-- id:%04x -->\n", bi*per+i)
			if i%3 == 0 {
				fmt.Fprintf(&b, "`#area` `#chore` **priority:** high **due:** 2026-08-0%d\n", i%9+1)
			} else {
				b.WriteString("`#area`\n")
			}
			b.WriteString("\nSome description text that spans a line or two, roughly what a\nreal task carries when someone bothered to explain it.\n")
			if i%2 == 0 {
				b.WriteString("\n#### Comments\n\n- **ai** (2026-07-20): Looked into this, needs a follow-up.\n")
			}
		}
	}
	return []byte(b.String())
}

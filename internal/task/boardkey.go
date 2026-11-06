package task

import "unicode"

// BoardKey is the one case-insensitive key for a board name: two names have
// equal keys exactly when strings.EqualFold reports them equal. Each rune maps
// to the smallest rune of its unicode.SimpleFold orbit, which is also how
// todomd.online's TypeScript boardKey is built, so both sides group board
// names identically.
func BoardKey(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		out = append(out, foldMin(r))
	}
	return string(out)
}

// SameBoard reports whether two board names name the same board.
func SameBoard(a, b string) bool { return BoardKey(a) == BoardKey(b) }

func foldMin(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < m {
			m = f
		}
	}
	return m
}

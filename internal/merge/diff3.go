package merge

import (
	"slices"
	"strings"
)

// Line-based three-way merge for the preamble and descriptions (spec §8).
//
// A line-for-line port of todomd.online's diff3.ts: the two-way diff is
// Myers' O(ND) algorithm with the linear-space middle-snake bisection (as in
// diff-match-patch's diff_bisect) after trimming the common prefix and suffix,
// and the three-way step is the classic diff3 walk over stable and unstable
// chunks. The result has to be identical on both sides of a sync.

// MaxMergeLines: above this many lines on any side a field isn't merged line
// by line; it counts as changed on both sides.
const MaxMergeLines = 20000

// MaxEditLines: nor when more lines than this were inserted plus deleted
// between base and a side, which bounds Myers' O((N+M)·D) cost.
const MaxEditLines = 2000

const unlimited = -1

// match returns a longest common subsequence of a and b — for each index of
// a, the index in b it is matched to, or -1 — or nil when the edit distance
// (lines deleted from a plus lines inserted from b) exceeds maxEdits
// (unlimited for no bound).
func match(a, b []int, maxEdits int) []int {
	m := make([]int, len(a))
	for i := range m {
		m[i] = -1
	}
	// The middle snake of an edit script of length D is found by step
	// ceil(D/2) of the search, so a script within maxEdits is never cut off.
	dLimit := unlimited
	if maxEdits != unlimited {
		dLimit = (maxEdits+1)/2 + 1
	}
	if !diffRange(a, 0, len(a), b, 0, len(b), m, dLimit) {
		return nil
	}
	common := 0
	for _, j := range m {
		if j >= 0 {
			common++
		}
	}
	if maxEdits != unlimited && len(a)+len(b)-2*common > maxEdits {
		return nil
	}
	return m
}

// diffRange fills m for a[aLo:aHi] vs b[bLo:bHi]. It returns false if the
// top-level search ran past dLimit steps.
func diffRange(a []int, aLo, aHi int, b []int, bLo, bHi int, m []int, dLimit int) bool {
	for aLo < aHi && bLo < bHi && a[aLo] == b[bLo] {
		m[aLo] = bLo
		aLo++
		bLo++
	}
	for aLo < aHi && bLo < bHi && a[aHi-1] == b[bHi-1] {
		aHi--
		bHi--
		m[aHi] = bHi
	}
	if aLo == aHi || bLo == bHi {
		return true
	}
	x, y, res := bisect(a, aLo, aHi, b, bLo, bHi, dLimit)
	switch res {
	case bisectLimit:
		return false
	case bisectNone:
		return true // nothing in common
	}
	// The halves' edit scripts are shorter than the whole: no limit there.
	diffRange(a, aLo, aLo+x, b, bLo, bLo+y, m, unlimited)
	diffRange(a, aLo+x, aHi, b, bLo+y, bHi, m, unlimited)
	return true
}

const (
	bisectFound = iota
	bisectNone
	bisectLimit
)

// bisect finds the middle snake of a[aLo:aHi] vs b[bLo:bHi] and returns the
// split point relative to (aLo, bLo).
func bisect(a []int, aLo, aHi int, b []int, bLo, bHi int, dLimit int) (int, int, int) {
	n := aHi - aLo
	mm := bHi - bLo
	maxD := (n + mm + 1) / 2
	off := maxD
	size := 2 * maxD
	if off+1 >= size {
		// One line against one other line (the common prefix and suffix are
		// trimmed already). The reference reads past the end of its typed
		// arrays here, which yields undefined and finds no snake; the answer
		// is the same: nothing in common.
		return 0, 0, bisectNone
	}
	v1 := make([]int, size)
	v2 := make([]int, size)
	for i := range v1 {
		v1[i], v2[i] = -1, -1
	}
	v1[off+1] = 0
	v2[off+1] = 0
	delta := n - mm
	front := delta%2 != 0
	k1start, k1end, k2start, k2end := 0, 0, 0, 0
	for d := 0; d < maxD; d++ {
		if dLimit != unlimited && d >= dLimit {
			return 0, 0, bisectLimit
		}
		for k1 := -d + k1start; k1 <= d-k1end; k1 += 2 {
			k1o := off + k1
			var x1 int
			if k1 == -d || (k1 != d && v1[k1o-1] < v1[k1o+1]) {
				x1 = v1[k1o+1]
			} else {
				x1 = v1[k1o-1] + 1
			}
			y1 := x1 - k1
			for x1 < n && y1 < mm && a[aLo+x1] == b[bLo+y1] {
				x1++
				y1++
			}
			v1[k1o] = x1
			if x1 > n {
				k1end += 2
			} else if y1 > mm {
				k1start += 2
			} else if front {
				k2o := off + delta - k1
				if k2o >= 0 && k2o < size && v2[k2o] != -1 && x1 >= n-v2[k2o] {
					return x1, y1, bisectFound
				}
			}
		}
		for k2 := -d + k2start; k2 <= d-k2end; k2 += 2 {
			k2o := off + k2
			var x2 int
			if k2 == -d || (k2 != d && v2[k2o-1] < v2[k2o+1]) {
				x2 = v2[k2o+1]
			} else {
				x2 = v2[k2o-1] + 1
			}
			y2 := x2 - k2
			for x2 < n && y2 < mm && a[aHi-x2-1] == b[bHi-y2-1] {
				x2++
				y2++
			}
			v2[k2o] = x2
			if x2 > n {
				k2end += 2
			} else if y2 > mm {
				k2start += 2
			} else if !front {
				k1o := off + delta - k2
				if k1o >= 0 && k1o < size && v1[k1o] != -1 {
					x1 := v1[k1o]
					y1 := off + x1 - k1o
					if x1 >= n-x2 {
						return x1, y1, bisectFound
					}
				}
			}
		}
	}
	return 0, 0, bisectNone
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// interner maps lines (or any string keys) to small integers, so the diff
// compares numbers.
type interner map[string]int

func (in interner) ids(xs []string) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		id, ok := in[x]
		if !ok {
			id = len(in)
			in[x] = id
		}
		out[i] = id
	}
	return out
}

// Merge3 merges ours' and theirs' edits of base line by line. Non-overlapping
// edits combine and identical edits count once; ok is false for an
// overlapping edit, a side over MaxMergeLines, or a side more than
// MaxEditLines away from base (the caller then takes theirs whole).
//
// A clean result is only accepted if merging ours into it again changes
// nothing: with repeated lines plain diff3 can align a re-push differently
// and apply ours' insertion twice, which would break the idempotent re-push
// that spec §8 requires.
func Merge3(base, ours, theirs string) (string, bool) {
	if ours == theirs || base == theirs {
		return ours, true
	}
	if base == ours {
		return theirs, true
	}
	first, ok := diff3(base, ours, theirs)
	if !ok {
		return "", false
	}
	if first == theirs || first == ours {
		return first, true
	}
	if again, ok := diff3(base, ours, first); ok && again == first {
		return first, true
	}
	return "", false
}

// diff3 is plain diff3: the merged text, or false on an overlapping edit.
func diff3(base, ours, theirs string) (string, bool) {
	if ours == theirs || base == theirs {
		return ours, true
	}
	if base == ours {
		return theirs, true
	}
	B, O, T := splitLines(base), splitLines(ours), splitLines(theirs)
	if max(len(B), len(O), len(T)) > MaxMergeLines {
		return "", false
	}
	in := interner{}
	b := in.ids(B)
	mO := match(b, in.ids(O), MaxEditLines)
	mT := match(b, in.ids(T), MaxEditLines)
	if mO == nil || mT == nil {
		return "", false
	}

	var out []string
	ib, io, it := 0, 0, 0
	for {
		// A stable chunk: base lines matched on both sides, in step.
		s := 0
		for ib+s < len(B) && mO[ib+s] == io+s && mT[ib+s] == it+s {
			s++
		}
		if s > 0 {
			out = append(out, B[ib:ib+s]...)
			ib += s
			io += s
			it += s
			continue
		}
		// An unstable chunk runs to the next base line matched on both sides.
		j := ib
		for j < len(B) && (mO[j] < 0 || mT[j] < 0) {
			j++
		}
		oEnd, tEnd := len(O), len(T)
		if j < len(B) {
			oEnd, tEnd = mO[j], mT[j]
		}
		bc, oc, tc := B[ib:j], O[io:oEnd], T[it:tEnd]
		switch {
		case slices.Equal(oc, bc):
			out = append(out, tc...)
		case slices.Equal(tc, bc) || slices.Equal(oc, tc):
			out = append(out, oc...)
		default:
			return "", false
		}
		ib, io, it = j, oEnd, tEnd
		if j == len(B) {
			break
		}
	}
	return strings.Join(out, "\n"), true
}

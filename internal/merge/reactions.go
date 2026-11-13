package merge

import "github.com/walm/todomd/internal/task"

// reactionPair is one person's reaction, the unit reactions merge in. Emoji
// is task.EmojiKey's form, so two spellings of one emoji are one pair.
type reactionPair struct{ emoji, name string }

func pairs(rs []task.Reaction) []reactionPair {
	var out []reactionPair
	for _, r := range rs {
		for _, n := range r.By {
			out = append(out, reactionPair{task.EmojiKey(r.Emoji), n})
		}
	}
	return out
}

// occurrences indexes comments by commentKey and occurrence, so the n-th
// copy of a repeated comment on one side meets the n-th copy on the other.
func occurrences(cs []task.Comment) map[string][]task.Comment {
	m := map[string][]task.Comment{}
	for _, c := range cs {
		k := commentKey(c)
		m[k] = append(m[k], c)
	}
	return m
}

// mergeReactions gives each comment of the merged list its merged
// reactions. A comment is matched across the three sides by commentKey and
// occurrence. Its reactions merge as a set of (emoji, name) pairs with
// mergeSet, the tags rule: what either side added is in, what either side
// removed is out. A side without the comment counts as unchanged from the
// base (or, with no base copy either, as having none). The order is
// mergeSet's, theirs' pairs first, grouped by emoji at its first place, and
// an emoji keeps theirs' spelling, else ours', else the base's, the one
// with more U+FE0F winning (task.BetterSpelling).
func mergeReactions(hasBase bool, b, o, t, list []task.Comment, noDeletes bool) []task.Comment {
	out := task.CloneComments(list)
	if out == nil {
		return nil
	}
	bm, om, tm := occurrences(b), occurrences(o), occurrences(t)
	seen := map[string]int{}
	for i := range out {
		k := commentKey(out[i])
		n := seen[k]
		seen[k]++
		get := func(m map[string][]task.Comment) ([]task.Reaction, bool) {
			if cs := m[k]; n < len(cs) {
				return cs[n].Reactions, true
			}
			return nil, false
		}
		rb, inB := get(bm)
		ro, inO := get(om)
		rt, inT := get(tm)
		if !inO {
			ro = rb
		}
		if !inT {
			rt = rb
		}
		spelling := map[string]string{}
		for _, rs := range [][]task.Reaction{rb, ro, rt} {
			for _, r := range rs {
				ek := task.EmojiKey(r.Emoji)
				spelling[ek] = task.BetterSpelling(r.Emoji, spelling[ek])
			}
		}
		merged := mergeSet(hasBase && inB, pairs(rb), pairs(ro), pairs(rt), noDeletes)
		var rs []task.Reaction
		at := map[string]int{}
		for _, p := range merged {
			j, ok := at[p.emoji]
			if !ok {
				j = len(rs)
				at[p.emoji] = j
				rs = append(rs, task.Reaction{Emoji: spelling[p.emoji]})
			}
			rs[j].By = append(rs[j].By, p.name)
		}
		out[i].Reactions = rs
	}
	return out
}

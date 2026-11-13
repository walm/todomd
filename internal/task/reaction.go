package task

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// A comment can carry emoji reactions: for each emoji, who reacted with it.
// TODO.md stores them as a hidden last line of the comment:
//
//	- **anna** (2026-11-13): Can you look at the export?
//	  <!-- reactions: 👍 walm,anna; 👀 agent -->
//
// See docs/comment-reactions-format.md.

// Reaction is one emoji on a comment and who reacted with it, in the order
// they reacted. By is never empty in a valid model.
type Reaction struct {
	Emoji string
	By    []string
}

// MaxReactions is how many different emoji a writer lets a comment have.
// Readers and merges accept more, so two people adding the 20th and 21st at
// once never break a file.
const MaxReactions = 20

// MaxEmojiRunes bounds one reaction emoji, in code points. The longest
// standard emoji (family and flag sequences) are well under it.
const MaxEmojiRunes = 16

// QuickReactions is the quick set the web offers, in its order.
var QuickReactions = []string{"👍", "❤️", "🎉", "👀", "✅", "🙏"}

var reactorRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidReactor reports whether s is a reactor name as TODO.md stores it:
// lowercase ASCII letters and digits, then also '.', '_' or '-', at most 64.
func ValidReactor(s string) bool { return reactorRe.MatchString(s) }

// ParseReactor reads a reactor name the way a person types it: surrounding
// space dropped, ASCII lowercased.
func ParseReactor(s string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(s))
	if !ValidReactor(n) {
		return "", fmt.Errorf("invalid name %q for a reaction (want letters, digits, '.', '_' or '-', starting with a letter or digit, at most 64)", s)
	}
	return n, nil
}

// emojiPart reports whether r may appear in a reaction emoji: no ASCII but
// the keycap bases '#', '*' and 0-9, no control or space characters, and
// none of the line's own delimiters.
func emojiPart(r rune) bool {
	switch {
	case r == '#' || r == '*' || (r >= '0' && r <= '9'):
		return true
	case r < 0xA1: // ASCII, C1 controls, NBSP
		return false
	}
	switch r {
	case 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return false
	}
	return !(r >= 0x2000 && r <= 0x200A) && r != utf8.RuneError
}

// joiner reports whether r is a zero-width joiner or a variation selector,
// which an emoji can't be made of alone. (U+20E3, the keycap, does count:
// 1️⃣ is 1, U+FE0F, U+20E3.)
func joiner(r rune) bool {
	return r == 0x200D || r == 0xFE0E || r == 0xFE0F
}

// ValidEmoji reports whether s can be a reaction: 1 to MaxEmojiRunes code
// points that emojiPart allows, at least one of them neither ASCII nor a
// joiner or selector. It doesn't check that s is a real emoji; that would
// need Unicode's tables, and Go and JavaScript must agree.
func ValidEmoji(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	n, base := 0, false
	for _, r := range s {
		n++
		if !emojiPart(r) {
			return false
		}
		if r >= 0xA1 && !joiner(r) {
			base = true
		}
	}
	return n >= 1 && n <= MaxEmojiRunes && base
}

// EmojiKey is what makes two reaction emoji the same: the emoji without
// variation selectors (U+FE0E, U+FE0F), so ❤ and ❤️ are one reaction. Skin
// tones count: 👍 and 👍🏽 are two.
func EmojiKey(s string) string {
	if !strings.ContainsAny(s, "\uFE0E\uFE0F") {
		return s
	}
	return strings.NewReplacer("\uFE0E", "", "\uFE0F", "").Replace(s)
}

// BetterSpelling picks the spelling to keep when two spellings of one emoji
// meet: the one with more U+FE0F (the fully qualified form renders as an
// emoji everywhere), a on a tie.
func BetterSpelling(a, b string) string {
	if strings.Count(b, "\uFE0F") > strings.Count(a, "\uFE0F") {
		return b
	}
	return a
}

// ParseEmoji reads a reaction emoji the way a person types it. One of the
// quick set, with or without its variation selector, gets the quick set's
// spelling; anything else is kept as given.
func ParseEmoji(s string) (string, error) {
	e := strings.TrimSpace(s)
	for _, q := range QuickReactions {
		if EmojiKey(q) == EmojiKey(e) {
			return q, nil
		}
	}
	if !ValidEmoji(e) {
		return "", fmt.Errorf("%q is not an emoji", s)
	}
	return e, nil
}

// NormalizeReactions merges entries for the same emoji (by EmojiKey, the
// better spelling kept, at the first one's place), drops repeated names
// and entries left with nobody. The result shares nothing with rs.
func NormalizeReactions(rs []Reaction) []Reaction {
	var out []Reaction
	at := map[string]int{}
	for _, r := range rs {
		k := EmojiKey(r.Emoji)
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, Reaction{Emoji: r.Emoji})
		} else {
			out[i].Emoji = BetterSpelling(out[i].Emoji, r.Emoji)
		}
		for _, n := range r.By {
			if !slices.Contains(out[i].By, n) {
				out[i].By = append(out[i].By, n)
			}
		}
	}
	return slices.DeleteFunc(out, func(r Reaction) bool { return len(r.By) == 0 })
}

// CloneReactions deep-copies a reaction list; nil stays nil.
func CloneReactions(rs []Reaction) []Reaction {
	if rs == nil {
		return nil
	}
	out := make([]Reaction, len(rs))
	for i, r := range rs {
		out[i] = Reaction{Emoji: r.Emoji, By: slices.Clone(r.By)}
	}
	return out
}

// SameReactions reports whether two lists are equal, order included.
func SameReactions(a, b []Reaction) bool {
	return slices.EqualFunc(a, b, func(x, y Reaction) bool {
		return x.Emoji == y.Emoji && slices.Equal(x.By, y.By)
	})
}

// CloneComments deep-copies comments, their reactions included.
func CloneComments(cs []Comment) []Comment {
	if cs == nil {
		return nil
	}
	out := slices.Clone(cs)
	for i := range out {
		out[i].Reactions = CloneReactions(cs[i].Reactions)
	}
	return out
}

// SameComment reports whether two comments are equal, reactions included.
func SameComment(a, b Comment) bool {
	return a.Author == b.Author && a.Date == b.Date && a.Text == b.Text && SameReactions(a.Reactions, b.Reactions)
}

// SameComments compares two comment lists with SameComment.
func SameComments(a, b []Comment) bool { return slices.EqualFunc(a, b, SameComment) }

// React adds name's reaction emoji to c, or with remove takes it away. It
// reports whether c changed. A new emoji goes last, a new name last in its
// emoji's list; an emoji nobody is left on goes away.
func (c *Comment) React(emoji, name string, remove bool) (bool, error) {
	k := EmojiKey(emoji)
	i := slices.IndexFunc(c.Reactions, func(r Reaction) bool { return EmojiKey(r.Emoji) == k })
	if remove {
		if i < 0 {
			return false, nil
		}
		j := slices.Index(c.Reactions[i].By, name)
		if j < 0 {
			return false, nil
		}
		rs := CloneReactions(c.Reactions)
		rs[i].By = slices.Delete(rs[i].By, j, j+1)
		c.Reactions = NormalizeReactions(rs)
		if len(c.Reactions) == 0 {
			c.Reactions = nil
		}
		return true, nil
	}
	if i < 0 {
		if len(c.Reactions) >= MaxReactions {
			return false, fmt.Errorf("a comment can have at most %d different reactions", MaxReactions)
		}
		c.Reactions = append(CloneReactions(c.Reactions), Reaction{Emoji: emoji, By: []string{name}})
		return true, nil
	}
	if slices.Contains(c.Reactions[i].By, name) {
		return false, nil
	}
	rs := CloneReactions(c.Reactions)
	rs[i].By = append(rs[i].By, name)
	c.Reactions = rs
	return true, nil
}

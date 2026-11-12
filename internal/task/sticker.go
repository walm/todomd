package task

import (
	"fmt"
	"strings"
)

// A sticker is one mark a card can carry to stand out on the board: an
// emoji from a fixed set, or a coloured corner ribbon. It is stored by its
// ASCII name (TODO.md writes `<!-- sticker: fire -->`); "" is no sticker.
// See docs/card-stickers-format.md.

// Sticker is one entry of the fixed set.
type Sticker struct {
	Name  string // as TODO.md and the sync wire format write it
	Emoji string // "" for a ribbon
}

// Stickers lists every sticker in picker order: the twelve emoji, then the
// five ribbon colours.
var Stickers = []Sticker{
	{"fire", "🔥"}, {"star", "⭐"}, {"eyes", "👀"}, {"wip", "🚧"},
	{"check", "✅"}, {"target", "🎯"}, {"zap", "⚡"}, {"idea", "💡"},
	{"bug", "🐛"}, {"ship", "🚀"}, {"question", "❓"}, {"heart", "❤️"},
	{"red", ""}, {"amber", ""}, {"green", ""}, {"blue", ""}, {"violet", ""},
}

var stickerByName = func() map[string]Sticker {
	m := map[string]Sticker{}
	for _, s := range Stickers {
		m[s.Name] = s
	}
	return m
}()

// ValidSticker reports whether name is a sticker in its canonical spelling.
// "" (no sticker) is not one.
func ValidSticker(name string) bool { _, ok := stickerByName[name]; return ok }

// StickerEmoji is the emoji of an emoji sticker, "" for a ribbon or an
// unknown name.
func StickerEmoji(name string) string { return stickerByName[name].Emoji }

// IsRibbon reports whether name is one of the ribbon colours.
func IsRibbon(name string) bool {
	s, ok := stickerByName[name]
	return ok && s.Emoji == ""
}

// StickerLabel is how a sticker reads in plain text: "🔥 fire" or
// "red ribbon"; "" for none.
func StickerLabel(name string) string {
	switch s, ok := stickerByName[name]; {
	case !ok:
		return ""
	case s.Emoji == "":
		return s.Name + " ribbon"
	default:
		return s.Emoji + " " + s.Name
	}
}

// stripVariation drops emoji presentation selectors, so ❤ and ❤️ (or ⭐ and
// ⭐️) are the same sticker.
func stripVariation(s string) string {
	return strings.NewReplacer("️", "", "︎", "").Replace(s)
}

// ParseSticker reads a sticker the way a person types it: its name in any
// case ("fire", "Red"), its emoji with or without a variation selector
// ("🔥", "❤", "❤️"), or "none" for no sticker (""). It returns the canonical
// name.
func ParseSticker(s string) (string, error) {
	in := strings.TrimSpace(s)
	low := strings.ToLower(in)
	if low == "none" {
		return "", nil
	}
	if ValidSticker(low) {
		return low, nil
	}
	if e := stripVariation(in); e != "" {
		for _, st := range Stickers {
			if st.Emoji != "" && stripVariation(st.Emoji) == e {
				return st.Name, nil
			}
		}
	}
	return "", fmt.Errorf("unknown sticker %q (want one of %s, or none)", s, StickerChoices())
}

// StickerChoices lists the names for messages: "fire 🔥, star ⭐, …, red,
// amber, …".
func StickerChoices() string {
	parts := make([]string, len(Stickers))
	for i, s := range Stickers {
		parts[i] = strings.TrimSpace(s.Name + " " + s.Emoji)
	}
	return strings.Join(parts, ", ")
}

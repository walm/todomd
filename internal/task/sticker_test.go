package task

import "testing"

func TestParseSticker(t *testing.T) {
	for in, want := range map[string]string{
		"fire": "fire", "FIRE": "fire", " Red ": "red", "🔥": "fire", "❤️": "heart", "❤": "heart",
		"⭐": "star", "⭐️": "star", "⚡": "zap", "✅": "check", "❓": "question", "none": "", "None": "",
		"violet": "violet", "🚧": "wip", "🐛": "bug",
	} {
		if got, err := ParseSticker(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "banana", "🍌", "fire star", "️", "ribbon", "red ribbon", "🔥🔥"} {
		if got, err := ParseSticker(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
	if len(Stickers) != 17 {
		t.Fatalf("%d stickers", len(Stickers))
	}
	emoji := 0
	for _, s := range Stickers {
		if !ValidSticker(s.Name) || (s.Emoji == "") != IsRibbon(s.Name) || StickerLabel(s.Name) == "" {
			t.Errorf("%+v", s)
		}
		if s.Emoji != "" {
			emoji++
			if got, _ := ParseSticker(s.Emoji); got != s.Name {
				t.Errorf("%s: emoji reads as %q", s.Name, got)
			}
		}
	}
	if emoji != 12 || ValidSticker("") || ValidSticker("Fire") || StickerLabel("") != "" ||
		StickerLabel("red") != "red ribbon" || StickerLabel("fire") != "🔥 fire" {
		t.Error("set")
	}
}

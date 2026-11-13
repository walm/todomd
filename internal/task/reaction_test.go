package task

import (
	"strings"
	"testing"
)

func TestValidEmoji(t *testing.T) {
	for _, s := range []string{"👍", "❤️", "❤", "☀", "1️⃣", "#️⃣", "*️⃣", "🇸🇪", "👨‍👩‍👧", "👍🏽", "✅", "€", "⃣"} {
		if !ValidEmoji(s) {
			t.Errorf("%q rejected", s)
		}
	}
	for _, s := range []string{"", "ok", "1", "#", "👍a", "👍 ", " 👍", "👍,", "👍;", "<👍", "\u00a0", "\ufe0f", "\u200d",
		"\ufe0f\u200d", "a\u2028", "👍\u3000", "👍\ufeff", "\xff", strings.Repeat("👍", MaxEmojiRunes+1)} {
		if ValidEmoji(s) {
			t.Errorf("%q accepted", s)
		}
	}
	if !ValidEmoji(strings.Repeat("👍", MaxEmojiRunes)) {
		t.Error("the longest emoji rejected")
	}
}

func TestEmojiKeyAndSpelling(t *testing.T) {
	if EmojiKey("❤️") != "❤" || EmojiKey("❤\ufe0e") != "❤" || EmojiKey("👍🏽") == EmojiKey("👍") {
		t.Error("EmojiKey")
	}
	if BetterSpelling("❤", "❤️") != "❤️" || BetterSpelling("❤️", "❤") != "❤️" || BetterSpelling("a", "b") != "a" {
		t.Error("BetterSpelling")
	}
	for in, want := range map[string]string{"❤": "❤️", " 👍 ": "👍", "✅": "✅", "🦀": "🦀", "☀": "☀"} {
		if got, err := ParseEmoji(in); err != nil || got != want {
			t.Errorf("ParseEmoji(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseEmoji("thumbsup"); err == nil {
		t.Error("ParseEmoji accepted a word")
	}
}

func TestReactorNames(t *testing.T) {
	for in, want := range map[string]string{"walm": "walm", " Walm ": "walm", "bob.smith": "bob.smith", "a-1": "a-1", "0": "0"} {
		if got, err := ParseReactor(in); err != nil || got != want {
			t.Errorf("ParseReactor(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "bob smith", "-a", ".a", "a,b", "a;b", "wålm", strings.Repeat("a", 65)} {
		if _, err := ParseReactor(in); err == nil {
			t.Errorf("ParseReactor(%q) accepted", in)
		}
	}
}

func TestReact(t *testing.T) {
	c := Comment{Author: "a", Text: "t"}
	steps := []struct {
		emoji, name string
		remove      bool
		changed     bool
		want        string
	}{
		{"👍", "walm", false, true, "👍 walm"},
		{"👍", "walm", false, false, "👍 walm"},
		{"👀", "agent", false, true, "👍 walm; 👀 agent"},
		{"👍", "anna", false, true, "👍 walm,anna; 👀 agent"},
		{"👍", "walm", true, true, "👍 anna; 👀 agent"},
		{"🎉", "walm", true, false, "👍 anna; 👀 agent"},
		{"👀", "agent", true, true, "👍 anna"},
		{"👍", "anna", true, true, ""},
		{"❤", "a", false, true, "❤ a"},
		{"❤️", "b", false, true, "❤ a,b"}, // same reaction, its first spelling
	}
	for i, s := range steps {
		before := CloneReactions(c.Reactions)
		changed, err := c.React(s.emoji, s.name, s.remove)
		if err != nil || changed != s.changed {
			t.Fatalf("step %d: changed %v, %v", i, changed, err)
		}
		if got := render(c.Reactions); got != s.want {
			t.Fatalf("step %d: %q, want %q", i, got, s.want)
		}
		if changed && SameReactions(before, c.Reactions) {
			t.Fatalf("step %d: changed but equal", i)
		}
	}
	if c.React("👍", "x", true); c.Reactions == nil {
		t.Fatal("lost reactions")
	}
	full := Comment{}
	for i := 0; i < MaxReactions; i++ {
		if _, err := full.React(string(rune(0x1F600+i)), "a", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := full.React("👍", "a", false); err == nil {
		t.Error("allowed one reaction too many")
	}
	if ch, err := full.React(string(rune(0x1F600)), "b", false); err != nil || !ch {
		t.Error("a name on an existing emoji at the limit was refused")
	}
}

func TestNormalizeReactions(t *testing.T) {
	in := []Reaction{{"❤", []string{"a"}}, {"👀", []string{"c", "c"}}, {"❤️", []string{"b", "a"}}, {"🎉", nil}}
	if got := render(NormalizeReactions(in)); got != "❤️ a,b; 👀 c" {
		t.Errorf("got %q", got)
	}
	if in[0].Emoji != "❤" || len(in[1].By) != 2 {
		t.Error("changed its input")
	}
}

func render(rs []Reaction) string {
	var parts []string
	for _, r := range rs {
		parts = append(parts, r.Emoji+" "+strings.Join(r.By, ","))
	}
	return strings.Join(parts, "; ")
}

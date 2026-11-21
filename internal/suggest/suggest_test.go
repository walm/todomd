package suggest

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	long := strings.Repeat("x", 41)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"explicit", "Ready?\n<!-- suggest: GO | push and merge | Done -->", []string{"GO", "push and merge", "Done"}},
		{"keyword case", "x\n<!-- SUGGEST:a|b -->", []string{"a", "b"}},
		{"trailing blank", "x\n<!-- suggest: a -->\n\n", []string{"a"}},
		{"dedupe", "x\n<!-- suggest: a | a | b -->", []string{"a", "b"}},
		{"explicit beats fallback", "Say **no**\n<!-- suggest: yes -->", []string{"yes"}},
		{"not last line", "<!-- suggest: a -->\nmore", nil},
		{"five options invalid", "x\n<!-- suggest: a|b|c|d|e -->", nil},
		{"empty option invalid", "x\n<!-- suggest: a||b -->", nil},
		{"too long invalid", "x\n<!-- suggest: " + long + " -->", nil},
		{"empty invalid", "x\n<!-- suggest: -->", nil},
		{"invalid line falls back", "Say **GO**\n<!-- suggest: a||b -->", []string{"GO"}},
		{"fallback", "Say **push and merge** to ship, or say **GO** there", []string{"push and merge", "GO"}},
		{"fallback case and dedupe", "SAY **A** or say **A** or say **B**", []string{"A", "B"}},
		{"fallback max 4", "say **1** say **2** say **3** say **4** say **5**", []string{"1", "2", "3", "4"}},
		{"fallback skips long", "say **" + long + "** or say **ok**", []string{"ok"}},
		{"bold without say", "this is **bold**", nil},
		{"say not directly before", "say then **bold**", nil},
		{"plain", "just a comment", nil},
	}
	for _, c := range cases {
		if got := Parse(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLine(t *testing.T) {
	got, err := Line([]string{"GO", " push and merge ", "GO"})
	if err != nil || got != "<!-- suggest: GO | push and merge -->" {
		t.Fatalf("got %q, %v", got, err)
	}
	if p := Parse("x\n" + got); !reflect.DeepEqual(p, []string{"GO", "push and merge"}) {
		t.Errorf("round trip: %q", p)
	}
	for _, bad := range [][]string{nil, {""}, {"a|b"}, {"a-->b"}, {strings.Repeat("x", 41)}, {"1", "2", "3", "4", "5"}} {
		if _, err := Line(bad); err == nil {
			t.Errorf("Line(%q) should fail", bad)
		}
	}
}

func TestStrip(t *testing.T) {
	cases := map[string]string{
		"Ready?\n<!-- suggest: GO | Done -->": "Ready?",
		"a\n\nb\n<!-- suggest: GO -->\n\n":    "a\n\nb",
		"<!-- suggest: GO -->":                "",
		"Say **GO**":                          "Say **GO**",
		"x\n<!-- suggest: a||b -->":           "x\n<!-- suggest: a||b -->",
		"<!-- suggest: GO -->\nmore":          "<!-- suggest: GO -->\nmore",
	}
	for in, want := range cases {
		if got := Strip(in); got != want {
			t.Errorf("Strip(%q) = %q, want %q", in, got, want)
		}
	}
}

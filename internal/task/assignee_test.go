package task

import (
	"slices"
	"strings"
	"testing"

	"github.com/walm/todomd/internal/mentions"
)

func TestValidHandle(t *testing.T) {
	for _, h := range []string{"a", "anna", "agent", "a-b", "x1", "0", strings.Repeat("a", 39), "a-" + strings.Repeat("b", 37)} {
		if !ValidHandle(h) {
			t.Errorf("%q rejected", h)
		}
	}
	for _, h := range []string{"", "-a", "a-", "Anna", "@anna", "a_b", "a.b", "a b", strings.Repeat("a", 40), "ånna", "anna "} {
		if ValidHandle(h) {
			t.Errorf("%q accepted", h)
		}
	}
}

// A valid handle is exactly what an @mention of it extracts.
func TestValidHandleMatchesMentions(t *testing.T) {
	for _, h := range []string{"a", "anna", "a-b-c", "x1", strings.Repeat("z", 39), "a--b"} {
		if got := mentions.Extract("@" + h); ValidHandle(h) != slices.Equal(got, []string{h}) {
			t.Errorf("%q: ValidHandle %v, Extract %v", h, ValidHandle(h), got)
		}
	}
}

func TestParseHandle(t *testing.T) {
	for in, want := range map[string]string{"@Anna": "anna", " bob ": "bob", "agent": "agent", "@a-1": "a-1"} {
		if got, err := ParseHandle(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "@", "@@anna", "a_b", "-x"} {
		if _, err := ParseHandle(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

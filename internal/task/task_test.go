package task

import "testing"

func TestDate(t *testing.T) {
	d, err := ParseDate("2026-08-01")
	if err != nil || d.String() != "2026-08-01" {
		t.Fatalf("%v %v", d, err)
	}
	if _, err := ParseDate("2026-13-01"); err == nil {
		t.Error("bad month accepted")
	}
	if _, err := ParseDate("tomorrow"); err == nil {
		t.Error("garbage accepted")
	}
	a, _ := ParseDate("2026-01-01")
	b, _ := ParseDate("2026-01-31")
	if !a.Before(b) || b.Before(a) {
		t.Error("Before broken")
	}
	if got := b.DaysUntil(a); got != 30 {
		t.Errorf("DaysUntil = %d", got)
	}
}

func TestNormalizeTag(t *testing.T) {
	for in, want := range map[string]string{
		"parser": "parser", "#core": "core", " #A_b-1 ": "a_b-1",
	} {
		got, err := NormalizeTag(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#", "has space", "bäd"} {
		if _, err := NormalizeTag(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestIDs(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID(taken)
		if !ValidID(id) || taken[id] {
			t.Fatalf("bad id %q", id)
		}
		taken[id] = true
	}
}

func TestValidation(t *testing.T) {
	if _, err := ValidateTitle("a\nb"); err == nil {
		t.Error("newline title accepted")
	}
	if _, err := ValidateTitle("  "); err == nil {
		t.Error("blank title accepted")
	}
	if got, _ := ValidateTitle("  ok  "); got != "ok" {
		t.Errorf("trim failed: %q", got)
	}
}

func TestPriority(t *testing.T) {
	// Normal is the zero value, so an untouched task is Normal.
	var zero Task
	if zero.Priority != PriorityNormal || zero.Priority.String() != "normal" {
		t.Errorf("zero value = %v (%s), want normal", zero.Priority, zero.Priority)
	}
	// Numeric order matches importance, so sorting works.
	if !(PriorityHigh > PriorityNormal && PriorityNormal > PriorityLow) {
		t.Error("priority ordering is wrong")
	}
	for in, want := range map[string]Priority{
		"high": PriorityHigh, "HIGH": PriorityHigh, " High ": PriorityHigh, "h": PriorityHigh,
		"normal": PriorityNormal, "": PriorityNormal, "n": PriorityNormal,
		"low": PriorityLow, "LOW": PriorityLow, "l": PriorityLow,
	} {
		got, err := ParsePriority(in)
		if err != nil || got != want {
			t.Errorf("ParsePriority(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"urgent", "critical", "1", "highest"} {
		if _, err := ParsePriority(bad); err == nil {
			t.Errorf("ParsePriority(%q) should fail", bad)
		}
	}
}

func TestParseLinkKind(t *testing.T) {
	for in, want := range map[string]string{
		"blocks": "blocks", "Blocked-By": "blocked by", "SPLIT FROM": "split from", "split\tinto": "split into",
		" duplicate-of ": "duplicate of", "duplicated  by": "duplicated by", "related": "related",
	} {
		if got, err := ParseLinkKind(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "bogus", "blocked--by", "blocked - by", "blocked_by", "duplicate", "split", "bloc\u212as", "related x", "-blocks"} {
		if got, err := ParseLinkKind(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
	for _, k := range LinkKinds {
		if !ValidLinkKind(k) || InverseLinkKind(InverseLinkKind(k)) != k {
			t.Errorf("%q", k)
		}
	}
	if len(LinkKinds) != 7 || InverseLinkKind("blocks") != "blocked by" || InverseLinkKind("related") != "related" || InverseLinkKind("x") != "" {
		t.Error("inverse")
	}
}

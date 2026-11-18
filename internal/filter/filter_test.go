package filter

import (
	"testing"

	"github.com/walm/todomd/internal/task"
)

func titles(t *testing.T, q string, me []string, cards []*task.Task) []string {
	t.Helper()
	f, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse(%q): %v", q, err)
	}
	var out []string
	for _, c := range cards {
		if f.Match(c, "Backlog", me) {
			out = append(out, c.Title)
		}
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The web's own cases (filter.test.ts).
func TestWebCases(t *testing.T) {
	cards := []*task.Task{
		{ID: "a1", Title: "Fix the parser", Priority: task.PriorityHigh},
		{ID: "b2", Title: "High contrast mode"},
		{ID: "c3", Title: "Tidy the docs", Priority: task.PriorityLow},
	}
	for q, want := range map[string][]string{
		"parser":        {"Fix the parser"},
		"a1":            {"Fix the parser"},
		"high":          {"High contrast mode"},
		"priority:high": {"Fix the parser"},
		"!high":         {"Fix the parser"},
		"!low":          {"Tidy the docs"},
		"!normal":       {"High contrast mode"},
		"!high parser":  {"Fix the parser"},
		"!high docs":    nil,
	} {
		if got := titles(t, q, nil, cards); !eq(got, want) {
			t.Errorf("%q = %v, want %v", q, got, want)
		}
	}
}

func TestTermsAndOr(t *testing.T) {
	cards := []*task.Task{
		{ID: "a1", Title: "A", Tags: []string{"ios"}, Assignee: "ios"},
		{ID: "b2", Title: "B", Tags: []string{"ios", "bug"}},
		{ID: "c3", Title: "C", Tags: []string{"android"}, Assignee: "anna", Sticker: "fire"},
		{ID: "d4", Title: "D", Description: "ping @ios please"},
		{ID: "e5", Title: "E", Tags: []string{"iosx"}},
	}
	me := []string{"ios"}
	for q, want := range map[string][]string{
		"tag:ios":                     {"A", "B"},
		"tag:#ios":                    {"A", "B"},
		"tag:ios -tag:bug":            {"A"},
		"ios":                         {"A", "B", "D", "E"}, // free text: substring, like the web
		"assignee:me":                 {"A"},
		"assignee:@Anna":              {"C"},
		"assignee:none":               {"B", "D", "E"},
		"assignee:any":                {"A", "C"},
		"has:assignee":                {"A", "C"},
		"assignee:me assignee:anna":   {"A", "C"}, // several assignees: any
		"mentions:me":                 {"D"},
		"@me":                         {"D"},
		"tag:android OR assignee:me":  {"A", "C"},
		"tag:bug OR tag:android":      {"B", "C"},
		"sticker:fire":                {"C"},
		"sticker:🔥":                   {"C"},
		"has:sticker":                 {"C"},
		"sticker:nope":                nil, // an unknown sticker stays a word
		"board:backlog tag:android":   {"C"},
		"board:Doing":                 nil,
		"-board:Doing tag:android":    {"C"},
		`board:"Backlog" tag:android`: {"C"},
		"assignee:":                   nil, // stays a word
		"-mentions:me tag:ios":        {"A", "B"},
	} {
		if got := titles(t, q, me, cards); !eq(got, want) {
			t.Errorf("%q = %v, want %v", q, got, want)
		}
	}
	if got := titles(t, "mentions:me", nil, cards); got != nil {
		t.Errorf("mentions:me with no handle of mine = %v, want none", got)
	}
}

func TestParseErrorsAndPass(t *testing.T) {
	for _, bad := range []string{"", "  ", "OR", "tag:a OR", "OR tag:a", "tag:a OR OR tag:b"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
	q, _ := Parse("tag:a")
	if !q.MentionsPass() {
		t.Error("mentions should pass by default")
	}
	q, _ = Parse("tag:a OR -mentions:me")
	if q.MentionsPass() {
		t.Error("-mentions:me should turn the pass-through off")
	}
	q, _ = Parse(" tag:a   OR  !high ")
	if q.String() != "tag:a OR !high" {
		t.Errorf("String = %q", q.String())
	}
}

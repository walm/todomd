// Package task defines the todomd data model: a File of Boards of Tasks,
// plus the Date type, ID generation, and field validation shared by the
// CLI and TUI.
package task

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Date is a calendar date with no time component.
type Date struct {
	Year  int
	Month int
	Day   int
}

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", s)
	}
	return Date{Year: t.Year(), Month: int(t.Month()), Day: t.Day()}, nil
}

// Today returns the current local date.
func Today() Date {
	now := time.Now()
	return Date{Year: now.Year(), Month: int(now.Month()), Day: now.Day()}
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func (d Date) IsZero() bool { return d == Date{} }

// Before reports whether d is strictly earlier than o.
func (d Date) Before(o Date) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

// DaysUntil returns the number of days from base to d (negative if past).
func (d Date) DaysUntil(base Date) int {
	a := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
	b := time.Date(base.Year, time.Month(base.Month), base.Day, 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Hours() / 24)
}

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		return nil
	}
	p, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = p
	return nil
}

// File is the parsed representation of a TODO.md.
type File struct {
	Title    string
	Preamble string // verbatim text between the title and the first board
	Boards   []*Board
}

// Board is a Kanban column.
type Board struct {
	Name  string
	Tasks []*Task
}

// Task is a single card.
type Task struct {
	ID          string
	Title       string
	Tags        []string
	Due         *Date
	Description string // verbatim markdown
	Comments    []Comment
}

// Comment is a dated note on a task, by a human or an AI.
type Comment struct {
	Author string
	Date   Date
	Text   string
}

// AllTasks returns every task in board order.
func (f *File) AllTasks() []*Task {
	var out []*Task
	for _, b := range f.Boards {
		out = append(out, b.Tasks...)
	}
	return out
}

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// IDLen is the length of generated task IDs.
const IDLen = 4

var idRe = regexp.MustCompile(`^[0-9a-z]{4}$`)

// ValidID reports whether s is a well-formed task ID.
func ValidID(s string) bool { return idRe.MatchString(s) }

// NewID returns a random base36 ID not present in taken.
func NewID(taken map[string]bool) string {
	for {
		b := make([]byte, IDLen)
		if _, err := rand.Read(b); err != nil {
			panic(err) // crypto/rand failure is unrecoverable
		}
		var sb strings.Builder
		for _, c := range b {
			sb.WriteByte(idAlphabet[int(c)%len(idAlphabet)])
		}
		id := sb.String()
		if !taken[id] {
			return id
		}
	}
}

// AssignIDs gives every task a valid unique ID: the first occurrence of a
// valid ID keeps it, duplicates and missing/invalid IDs are regenerated.
func (f *File) AssignIDs() {
	taken := map[string]bool{}
	var fix []*Task
	for _, t := range f.AllTasks() {
		if ValidID(t.ID) && !taken[t.ID] {
			taken[t.ID] = true
		} else {
			fix = append(fix, t)
		}
	}
	for _, t := range fix {
		t.ID = NewID(taken)
		taken[t.ID] = true
	}
}

var tagRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// NormalizeTag validates a tag, accepting an optional leading '#' and
// uppercase input.
func NormalizeTag(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#")))
	if !tagRe.MatchString(s) {
		return "", fmt.Errorf("invalid tag %q (want [a-z0-9_-]+)", s)
	}
	return s, nil
}

func singleLine(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	if strings.ContainsAny(s, "\n\r") {
		return "", fmt.Errorf("%s must not contain newlines", field)
	}
	return s, nil
}

// ValidateTitle checks a task title (single line, non-empty).
func ValidateTitle(s string) (string, error) { return singleLine("title", s) }

// ValidateBoardName checks a board name (single line, non-empty).
func ValidateBoardName(s string) (string, error) { return singleLine("board name", s) }

// ValidateAuthor checks a comment author (single line, non-empty).
func ValidateAuthor(s string) (string, error) { return singleLine("author", s) }

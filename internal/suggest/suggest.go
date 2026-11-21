// Package suggest reads and writes suggested replies in comment text. The
// syntax is shared with todomd.online and stored as plain text:
//
//	<!-- suggest: GO | push and merge | Done -->
//
// as the last line of a comment (keyword case-insensitive, 1-4 options
// separated by "|", each 1-40 characters, duplicates dropped). An invalid
// line is ignored. Only when there is no valid explicit line, a bold phrase
// right after "say" ("Say **push and merge** to ship") counts, up to 4.
package suggest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxOptions = 4
	MaxLen     = 40
)

var (
	explicit = regexp.MustCompile(`(?i)^<!--\s*suggest\s*:(.*)-->$`)
	fallback = regexp.MustCompile(`(?i)\bsay \*\*([^*\n]+)\*\*`)
)

// Parse returns the suggested replies in a comment's text, or nil.
func Parse(text string) []string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), " \t\n")
	last := text
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		last = text[i+1:]
	}
	if m := explicit.FindStringSubmatch(strings.TrimSpace(last)); m != nil {
		if opts, err := parseOptions(m[1]); err == nil {
			return opts
		}
	}
	var out []string
	for _, m := range fallback.FindAllStringSubmatch(text, -1) {
		o := strings.TrimSpace(m[1])
		if o == "" || utf8.RuneCountInString(o) > MaxLen || contains(out, o) {
			continue
		}
		out = append(out, o)
		if len(out) == MaxOptions {
			break
		}
	}
	return out
}

// Strip removes a valid explicit suggest line from the end of text, for
// views that show the options separately. Anything else is returned as is.
func Strip(text string) string {
	t := strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), " \t\n")
	i := strings.LastIndexByte(t, '\n')
	m := explicit.FindStringSubmatch(strings.TrimSpace(t[i+1:]))
	if m == nil {
		return text
	}
	if _, err := parseOptions(m[1]); err != nil {
		return text
	}
	if i < 0 {
		return ""
	}
	return strings.TrimRight(t[:i], " \t\n")
}

func parseOptions(s string) ([]string, error) {
	var out []string
	for _, o := range strings.Split(s, "|") {
		o = strings.TrimSpace(o)
		if o == "" || utf8.RuneCountInString(o) > MaxLen {
			return nil, errors.New("invalid option")
		}
		if !contains(out, o) {
			out = append(out, o)
		}
	}
	if len(out) > MaxOptions {
		return nil, errors.New("too many options")
	}
	return out, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Line builds the explicit line for opts, validating each.
func Line(opts []string) (string, error) {
	if len(opts) == 0 {
		return "", errors.New("no suggestions")
	}
	var clean []string
	for _, o := range opts {
		o = strings.TrimSpace(o)
		switch {
		case o == "":
			return "", errors.New("--suggest must not be empty")
		case utf8.RuneCountInString(o) > MaxLen:
			return "", fmt.Errorf("--suggest %q is longer than %d characters", o, MaxLen)
		case strings.ContainsAny(o, "|\n\r") || strings.Contains(o, "-->"):
			return "", fmt.Errorf(`--suggest %q must not contain "|", "-->" or a newline`, o)
		}
		if !contains(clean, o) {
			clean = append(clean, o)
		}
	}
	if len(clean) > MaxOptions {
		return "", fmt.Errorf("at most %d --suggest options", MaxOptions)
	}
	return "<!-- suggest: " + strings.Join(clean, " | ") + " -->", nil
}

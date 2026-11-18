// Package mentions finds @handle mentions in comment text. The syntax is
// shared with todomd.online and stored as plain text, so there is no file
// format: a mention is "@handle" where handle matches
// ^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$, written case-insensitively and
// normalised to lowercase.
//
// It must start the text or follow whitespace or one of ( [ " '. It ends at
// the first character that isn't a letter, digit or hyphen; a trailing
// hyphen isn't part of it. Emails, code spans, fenced code blocks, link
// destinations and autolinks don't count.
package mentions

import (
	"regexp"
	"strings"
)

const maxHandle = 39

var (
	// ](dest "title") — mask the destination only.
	linkDest = regexp.MustCompile(`\]\(\s*(?:<[^>\n]*>|[^)\s]*)`)
	autolink = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9+.-]*:[^>\s]*>`)
)

const mask = '\x00' // stands in for hidden text; never matches as a boundary

// Extract returns the distinct handles mentioned in text, lowercase, in
// order of first appearance.
func Extract(text string) []string {
	if !strings.Contains(text, "@") {
		return nil
	}
	s := []rune(maskCode(text))
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(s); i++ {
		if s[i] != '@' || (i > 0 && !boundary(s[i-1])) {
			continue
		}
		j := i + 1
		for j < len(s) && handleRune(s[j]) {
			j++
		}
		run := s[i+1 : j]
		for len(run) > 0 && run[len(run)-1] == '-' {
			run = run[:len(run)-1]
		}
		if len(run) == 0 || len(run) > maxHandle || run[0] == '-' {
			continue
		}
		h := strings.ToLower(string(run))
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
		i = j - 1
	}
	return out
}

// Has reports whether handle (any case, optional leading @) is mentioned.
func Has(mentioned []string, handle string) bool {
	handle = strings.ToLower(strings.TrimPrefix(handle, "@"))
	for _, m := range mentioned {
		if m == handle {
			return true
		}
	}
	return false
}

func handleRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
}

func boundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', '(', '[', '"', '\'':
		return true
	}
	return false
}

// maskCode replaces fenced blocks, code spans, link destinations and
// autolinks with mask runes (newlines kept), so they can't hold mentions.
func maskCode(text string) string {
	var out, prose []string
	flush := func() {
		if len(prose) > 0 {
			out = append(out, strings.Split(maskSpans(strings.Join(prose, "\n")), "\n")...)
			prose = nil
		}
	}
	var fence string // the opening fence marker while inside a block
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimLeft(ln, " ")
		switch {
		case fence != "":
			if closesFence(t, fence) {
				fence = ""
			}
			out = append(out, blank(ln))
		case len(ln)-len(t) <= 3 && opensFence(t) != "":
			flush()
			fence = opensFence(t)
			out = append(out, blank(ln))
		default:
			prose = append(prose, ln) // kept together so code spans can wrap lines
		}
	}
	flush()
	return strings.Join(out, "\n")
}

func opensFence(t string) string {
	for _, c := range []string{"`", "~"} {
		n := len(t) - len(strings.TrimLeft(t, c))
		if n >= 3 {
			if c == "`" && strings.Contains(t[n:], "`") {
				return "" // that's an inline span, not a fence
			}
			return strings.Repeat(c, n)
		}
	}
	return ""
}

func closesFence(t, fence string) bool {
	n := len(t) - len(strings.TrimLeft(t, fence[:1]))
	return n >= len(fence) && strings.TrimSpace(t[n:]) == ""
}

func blank(s string) string {
	return strings.Repeat(string(mask), len([]rune(s)))
}

// maskSpans hides inline code spans, then link destinations and autolinks.
func maskSpans(s string) string {
	r := []rune(s)
	for i := 0; i < len(r); {
		if r[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(r) && r[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(r); {
			if r[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(r) && r[j+m] == '`' {
				m++
			}
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			i += n // unmatched: literal backticks
			continue
		}
		for k := i; k < end; k++ {
			if r[k] != '\n' {
				r[k] = mask
			}
		}
		i = end
	}
	out := string(r)
	for _, re := range []*regexp.Regexp{linkDest, autolink} {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			if strings.HasPrefix(m, "]") {
				return "]" + blank(m[1:]) // keep "]" so "[x](" structure stays
			}
			return blank(m)
		})
	}
	return out
}

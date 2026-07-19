// Package markdown parses and writes the TODO.md format.
//
// The format is line-based: `#` file title, `##` boards, `###` tasks, an
// `<!-- id:xxxx -->` comment per task, an optional metadata line (inline-code
// tags and a due date) immediately after the ID comment, a verbatim
// description, and an optional `#### Comments` list. Code fences at column 0
// shield their contents from structural interpretation. Lines in descriptions
// that would otherwise read as structure are backslash-escaped on write and
// unescaped on parse, so any text round-trips exactly.
package markdown

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// ParseError is a structural error at a specific line of the file.
type ParseError struct {
	Line int // 1-based
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

var (
	idRe        = regexp.MustCompile(`^<!--\s*id:\s*([0-9a-z]+)\s*-->\s*$`)
	tagTokRe    = regexp.MustCompile("^`#([a-z0-9_-]+)`$")
	commentRe   = regexp.MustCompile(`^- \*\*(.+?)\*\* \((\d{4}-\d{2}-\d{2})\): ?(.*)$`)
	commentsHdr = regexp.MustCompile(`^#### Comments[ \t]*$`)
	fenceOpenRe = regexp.MustCompile("^(`{3,}|~{3,})")
	// structRe matches lines that carry structural meaning outside fences
	// and therefore need escaping inside descriptions.
	structRe = regexp.MustCompile("^(#{1,3} |#### Comments[ \t]*$|<!--\\s*id:)")
)

// fenceMask returns, per line, whether the line is structural (i.e. NOT
// inside or part of a column-0 code fence). Fence delimiter lines themselves
// count as non-structural.
func fenceMask(lines []string) []bool {
	structural := make([]bool, len(lines))
	var delim byte
	var delimLen int
	for i, l := range lines {
		if delim == 0 {
			if m := fenceOpenRe.FindString(l); m != "" {
				delim, delimLen = m[0], len(m)
				continue
			}
			structural[i] = true
		} else {
			trimmed := strings.TrimRight(l, " \t")
			if len(trimmed) >= delimLen && strings.Trim(trimmed, string(delim)) == "" {
				delim = 0
			}
		}
	}
	return structural
}

// UnclosedFence reports whether text ends with an open column-0 code fence.
// Such text cannot be embedded in a TODO.md without swallowing the rest of
// the file, so the store rejects it.
func UnclosedFence(text string) bool {
	probe := append(strings.Split(text, "\n"), "sentinel")
	mask := fenceMask(probe)
	return !mask[len(probe)-1]
}

// escapeLine adds one backslash to a line that would otherwise parse as
// structure; unescapeLine reverses it. Together they are bijective.
func escapeLine(s string) string {
	if structRe.MatchString(strings.TrimLeft(s, `\`)) {
		return `\` + s
	}
	return s
}

func unescapeLine(s string) string {
	if strings.HasPrefix(s, `\`) && structRe.MatchString(strings.TrimLeft(s, `\`)) {
		return s[1:]
	}
	return s
}

func trimBlankEdges(lines []string) (trimmed []string, offset int) {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end], start
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// parseMetadata reports whether line is a metadata line (only `#tag` tokens
// and at most one `**due:** YYYY-MM-DD`), and if so returns its contents.
func parseMetadata(line string) (tags []string, due *task.Date, ok bool) {
	toks := strings.Fields(line)
	if len(toks) == 0 {
		return nil, nil, false
	}
	for i := 0; i < len(toks); {
		if m := tagTokRe.FindStringSubmatch(toks[i]); m != nil {
			tags = append(tags, m[1])
			i++
			continue
		}
		if toks[i] == "**due:**" && due == nil && i+1 < len(toks) {
			d, err := task.ParseDate(toks[i+1])
			if err != nil {
				return nil, nil, false
			}
			due = &d
			i += 2
			continue
		}
		return nil, nil, false
	}
	return tags, due, true
}

// Parse reads a TODO.md. Input line endings may be CRLF; the model is
// LF-only. Every task in the returned File has a valid unique ID.
func Parse(data []byte) (*task.File, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	structural := fenceMask(lines)

	isHeading := func(i int, prefix string) bool {
		return i < len(lines) && structural[i] && strings.HasPrefix(lines[i], prefix)
	}
	// isTerminator: a line that ends a description or comments section.
	isTerminator := func(i int) bool {
		if i >= len(lines) || !structural[i] {
			return false
		}
		return strings.HasPrefix(lines[i], "# ") ||
			strings.HasPrefix(lines[i], "## ") ||
			strings.HasPrefix(lines[i], "### ") ||
			commentsHdr.MatchString(lines[i])
	}

	f := &task.File{}
	i := 0
	for i < len(lines) && isBlank(lines[i]) {
		i++
	}
	if isHeading(i, "# ") {
		f.Title = strings.TrimSpace(lines[i][2:])
		i++
	}

	// Preamble: up to the first board heading.
	preStart := i
	for i < len(lines) && !isHeading(i, "## ") {
		if isHeading(i, "### ") {
			return nil, &ParseError{i + 1, "task heading before any '## board' heading"}
		}
		i++
	}
	if pre, _ := trimBlankEdges(lines[preStart:i]); len(pre) > 0 {
		f.Preamble = strings.Join(pre, "\n")
	}

	for i < len(lines) {
		if !isHeading(i, "## ") {
			i++
			continue
		}
		name := strings.TrimSpace(lines[i][3:])
		if name == "" {
			return nil, &ParseError{i + 1, "empty board name"}
		}
		board := &task.Board{Name: name}
		f.Boards = append(f.Boards, board)
		i++

		for i < len(lines) && !isHeading(i, "## ") {
			if isBlank(lines[i]) {
				i++
				continue
			}
			if !isHeading(i, "### ") {
				return nil, &ParseError{i + 1, fmt.Sprintf("unexpected content under board %q, expected '### task'", name)}
			}
			t := &task.Task{Title: strings.TrimSpace(lines[i][4:])}
			if t.Title == "" {
				return nil, &ParseError{i + 1, "empty task title"}
			}
			board.Tasks = append(board.Tasks, t)
			i++

			// ID: the first non-blank line, if it is an id comment.
			j := i
			for j < len(lines) && isBlank(lines[j]) {
				j++
			}
			if j < len(lines) && structural[j] {
				if m := idRe.FindStringSubmatch(lines[j]); m != nil {
					t.ID = m[1]
					i = j + 1
				}
			}
			// Metadata: only the line immediately after the id comment (or
			// the heading, if no id) — a blank line in between demotes it to
			// description, which is how descriptions whose first line merely
			// *looks* like metadata survive round-trips.
			if i < len(lines) && structural[i] && !isBlank(lines[i]) && !isTerminator(i) {
				if tags, due, ok := parseMetadata(lines[i]); ok {
					t.Tags, t.Due = tags, due
					i++
				}
			}

			// Description.
			descStart := i
			for i < len(lines) && !isTerminator(i) {
				i++
			}
			desc, off := trimBlankEdges(lines[descStart:i])
			out := make([]string, len(desc))
			for j, l := range desc {
				if structural[descStart+off+j] {
					l = unescapeLine(l)
				}
				out[j] = l
			}
			if len(out) > 0 {
				t.Description = strings.Join(out, "\n")
			}

			// Comments.
			if i < len(lines) && structural[i] && commentsHdr.MatchString(lines[i]) {
				i++
				var cur *task.Comment
				pendingBlanks := 0
				flush := func() {
					if cur != nil {
						cur.Text = strings.TrimRight(cur.Text, "\n ")
						t.Comments = append(t.Comments, *cur)
						cur = nil
					}
					pendingBlanks = 0
				}
				for i < len(lines) {
					if structural[i] && (strings.HasPrefix(lines[i], "# ") ||
						strings.HasPrefix(lines[i], "## ") ||
						strings.HasPrefix(lines[i], "### ")) {
						break
					}
					l := lines[i]
					switch {
					case isBlank(l):
						pendingBlanks++
					case structural[i] && commentsHdr.MatchString(l):
						return nil, &ParseError{i + 1, "duplicate '#### Comments' section"}
					case strings.HasPrefix(l, "- "):
						m := commentRe.FindStringSubmatch(l)
						if m == nil {
							return nil, &ParseError{i + 1, "malformed comment item (want '- **author** (YYYY-MM-DD): text')"}
						}
						d, err := task.ParseDate(m[2])
						if err != nil {
							return nil, &ParseError{i + 1, err.Error()}
						}
						flush()
						cur = &task.Comment{Author: m[1], Date: d, Text: m[3]}
					case strings.HasPrefix(l, " "):
						if cur == nil {
							return nil, &ParseError{i + 1, "comment continuation without a comment item"}
						}
						cur.Text += strings.Repeat("\n", pendingBlanks+1) + stripIndent(l)
						pendingBlanks = 0
					default:
						return nil, &ParseError{i + 1, "unexpected line in comments section"}
					}
					i++
				}
				flush()
			}
		}
	}

	f.AssignIDs()
	return f, nil
}

func stripIndent(s string) string {
	if strings.HasPrefix(s, "  ") {
		return s[2:]
	}
	return strings.TrimPrefix(s, " ")
}

// Write renders a File canonically: Parse(Write(f)) is semantically equal to
// f, and Write is a fixed point over Parse.
func Write(f *task.File) []byte {
	var b strings.Builder
	title := f.Title
	if title == "" {
		title = "TODO"
	}
	b.WriteString("# " + title + "\n")
	if f.Preamble != "" {
		b.WriteString("\n" + f.Preamble + "\n")
	}
	for _, board := range f.Boards {
		b.WriteString("\n## " + board.Name + "\n")
		for _, t := range board.Tasks {
			b.WriteString("\n### " + t.Title + "\n")
			b.WriteString("<!-- id:" + t.ID + " -->\n")
			if meta := metaLine(t); meta != "" {
				b.WriteString(meta + "\n")
			}
			if t.Description != "" {
				b.WriteString("\n")
				lines := strings.Split(t.Description, "\n")
				mask := fenceMask(lines)
				for j, l := range lines {
					if mask[j] {
						l = escapeLine(l)
					}
					b.WriteString(l + "\n")
				}
			}
			if len(t.Comments) > 0 {
				b.WriteString("\n#### Comments\n\n")
				for _, c := range t.Comments {
					lines := strings.Split(c.Text, "\n")
					fmt.Fprintf(&b, "- **%s** (%s): %s\n", c.Author, c.Date, lines[0])
					for _, l := range lines[1:] {
						if isBlank(l) {
							b.WriteString("\n")
						} else {
							b.WriteString("  " + l + "\n")
						}
					}
				}
			}
		}
	}
	return []byte(b.String())
}

func metaLine(t *task.Task) string {
	var toks []string
	for _, tag := range t.Tags {
		toks = append(toks, "`#"+tag+"`")
	}
	if t.Due != nil {
		toks = append(toks, "**due:** "+t.Due.String())
	}
	return strings.Join(toks, " ")
}

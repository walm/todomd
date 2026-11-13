// Package markdown parses and writes the TODO.md format.
//
// The format is line-based: `#` file title, `##` boards, `###` tasks, an
// `<!-- id:xxxx -->` comment per task, an optional metadata line (inline-code
// tags, an optional priority, and a due date) immediately after the ID
// comment, an optional sticker line (see stickers.go), a verbatim
// description, an optional `#### Links` list (see links.go) and an optional
// `#### Comments` list, each comment's reactions on its last line (see
// reactions.go). Code fences at column 0 shield their contents from
// structural interpretation. Lines in descriptions that would otherwise read
// as structure are backslash-escaped on write and unescaped on parse, so any
// text round-trips exactly.
package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/walm/todomd/internal/task"
)

// FormatVersion is the TODO.md format this package reads and writes. It
// matches todomd.online's FORMAT_VERSION (packages/todofile), which its
// discovery document reports as format_version.
//
//	1  boards, cards, metadata line, description, comments
//	2  adds the `#### Links` section (card links)
//	3  adds the `<!-- sticker: NAME -->` line (card stickers)
//	4  adds the `<!-- reactions: … -->` line (comment reactions)
const FormatVersion = 4

// LinksFormatVersion is the first format version with card links. A server
// on an older one can't take links in sync ops.
const LinksFormatVersion = 2

// StickersFormatVersion is the first format version with card stickers. A
// server on an older one can't take stickers in sync ops.
const StickersFormatVersion = 3

// ReactionsFormatVersion is the first format version with comment
// reactions. A server on an older one can't take them in sync ops.
const ReactionsFormatVersion = 4

// ParseError is a structural error at a specific line of the file.
type ParseError struct {
	Line int // 1-based
	Msg  string
	// Code classifies the error the way todomd.online does: "parse_error"
	// (also when empty) for structure, and for strict parses
	// "missing_ids", "invalid_id" or "duplicate_board".
	Code string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// ErrCode is Code, defaulting to "parse_error".
func (e *ParseError) ErrCode() string {
	if e.Code == "" {
		return "parse_error"
	}
	return e.Code
}

var (
	idRe        = regexp.MustCompile(`^<!--\s*id:\s*([0-9a-z]+)\s*-->\s*$`)
	idLikeRe    = regexp.MustCompile(`^<!--\s*id:`)
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

// escapeMetaLine/unescapeMetaLine protect a description whose FIRST line
// happens to match the metadata grammar from being adopted as tags/due on
// reparse. Same stacking-backslash scheme as escapeLine.
func escapeMetaLine(s string) string {
	if _, ok := parseMetadata(strings.TrimLeft(s, `\`)); ok {
		return `\` + s
	}
	return s
}

func unescapeMetaLine(s string) string {
	if strings.HasPrefix(s, `\`) {
		if _, ok := parseMetadata(strings.TrimLeft(s, `\`)); ok {
			return s[1:]
		}
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

// meta is what a task's metadata line carries.
type meta struct {
	tags     []string
	due      *task.Date
	priority task.Priority
}

// parseMetadata reports whether line is a metadata line — only `#tag` tokens,
// at most one `**due:** YYYY-MM-DD`, and at most one `**priority:** high|
// normal|low` — and if so returns its contents.
func parseMetadata(line string) (m meta, ok bool) {
	toks := strings.Fields(line)
	if len(toks) == 0 {
		return meta{}, false
	}
	seenDue, seenPriority := false, false
	for i := 0; i < len(toks); {
		if t := tagTokRe.FindStringSubmatch(toks[i]); t != nil {
			m.tags = append(m.tags, t[1])
			i++
			continue
		}
		if toks[i] == "**due:**" && !seenDue && i+1 < len(toks) {
			d, err := task.ParseDate(toks[i+1])
			if err != nil {
				return meta{}, false
			}
			m.due, seenDue = &d, true
			i += 2
			continue
		}
		if toks[i] == "**priority:**" && !seenPriority && i+1 < len(toks) {
			p, err := task.ParsePriority(toks[i+1])
			if err != nil {
				return meta{}, false
			}
			m.priority, seenPriority = p, true
			i += 2
			continue
		}
		return meta{}, false
	}
	return m, true
}

// Parse reads a TODO.md. Input line endings may be CRLF; the model is
// LF-only. Every task in the returned File has a valid unique ID.
func Parse(data []byte) (*task.File, error) { return parse(data, false) }

// ParseStrict is Parse without the silent repairs, matching what
// todomd.online accepts: a task with a missing, malformed or duplicate
// `<!-- id: -->`, or two boards whose names differ only in case, is an error
// (Code "missing_ids", "invalid_id" or "duplicate_board") instead of being
// fixed up.
func ParseStrict(data []byte) (*task.File, error) { return parse(data, true) }

func parse(data []byte, strict bool) (*task.File, error) {
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
	taskLines := map[*task.Task]int{}
	boardLines := map[string]bool{}
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
			return nil, &ParseError{Line: i + 1, Msg: "task heading before any '## board' heading"}
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
			return nil, &ParseError{Line: i + 1, Msg: "empty board name"}
		}
		if strict {
			key := task.BoardKey(name)
			if boardLines[key] {
				return nil, &ParseError{Line: i + 1, Code: "duplicate_board",
					Msg: fmt.Sprintf("duplicate board %q (board names are compared case-insensitively)", name)}
			}
			boardLines[key] = true
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
				return nil, &ParseError{Line: i + 1, Msg: fmt.Sprintf("unexpected content under board %q, expected '### task'", name)}
			}
			t := &task.Task{Title: strings.TrimSpace(lines[i][4:])}
			if t.Title == "" {
				return nil, &ParseError{Line: i + 1, Msg: "empty task title"}
			}
			taskLines[t] = i + 1
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
				} else if strict && idLikeRe.MatchString(lines[j]) {
					return nil, &ParseError{Line: j + 1, Msg: "malformed task id", Code: "invalid_id"}
				}
			}
			// Metadata: the first non-blank line after the id comment (or
			// heading), iff it matches the strict grammar. Blank lines in
			// between are tolerated — markdown formatters like to insert
			// one after the id comment. A description whose first line
			// merely *looks* like metadata is backslash-escaped by the
			// writer, so it never reaches this check unescaped.
			j = i
			for j < len(lines) && isBlank(lines[j]) {
				j++
			}
			if j < len(lines) && structural[j] && !isTerminator(j) {
				if md, ok := parseMetadata(lines[j]); ok {
					t.Tags, t.Due, t.Priority = md.tags, md.due, md.priority
					i = j + 1
				}
			}

			// Sticker: the next non-blank line, iff it is exactly a sticker
			// line. An escaped one is the description's first line.
			stickerEscaped := -1
			j = i
			for j < len(lines) && isBlank(lines[j]) {
				j++
			}
			if j < len(lines) && structural[j] {
				if name, n, ok := stickerLine(lines[j]); ok {
					if n == 0 {
						t.Sticker = name
						i = j + 1
					} else {
						stickerEscaped = j
					}
				}
			}

			// Description, and the links section at its end.
			descStart := i
			for i < len(lines) && !isTerminator(i) {
				i++
			}
			descEnd, escaped := i, -1
			if h, links := linksSection(lines, structural, descStart, i); h >= 0 {
				if lines[h][0] == '#' {
					t.Links, descEnd = links, h
				} else {
					escaped = h
				}
			}
			desc, off := trimBlankEdges(lines[descStart:descEnd])
			out := make([]string, len(desc))
			for j, l := range desc {
				switch k := descStart + off + j; {
				case k == escaped, k == stickerEscaped:
					l = l[1:]
				case structural[k]:
					l = unescapeLine(l)
				}
				out[j] = l
			}
			if len(out) > 0 {
				out[0] = unescapeMetaLine(out[0])
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
						splitReactions(cur)
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
						return nil, &ParseError{Line: i + 1, Msg: "duplicate '#### Comments' section"}
					case structural[i] && linksHdrRe.MatchString(l) && l[0] == '#':
						return nil, &ParseError{Line: i + 1, Msg: "'#### Links' must come before '#### Comments'"}
					case strings.HasPrefix(l, "- "):
						m := commentRe.FindStringSubmatch(l)
						if m == nil {
							return nil, &ParseError{Line: i + 1, Msg: "malformed comment item (want '- **author** (YYYY-MM-DD): text')"}
						}
						d, err := task.ParseDate(m[2])
						if err != nil {
							return nil, &ParseError{Line: i + 1, Msg: err.Error()}
						}
						flush()
						cur = &task.Comment{Author: m[1], Date: d, Text: m[3]}
					case strings.HasPrefix(l, " "):
						if cur == nil {
							return nil, &ParseError{Line: i + 1, Msg: "comment continuation without a comment item"}
						}
						cur.Text += strings.Repeat("\n", pendingBlanks+1) + stripIndent(l)
						pendingBlanks = 0
					default:
						return nil, &ParseError{Line: i + 1, Msg: "unexpected line in comments section"}
					}
					i++
				}
				flush()
			}
		}
	}

	if !strict {
		f.AssignIDs()
	}
	for _, t := range f.AllTasks() {
		t.Links = dropSelfLinks(t.ID, t.Links)
	}

	if strict {
		seen := map[string]bool{}
		for _, t := range f.AllTasks() {
			line := taskLines[t]
			switch {
			case t.ID == "":
				return nil, &ParseError{Line: line, Code: "missing_ids",
					Msg: fmt.Sprintf("task %q has no <!-- id: --> comment", t.Title)}
			case !task.ValidID(t.ID):
				return nil, &ParseError{Line: line, Code: "invalid_id",
					Msg: fmt.Sprintf("task %q has a malformed id \"%s\"", t.Title, t.ID)}
			case seen[t.ID]:
				return nil, &ParseError{Line: line, Code: "invalid_id",
					Msg: fmt.Sprintf("duplicate task id \"%s\"", t.ID)}
			}
			seen[t.ID] = true
		}
	}
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
			b.WriteString("\n")
			writeTask(&b, t)
		}
	}
	return []byte(b.String())
}

func writeTask(b *strings.Builder, t *task.Task) {
	b.WriteString("### " + t.Title + "\n")
	b.WriteString("<!-- id:" + t.ID + " -->\n")
	if meta := metaLine(t); meta != "" {
		b.WriteString(meta + "\n")
	}
	if t.Sticker != "" {
		// After a blank line, where older versions start a description.
		b.WriteString("\n" + writeStickerLine(t.Sticker) + "\n")
	}
	if t.Description != "" {
		b.WriteString("\n")
		lines := strings.Split(t.Description, "\n")
		mask := fenceMask(lines)
		escape := -1
		if len(t.Links) == 0 {
			// Followed by nothing that could end a links section, the
			// description's own last one would be read as the card's.
			escape, _ = linksSection(lines, mask, 0, len(lines))
		}
		for j, l := range lines {
			if mask[j] {
				l = escapeLine(l)
				if j == 0 {
					l = escapeMetaLine(l)
					if _, _, ok := stickerLine(l); ok && t.Sticker == "" {
						// It would read as the card's sticker.
						l = `\` + l
					}
				}
				if j == escape {
					l = `\` + l
				}
			}
			b.WriteString(l + "\n")
		}
	}
	if len(t.Links) > 0 {
		b.WriteString("\n#### Links\n\n")
		for _, l := range t.Links {
			b.WriteString("- " + l.Kind + " " + l.ID + "\n")
		}
	}
	if len(t.Comments) > 0 {
		b.WriteString("\n#### Comments\n\n")
		for _, c := range t.Comments {
			lines := commentLines(c)
			fmt.Fprintf(b, "- **%s** (%s): %s\n", c.Author, c.Date, lines[0])
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

// WriteTask renders one task as a standalone fragment for external editing.
func WriteTask(t *task.Task) []byte {
	var b strings.Builder
	writeTask(&b, t)
	return []byte(b.String())
}

// ParseTask parses a fragment produced by WriteTask (and edited by a human)
// back into a single task. Error line numbers refer to the fragment.
func ParseTask(data []byte) (*task.Task, error) {
	const wrapper = "# t\n\n## b\n" // 3 lines before the fragment
	f, err := Parse([]byte(wrapper + string(data)))
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.Line -= 3
		}
		return nil, err
	}
	tasks := f.AllTasks()
	if len(tasks) != 1 {
		return nil, fmt.Errorf("fragment must contain exactly one task ('### title'), found %d", len(tasks))
	}
	return tasks[0], nil
}

func metaLine(t *task.Task) string {
	var toks []string
	for _, tag := range t.Tags {
		toks = append(toks, "`#"+tag+"`")
	}
	if t.Priority != task.PriorityNormal {
		// Normal is the default and by far the common case, so leaving it out
		// keeps files (and diffs) free of noise.
		toks = append(toks, "**priority:** "+t.Priority.String())
	}
	if t.Due != nil {
		toks = append(toks, "**due:** "+t.Due.String())
	}
	return strings.Join(toks, " ")
}

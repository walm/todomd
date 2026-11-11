package markdown

import (
	"regexp"

	"github.com/walm/todomd/internal/task"
)

// A card's links are a `#### Links` section at the end of its description,
// before `#### Comments`:
//
//	#### Links
//
//	- split from mpbv
//	- related 8xto
//
// The section is the description's last `#### Links` line (outside code
// fences) when every line after it is blank or an item, with at least one
// item. Anything else leaves the heading in the description, so a file with
// such a heading in its text never fails to parse, and older todomd versions,
// which don't know the section, keep it as description text byte for byte.
//
// The one description line that would read as the section is written with
// an extra leading backslash, and read back without it. Only that line is
// escaped: a `#### Links` heading anywhere else in a description stays as it
// is, so files written before links existed read and write back unchanged.
// See docs/card-links-format.md.
var (
	// linksHdrRe also matches the escaped forms, `\#### Links`, `\\#### Links`, ….
	linksHdrRe = regexp.MustCompile(`^\\*#### Links[ \t]*$`)
	linkItemRe = regexp.MustCompile(`^-[ \t]+(.+?)[ \t]+([0-9a-z]{4})[ \t]*$`)
)

// parseLinkItem reads `- <kind> <id>`.
func parseLinkItem(line string) (task.Link, bool) {
	m := linkItemRe.FindStringSubmatch(line)
	if m == nil {
		return task.Link{}, false
	}
	kind, err := task.ParseLinkKind(m[1])
	if err != nil {
		return task.Link{}, false
	}
	return task.Link{Kind: kind, ID: m[2]}, true
}

// linksSection looks for the links section in lines[from:to]: the last
// structural `#### Links` line, escaped or not, followed only by blank lines
// and at least one link item. It returns that line's index, or -1, and the
// items with duplicates removed.
func linksSection(lines []string, structural []bool, from, to int) (int, []task.Link) {
	h := -1
	for k := to - 1; k >= from; k-- {
		if structural[k] && linksHdrRe.MatchString(lines[k]) {
			h = k
			break
		}
	}
	if h < 0 {
		return -1, nil
	}
	var links []task.Link
	seen := map[task.Link]bool{}
	for k := h + 1; k < to; k++ {
		if isBlank(lines[k]) {
			continue
		}
		l, ok := parseLinkItem(lines[k])
		if !ok || !structural[k] {
			return -1, nil
		}
		if !seen[l] {
			seen[l] = true
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		return -1, nil
	}
	return h, links
}

// dropSelfLinks removes links from a card to itself.
func dropSelfLinks(id string, links []task.Link) []task.Link {
	var out []task.Link
	for _, l := range links {
		if l.ID != id {
			out = append(out, l)
		}
	}
	return out
}

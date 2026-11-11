package task

import (
	"fmt"
	"regexp"
	"strings"
)

// Link kinds, spelled as TODO.md and the sync wire format write them. Each
// relation can be written from either end; a link is stored only on the card
// where it was written, and readers compute the other side.
const (
	LinkSplitFrom    = "split from"
	LinkSplitInto    = "split into"
	LinkBlocks       = "blocks"
	LinkBlockedBy    = "blocked by"
	LinkRelated      = "related"
	LinkDuplicateOf  = "duplicate of"
	LinkDuplicatedBy = "duplicated by"
)

// LinkKinds lists every kind, in display order.
var LinkKinds = []string{
	LinkBlockedBy, LinkBlocks, LinkSplitFrom, LinkSplitInto, LinkDuplicateOf, LinkDuplicatedBy, LinkRelated,
}

var inverse = map[string]string{
	LinkSplitFrom:    LinkSplitInto,
	LinkSplitInto:    LinkSplitFrom,
	LinkBlocks:       LinkBlockedBy,
	LinkBlockedBy:    LinkBlocks,
	LinkRelated:      LinkRelated,
	LinkDuplicateOf:  LinkDuplicatedBy,
	LinkDuplicatedBy: LinkDuplicateOf,
}

// MaxLinks is the most links one card may store.
const MaxLinks = 50

// Link is one relation stored on a card: "<this card> <Kind> <ID>".
type Link struct {
	Kind string
	ID   string
}

func (l Link) String() string { return l.Kind + " " + l.ID }

// ValidLinkKind reports whether k is a kind in its canonical spelling.
func ValidLinkKind(k string) bool { _, ok := inverse[k]; return ok }

// InverseLinkKind is how a link of kind k reads from the other card:
// "split from" ↔ "split into", "blocks" ↔ "blocked by", "duplicate of" ↔
// "duplicated by", "related" ↔ "related". It is "" for an unknown kind.
func InverseLinkKind(k string) string { return inverse[k] }

var linkKindRe = regexp.MustCompile(`^([A-Za-z]+)(?:(?:[ \t]+|-)([A-Za-z]+))?$`)

// ParseLinkKind reads a kind the way TODO.md does: ASCII letters in any
// case, with the two words of a kind separated by spaces or tabs or by one
// '-' ("Blocked-By"). It returns the canonical spelling.
func ParseLinkKind(s string) (string, error) {
	if m := linkKindRe.FindStringSubmatch(strings.Trim(s, " \t")); m != nil {
		k := strings.ToLower(m[1])
		if m[2] != "" {
			k += " " + strings.ToLower(m[2])
		}
		if ValidLinkKind(k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("invalid link kind %q (want split from, split into, blocks, blocked by, related, duplicate of or duplicated by)", s)
}

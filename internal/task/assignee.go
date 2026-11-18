package task

import (
	"fmt"
	"regexp"
	"strings"
)

// A card's assignee is one handle, or "" for none. TODO.md writes it as
// `<!-- assignee: anna -->`. A handle follows the @mention rule (see
// internal/mentions), so "assigned to me" and "mentions me" compare the same
// strings. It doesn't have to be a todomd.online project member: agents
// (`agent`, `claude`) and people who left are fine. See
// docs/card-assignees-format.md.

// MaxHandle is the longest handle, in bytes (handles are ASCII).
const MaxHandle = 39

var handleRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)

// ValidHandle reports whether h is a handle in canonical (lowercase, no @)
// form. "" (no assignee) is not one.
func ValidHandle(h string) bool { return handleRe.MatchString(h) }

// ParseHandle reads a handle the way people type it: surrounding spaces and
// one leading @ dropped, any case.
func ParseHandle(s string) (string, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
	if !ValidHandle(h) {
		return "", fmt.Errorf("invalid handle %q (lowercase letters, digits and inner hyphens, up to %d characters)", s, MaxHandle)
	}
	return h, nil
}

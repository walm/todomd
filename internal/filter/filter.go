// Package filter is the query language of `todomd watch --filter` and
// `todomd changes --filter`. It is the web board's filter box (todomd-online
// web/src/lib/filter.ts) plus a few terms the CLI adds:
//
//	free text        a word every matching card has in its title, description,
//	                 id or tags (any case, substring)
//	priority:high    or !high, !normal, !low; several priorities match any
//	sticker:fire     or an emoji or a colour; has:sticker / sticker:any for any
//	assignee:me      assignee:anna, assignee:none, assignee:any (has:assignee,
//	                 is:mine for me); several assignee terms match any
//	mentions:me      or @me: the description or a comment @mentions me
//
//	tag:ios          the card has exactly this tag (CLI only)
//	-tag:ios         it doesn't (CLI only)
//	board:Doing      it is on this board; board:"In progress" for spaces
//	-board:Doing     it isn't (CLI only)
//	-mentions:me     it doesn't @mention me, and @mentions of me no longer
//	                 get through whatever the filter says (CLI only)
//	a OR b           either alternative; every term of one alternative must
//	                 match (CLI only; the web box has no OR)
//
// "me" is the watcher's own name (--as, --author). Not mirrored from the
// web: nothing; every web term is here. Terms the web would take as text
// (an unknown sticker, "assignee:") stay text here too.
package filter

import (
	"fmt"
	"strings"

	"github.com/walm/todomd/internal/mentions"
	"github.com/walm/todomd/internal/task"
)

type assignee struct {
	any, none, me bool
	handle        string
}

// group is one alternative: all of its terms must match.
type group struct {
	texts      []string
	priorities []string
	stickers   []string // "" stands for any
	assignees  []assignee
	mine       bool // mentions:me
	notMine    bool // -mentions:me
	tags       []string
	notTags    []string
	boards     []string
	notBoards  []string
}

// Query is a parsed filter.
type Query struct {
	src    string
	groups []group
}

// Parse reads a filter. The empty query is an error; callers pass no query
// for "no filter".
func Parse(src string) (*Query, error) {
	toks := tokenize(src)
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty filter")
	}
	q := &Query{src: strings.Join(toks, " ")}
	var cur []string
	flush := func() error {
		if len(cur) == 0 {
			return fmt.Errorf("filter %q: OR needs a term on both sides", src)
		}
		g, err := parseGroup(cur)
		if err != nil {
			return err
		}
		q.groups = append(q.groups, g)
		cur = nil
		return nil
	}
	for _, t := range toks {
		if t == "OR" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		cur = append(cur, t)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return q, nil
}

// tokenize splits on whitespace; a board: value may be "quoted".
func tokenize(s string) []string {
	var out []string
	rs := []rune(s)
	for i := 0; i < len(rs); {
		if rs[i] == ' ' || rs[i] == '\t' || rs[i] == '\n' || rs[i] == '\r' {
			i++
			continue
		}
		j := i
		for j < len(rs) && !strings.ContainsRune(" \t\r\n", rs[j]) {
			if rs[j] == '"' && strings.HasSuffix(strings.ToLower(string(rs[i:j])), "board:") {
				k := j + 1
				for k < len(rs) && rs[k] != '"' {
					k++
				}
				j = k
				if j < len(rs) {
					j++
				}
				break
			}
			j++
		}
		out = append(out, string(rs[i:j]))
		i = j
	}
	return out
}

func parseGroup(terms []string) (group, error) {
	var g group
	for _, raw := range terms {
		term := strings.ToLower(raw)
		switch {
		case term == "mentions:me" || term == "@me":
			g.mine = true
		case term == "-mentions:me" || term == "-@me":
			g.notMine = true
		case strings.HasPrefix(term, "tag:") && len(term) > 4:
			g.tags = append(g.tags, strings.TrimPrefix(term[4:], "#"))
		case strings.HasPrefix(term, "-tag:") && len(term) > 5:
			g.notTags = append(g.notTags, strings.TrimPrefix(term[5:], "#"))
		case strings.HasPrefix(term, "board:") && len(term) > 6:
			g.boards = append(g.boards, unquote(term[6:]))
		case strings.HasPrefix(term, "-board:") && len(term) > 7:
			g.notBoards = append(g.notBoards, unquote(term[7:]))
		default:
			if a, ok := assigneeTerm(term); ok {
				g.assignees = append(g.assignees, a)
			} else if p, ok := priorityTerm(term); ok {
				g.priorities = append(g.priorities, p)
			} else if s, ok := stickerTerm(term); ok {
				g.stickers = append(g.stickers, s)
			} else {
				g.texts = append(g.texts, term)
			}
		}
	}
	return g, nil
}

func unquote(s string) string { return strings.Trim(s, `"`) }

func assigneeTerm(term string) (assignee, bool) {
	switch term {
	case "assignee:me", "is:mine":
		return assignee{me: true}, true
	case "has:assignee", "assignee:any":
		return assignee{any: true}, true
	}
	if !strings.HasPrefix(term, "assignee:") {
		return assignee{}, false
	}
	v := strings.TrimPrefix(term[len("assignee:"):], "@")
	switch v {
	case "":
		return assignee{}, false
	case "none":
		return assignee{none: true}, true
	}
	return assignee{handle: v}, true
}

func priorityTerm(term string) (string, bool) {
	for _, p := range []string{"high", "normal", "low"} {
		if term == "priority:"+p || term == "!"+p {
			return p, true
		}
	}
	return "", false
}

func stickerTerm(term string) (string, bool) {
	if term == "has:sticker" || term == "sticker:any" {
		return "", true
	}
	if !strings.HasPrefix(term, "sticker:") {
		return "", false
	}
	name, err := task.ParseSticker(term[len("sticker:"):])
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}

// String is the filter as parsed, for showing and saving.
func (q *Query) String() string {
	if q == nil {
		return ""
	}
	return q.src
}

// MentionsPass reports whether @mentions of me get through whatever the
// filter says: unless a -mentions:me term says otherwise.
func (q *Query) MentionsPass() bool {
	for _, g := range q.groups {
		if g.notMine {
			return false
		}
	}
	return true
}

// Mentions reports whether t's description or a comment @mentions one of me.
func Mentions(t *task.Task, me []string) bool {
	if len(me) == 0 {
		return false
	}
	texts := []string{t.Description}
	for _, c := range t.Comments {
		texts = append(texts, c.Text)
	}
	for _, x := range texts {
		ms := mentions.Extract(x)
		for _, h := range me {
			if mentions.Has(ms, h) {
				return true
			}
		}
	}
	return false
}

// Match reports whether the card t, on board, is in the filter. me is the
// watcher's names (for assignee:me and mentions:me).
func (q *Query) Match(t *task.Task, board string, me []string) bool {
	for i := range q.groups {
		if q.groups[i].match(t, board, me) {
			return true
		}
	}
	return false
}

func (g *group) match(t *task.Task, board string, me []string) bool {
	if g.mine && !Mentions(t, me) {
		return false
	}
	if g.notMine && Mentions(t, me) {
		return false
	}
	if len(g.priorities) > 0 && !contains(g.priorities, t.Priority.String()) {
		return false
	}
	if len(g.assignees) > 0 {
		ok := false
		for _, a := range g.assignees {
			switch {
			case a.any:
				ok = ok || t.Assignee != ""
			case a.none:
				ok = ok || t.Assignee == ""
			case a.me:
				ok = ok || (t.Assignee != "" && handleIn(t.Assignee, me))
			default:
				ok = ok || strings.EqualFold(a.handle, t.Assignee)
			}
		}
		if !ok {
			return false
		}
	}
	if len(g.stickers) > 0 {
		if t.Sticker == "" || !(contains(g.stickers, "") || contains(g.stickers, t.Sticker)) {
			return false
		}
	}
	for _, x := range g.tags {
		if !hasTag(t, x) {
			return false
		}
	}
	for _, x := range g.notTags {
		if hasTag(t, x) {
			return false
		}
	}
	for _, x := range g.boards {
		if !strings.EqualFold(x, board) {
			return false
		}
	}
	for _, x := range g.notBoards {
		if strings.EqualFold(x, board) {
			return false
		}
	}
	if len(g.texts) > 0 {
		hay := strings.ToLower(strings.Join(append([]string{t.Title, t.Description, t.ID}, t.Tags...), " "))
		for _, x := range g.texts {
			if !strings.Contains(hay, x) {
				return false
			}
		}
	}
	return true
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func hasTag(t *task.Task, tag string) bool {
	for _, x := range t.Tags {
		if strings.EqualFold(x, tag) {
			return true
		}
	}
	return false
}

func handleIn(handle string, names []string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimPrefix(n, "@"), handle) {
			return true
		}
	}
	return false
}

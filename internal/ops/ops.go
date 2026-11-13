// Package ops is the lossless change list that sync sends instead of whole
// files: Delta(a, b) lists the changes that take a to b, and Apply replays
// them. Apply(a, Delta(a, b)) always writes the same bytes as b.
//
// The JSON form is todomd.online's wire format, and both directions follow
// its TypeScript reference (packages/todofile/src/delta.ts) exactly: the
// server answers a sync with Delta(what the client sent, merged result), so
// the two implementations must agree op for op. Boards are addressed by name
// (matched with task.BoardKey) and tasks by id; After names the board or task
// a new or moved item follows, nil meaning "first".
package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/walm/todomd/internal/task"
)

// Op names.
const (
	DocTitle      = "doc.title"
	DocPreamble   = "doc.preamble"
	BoardAdd      = "board.add"
	BoardDelete   = "board.delete"
	BoardOrder    = "board.order"
	BoardSpelling = "board.spelling"
	TaskAdd       = "task.add"
	TaskDelete    = "task.delete"
	TaskMove      = "task.move"
	TaskUpdate    = "task.update"
	CommentAdd    = "comment.add"
	CommentSet    = "comment.set"
)

// Op is one change. Only the fields its kind uses are meaningful:
//
//	doc.title       Title
//	doc.preamble    Preamble
//	board.add       Board, After
//	board.delete    Board (always an empty board)
//	board.order     Boards (the complete order after the change)
//	board.spelling  Board, Name (a change of case only)
//	task.add        Board, After, Task
//	task.delete     ID
//	task.move       ID, Board, After
//	task.update     ID, Set
//	comment.add     TaskID, Comment
//	comment.set     TaskID, Comments (the whole list after an edit/delete)
type Op struct {
	Op       string
	Title    string
	Preamble string
	Board    string
	After    *string
	Boards   []string
	Name     string
	Task     *task.Task
	ID       string
	Set      Fields
	TaskID   string
	Comment  task.Comment
	Comments []task.Comment
}

// Fields are the task fields a task.update replaces; nil means unchanged.
// Due needs a separate flag because "clear the due date" is a change too.
// Links, like Tags, is the card's whole list after the change. Sticker is
// the new sticker's name, "" to remove it.
type Fields struct {
	Title       *string
	Tags        *[]string
	Priority    *task.Priority
	HasDue      bool
	Due         *task.Date
	Sticker     *string
	Description *string
	Links       *[]task.Link
}

// Empty reports whether no field is set.
func (s Fields) Empty() bool {
	return s.Title == nil && s.Tags == nil && s.Priority == nil && !s.HasDue && s.Sticker == nil && s.Description == nil && s.Links == nil
}

// OpError names the first op that couldn't be read or applied.
type OpError struct {
	Index int // -1 when the list itself is malformed
	Msg   string
}

func (e *OpError) Error() string { return fmt.Sprintf("op %d: %s", e.Index, e.Msg) }

func strp(s string) *string { return &s }

// --- encoding ------------------------------------------------------------------

// object writes a JSON object with keys in the given order.
type object struct {
	b     bytes.Buffer
	first bool
}

func newObject() *object { o := &object{first: true}; o.b.WriteByte('{'); return o }

func (o *object) field(k string, v any) {
	if !o.first {
		o.b.WriteByte(',')
	}
	o.first = false
	kb, _ := json.Marshal(k)
	o.b.Write(kb)
	o.b.WriteByte(':')
	if raw, ok := v.(json.RawMessage); ok {
		o.b.Write(raw)
		return
	}
	vb, err := json.Marshal(v)
	if err != nil {
		panic(err) // only strings, slices and nested raw values reach here
	}
	o.b.Write(vb)
}

func (o *object) bytes() []byte { o.b.WriteByte('}'); return o.b.Bytes() }

func commentJSON(c task.Comment) json.RawMessage {
	o := newObject()
	o.field("author", c.Author)
	o.field("date", c.Date.String())
	o.field("text", c.Text)
	// Only comments with reactions carry the key, so every other comment
	// reads as it did before reactions existed.
	if len(c.Reactions) > 0 {
		o.field("reactions", reactionsJSON(c.Reactions))
	}
	return o.bytes()
}

func reactionsJSON(rs []task.Reaction) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, r := range rs {
		if i > 0 {
			b.WriteByte(',')
		}
		o := newObject()
		o.field("emoji", r.Emoji)
		o.field("by", r.By)
		b.Write(o.bytes())
	}
	b.WriteByte(']')
	return b.Bytes()
}

func commentsJSON(cs []task.Comment) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, c := range cs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(commentJSON(c))
	}
	b.WriteByte(']')
	return b.Bytes()
}

func linkJSON(l task.Link) json.RawMessage {
	o := newObject()
	o.field("kind", l.Kind)
	o.field("id", l.ID)
	return o.bytes()
}

func linksJSON(ls []task.Link) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, l := range ls {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(linkJSON(l))
	}
	b.WriteByte(']')
	return b.Bytes()
}

// stickerJSON is a sticker name, or null for none.
func stickerJSON(name string) any {
	if name == "" {
		return nil
	}
	return name
}

func tagsOf(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func dueJSON(d *task.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func taskJSON(t *task.Task) json.RawMessage {
	o := newObject()
	o.field("id", t.ID)
	o.field("title", t.Title)
	o.field("tags", tagsOf(t.Tags))
	o.field("priority", t.Priority.String())
	o.field("due", dueJSON(t.Due))
	// Only cards with a sticker carry the key (and only with links the
	// links key), so every other task.add reads as it did before.
	if t.Sticker != "" {
		o.field("sticker", t.Sticker)
	}
	o.field("description", t.Description)
	if len(t.Links) > 0 {
		o.field("links", linksJSON(t.Links))
	}
	o.field("comments", commentsJSON(t.Comments))
	return o.bytes()
}

// MarshalJSON writes the wire form, keys in the reference order.
func (op Op) MarshalJSON() ([]byte, error) {
	o := newObject()
	o.field("op", op.Op)
	after := func() {
		if op.After == nil {
			o.field("after", nil)
		} else {
			o.field("after", *op.After)
		}
	}
	switch op.Op {
	case DocTitle:
		o.field("title", op.Title)
	case DocPreamble:
		o.field("preamble", op.Preamble)
	case BoardAdd:
		o.field("board", op.Board)
		after()
	case BoardDelete:
		o.field("board", op.Board)
	case BoardOrder:
		o.field("boards", tagsOf(op.Boards))
	case BoardSpelling:
		o.field("board", op.Board)
		o.field("name", op.Name)
	case TaskAdd:
		o.field("board", op.Board)
		after()
		o.field("task", taskJSON(op.Task))
	case TaskDelete:
		o.field("id", op.ID)
	case TaskMove:
		o.field("id", op.ID)
		o.field("board", op.Board)
		after()
	case TaskUpdate:
		o.field("id", op.ID)
		s := newObject()
		if op.Set.Title != nil {
			s.field("title", *op.Set.Title)
		}
		if op.Set.Tags != nil {
			s.field("tags", tagsOf(*op.Set.Tags))
		}
		if op.Set.Priority != nil {
			s.field("priority", op.Set.Priority.String())
		}
		if op.Set.HasDue {
			s.field("due", dueJSON(op.Set.Due))
		}
		if op.Set.Sticker != nil {
			s.field("sticker", stickerJSON(*op.Set.Sticker))
		}
		if op.Set.Description != nil {
			s.field("description", *op.Set.Description)
		}
		if op.Set.Links != nil {
			s.field("links", linksJSON(*op.Set.Links))
		}
		o.field("set", json.RawMessage(s.bytes()))
	case CommentAdd:
		o.field("task", op.TaskID)
		o.field("comment", commentJSON(op.Comment))
	case CommentSet:
		o.field("task", op.TaskID)
		o.field("comments", commentsJSON(op.Comments))
	default:
		return nil, fmt.Errorf("unknown op %q", op.Op)
	}
	return o.bytes(), nil
}

// --- strict decoding -------------------------------------------------------------

var opKeys = map[string][]string{
	DocTitle:      {"title"},
	DocPreamble:   {"preamble"},
	BoardAdd:      {"board", "after"},
	BoardDelete:   {"board"},
	BoardOrder:    {"boards"},
	BoardSpelling: {"board", "name"},
	TaskAdd:       {"board", "after", "task"},
	TaskDelete:    {"id"},
	TaskMove:      {"id", "board", "after"},
	TaskUpdate:    {"id", "set"},
	CommentAdd:    {"task", "comment"},
	CommentSet:    {"task", "comments"},
}

var (
	taskKeys    = []string{"id", "title", "tags", "priority", "due", "description", "comments"}
	fieldKeys   = []string{"title", "tags", "priority", "due", "sticker", "description", "links"}
	commentKeys = []string{"author", "date", "text"}
	// reactions is optional in a comment: one without them leaves it out.
	reactionKeys = []string{"emoji", "by"}
	linkKeys     = []string{"kind", "id"}
)

type failure struct{ msg string }

func fail(format string, args ...any) { panic(failure{fmt.Sprintf(format, args...)}) }

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func readObject(raw json.RawMessage, what string, keys []string, required bool) map[string]json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		fail("%s must be an object", what)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(t, &m); err != nil {
		fail("%s must be an object", what)
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			fail("unknown key %s in %s", strconv.Quote(k), what)
		}
	}
	if required {
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				fail("missing key %s in %s", strconv.Quote(k), what)
			}
		}
	}
	return m
}

func readString(raw json.RawMessage, what string) string {
	t := bytes.TrimSpace(raw)
	var s string
	if len(t) == 0 || t[0] != '"' || json.Unmarshal(t, &s) != nil {
		fail("%s must be a string", what)
	}
	return s
}

func readID(raw json.RawMessage, what string) string {
	s := readString(raw, what)
	if !task.ValidID(s) {
		fail("%s is not a valid task id", what)
	}
	return s
}

func readDate(raw json.RawMessage, what string) task.Date {
	s := readString(raw, what)
	d, err := task.ParseDate(s)
	if err != nil || d.String() != s {
		fail("%s is not a valid date (want YYYY-MM-DD)", what)
	}
	return d
}

func readStrings(raw json.RawMessage, what string) []string {
	t := bytes.TrimSpace(raw)
	var items []json.RawMessage
	if len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &items) != nil {
		fail("%s must be an array", what)
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = readString(it, fmt.Sprintf("%s[%d]", what, i))
	}
	return out
}

func readPriority(raw json.RawMessage) task.Priority {
	switch readString(raw, "priority") {
	case "high":
		return task.PriorityHigh
	case "normal":
		return task.PriorityNormal
	case "low":
		return task.PriorityLow
	}
	fail("priority must be high, normal or low")
	return 0
}

func readComment(raw json.RawMessage, what string) task.Comment {
	c := readObject(raw, what, append(slices.Clone(commentKeys), "reactions"), false)
	for _, k := range commentKeys {
		if _, ok := c[k]; !ok {
			fail("missing key %s in %s", strconv.Quote(k), what)
		}
	}
	out := task.Comment{
		Author: readString(c["author"], what+".author"),
		Date:   readDate(c["date"], what+".date"),
		Text:   readString(c["text"], what+".text"),
	}
	if v, ok := c["reactions"]; ok {
		out.Reactions = readReactions(v, what+".reactions")
	}
	return out
}

// readReactions reads a comment's reactions: an array of {emoji, by}, each
// emoji once (variation selectors aside) with a non-empty list of distinct
// names. [] is no reactions.
func readReactions(raw json.RawMessage, what string) []task.Reaction {
	t := bytes.TrimSpace(raw)
	var items []json.RawMessage
	if len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &items) != nil {
		fail("%s must be an array", what)
	}
	var out []task.Reaction
	seen := map[string]bool{}
	for i, it := range items {
		w := fmt.Sprintf("%s[%d]", what, i)
		o := readObject(it, w, reactionKeys, true)
		r := task.Reaction{Emoji: readString(o["emoji"], w+".emoji"), By: readStrings(o["by"], w+".by")}
		if !task.ValidEmoji(r.Emoji) {
			fail("%s.emoji is not an emoji", w)
		}
		if seen[task.EmojiKey(r.Emoji)] {
			fail("%s.emoji repeats an emoji", w)
		}
		seen[task.EmojiKey(r.Emoji)] = true
		if len(r.By) == 0 {
			fail("%s.by must not be empty", w)
		}
		for j, n := range r.By {
			if !task.ValidReactor(n) {
				fail("%s.by[%d] is not a valid name (lowercase letters, digits, '.', '_', '-')", w, j)
			}
			if slices.Index(r.By, n) < j {
				fail("%s.by[%d] repeats a name", w, j)
			}
		}
		out = append(out, r)
	}
	return out
}

func readComments(raw json.RawMessage, what string) []task.Comment {
	t := bytes.TrimSpace(raw)
	var items []json.RawMessage
	if len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &items) != nil {
		fail("%s must be an array", what)
	}
	out := make([]task.Comment, len(items))
	for i, it := range items {
		out[i] = readComment(it, fmt.Sprintf("%s[%d]", what, i))
	}
	return out
}

func readLink(raw json.RawMessage, what string) task.Link {
	l := readObject(raw, what, linkKeys, true)
	kind := readString(l["kind"], what+".kind")
	if !task.ValidLinkKind(kind) {
		fail("%s.kind must be split from, split into, blocks, blocked by, related, duplicate of or duplicated by", what)
	}
	return task.Link{Kind: kind, ID: readID(l["id"], what+".id")}
}

// readLinks reads a link list; an empty one is nil, as Parse returns it.
func readLinks(raw json.RawMessage, what string) []task.Link {
	t := bytes.TrimSpace(raw)
	var items []json.RawMessage
	if len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &items) != nil {
		fail("%s must be an array", what)
	}
	var out []task.Link
	for i, it := range items {
		out = append(out, readLink(it, fmt.Sprintf("%s[%d]", what, i)))
	}
	return out
}

// readSticker reads a sticker name, or null for none ("").
func readSticker(raw json.RawMessage, what string) string {
	if isNull(raw) {
		return ""
	}
	name := readString(raw, what)
	if !task.ValidSticker(name) {
		fail("%s must be a sticker name (fire, star, …, red, amber, green, blue, violet) or null", what)
	}
	return name
}

func readAfter(raw json.RawMessage, id bool) *string {
	if isNull(raw) {
		return nil
	}
	if id {
		return strp(readID(raw, "after"))
	}
	return strp(readString(raw, "after"))
}

func readOp(raw json.RawMessage) Op {
	all := []string{"op"}
	for _, ks := range opKeys {
		all = append(all, ks...)
	}
	head := readObject(raw, "op", all, false)
	nameRaw, ok := head["op"]
	if !ok {
		fail("unknown op undefined")
	}
	var name string
	if json.Unmarshal(nameRaw, &name) != nil || opKeys[name] == nil {
		fail("unknown op %s", string(bytes.TrimSpace(nameRaw)))
	}
	o := readObject(raw, name, append([]string{"op"}, opKeys[name]...), true)
	op := Op{Op: name}
	switch name {
	case DocTitle:
		op.Title = readString(o["title"], "title")
	case DocPreamble:
		op.Preamble = readString(o["preamble"], "preamble")
	case BoardAdd:
		op.Board = readString(o["board"], "board")
		op.After = readAfter(o["after"], false)
	case BoardDelete:
		op.Board = readString(o["board"], "board")
	case BoardOrder:
		op.Boards = readStrings(o["boards"], "boards")
	case BoardSpelling:
		op.Board = readString(o["board"], "board")
		op.Name = readString(o["name"], "name")
	case TaskAdd:
		// sticker and links are optional: a task without them leaves them
		// out.
		t := readObject(o["task"], "task", append(slices.Clone(taskKeys), "sticker", "links"), false)
		for _, k := range taskKeys {
			if _, ok := t[k]; !ok {
				fail("missing key %s in task", strconv.Quote(k))
			}
		}
		tk := &task.Task{
			ID:          readID(t["id"], "task.id"),
			Title:       readString(t["title"], "task.title"),
			Tags:        readStrings(t["tags"], "task.tags"),
			Priority:    readPriority(t["priority"]),
			Description: readString(t["description"], "task.description"),
			Comments:    readComments(t["comments"], "task.comments"),
		}
		if v, ok := t["sticker"]; ok {
			tk.Sticker = readSticker(v, "task.sticker")
		}
		if v, ok := t["links"]; ok {
			tk.Links = readLinks(v, "task.links")
		}
		if !isNull(t["due"]) {
			d := readDate(t["due"], "task.due")
			tk.Due = &d
		}
		op.Board = readString(o["board"], "board")
		op.After = readAfter(o["after"], true)
		op.Task = tk
	case TaskDelete:
		op.ID = readID(o["id"], "id")
	case TaskMove:
		op.ID = readID(o["id"], "id")
		op.Board = readString(o["board"], "board")
		op.After = readAfter(o["after"], true)
	case TaskUpdate:
		s := readObject(o["set"], "set", fieldKeys, false)
		if v, ok := s["title"]; ok {
			op.Set.Title = strp(readString(v, "set.title"))
		}
		if v, ok := s["tags"]; ok {
			tags := readStrings(v, "set.tags")
			op.Set.Tags = &tags
		}
		if v, ok := s["priority"]; ok {
			p := readPriority(v)
			op.Set.Priority = &p
		}
		if v, ok := s["due"]; ok {
			op.Set.HasDue = true
			if !isNull(v) {
				d := readDate(v, "set.due")
				op.Set.Due = &d
			}
		}
		if v, ok := s["sticker"]; ok {
			op.Set.Sticker = strp(readSticker(v, "set.sticker"))
		}
		if v, ok := s["description"]; ok {
			op.Set.Description = strp(readString(v, "set.description"))
		}
		if v, ok := s["links"]; ok {
			links := readLinks(v, "set.links")
			op.Set.Links = &links
		}
		op.ID = readID(o["id"], "id")
	case CommentAdd:
		op.TaskID = readID(o["task"], "task")
		op.Comment = readComment(o["comment"], "comment")
	case CommentSet:
		op.TaskID = readID(o["task"], "task")
		op.Comments = readComments(o["comments"], "comments")
	}
	return op
}

// Decode reads a change list from untrusted JSON the way todomd.online's
// validateOps does: known op and field names only, every required key
// present, no extra keys, types checked, ids and dates well-formed. Whether
// the values can be represented in a TODO.md is a separate question (see
// merge.CheckTask).
func Decode(data []byte) (out []Op, err error) {
	t := bytes.TrimSpace(data)
	var items []json.RawMessage
	if len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &items) != nil {
		return nil, &OpError{Index: -1, Msg: "ops must be an array"}
	}
	i := 0
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(failure)
			if !ok {
				panic(r)
			}
			out, err = nil, &OpError{Index: i, Msg: f.msg}
		}
	}()
	out = make([]Op, 0, len(items))
	for ; i < len(items); i++ {
		out = append(out, readOp(items[i]))
	}
	return out, nil
}

// UnmarshalJSON reads one op strictly (see Decode).
func (op *Op) UnmarshalJSON(data []byte) error {
	list, err := Decode(append(append([]byte{'['}, data...), ']'))
	if err != nil {
		return err
	}
	*op = list[0]
	return nil
}

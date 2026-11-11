// Package merge is the three-way merge of the sync spec (§8), a port of
// todomd.online's merge.ts that has to produce byte-identical results: the
// server merges what a CLI pushes against head, and the CLI uses the same
// rules for its local re-merge when the file changed during a round trip.
//
// When both sides change one thing differently, theirs wins and ours' value
// is kept in a conflict comment by todomd-sync. The decisions the spec
// leaves open are made exactly as merge.ts makes them:
//   - Board order follows the same rule as task order.
//   - When both sides change a board's spelling, theirs wins, silently.
//   - An overlapping text edit takes theirs' whole text, not a mix.
//   - A board deleted on one side stays whenever a merged task ends up in it.
//   - A colliding ours task gets an id derived from its content, and an
//     identical copy theirs already holds (an earlier rename) is reused.
//   - A conflict comment isn't added twice.
//   - The file title and preamble keep ours' value in the Conflict's note.
//   - NoDeletesFromOurs also covers tags, links and comments.
//   - Links merge as a set, like tags. When ours' card is renamed after an
//     id collision, the links ours added that point at its old id are
//     remapped to the new one.
package merge

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

// SyncAuthor writes conflict comments.
const SyncAuthor = "todomd-sync"

// Conflict is one place where both sides changed the same thing.
type Conflict struct {
	Kind  string `json:"kind"` // field, text, delete_vs_edit, board_delete
	Task  string `json:"task,omitempty"`
	Board string `json:"board,omitempty"`
	Field string `json:"field,omitempty"`
	Kept  string `json:"kept"` // theirs or ours
	Note  string `json:"note"`
}

// Rename is an ours task whose id collided with a different task theirs
// added, and which got a new id.
type Rename struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Title string `json:"title"`
}

// Options tune a merge.
type Options struct {
	// Baseless: a first link with no common base; the same id means the
	// same task.
	Baseless bool
	// NoDeletesFromOurs: a git-caused change; nothing missing from ours is
	// deleted (spec §12.2).
	NoDeletesFromOurs bool
	// Date for conflict comments; the zero value means today.
	Date task.Date
}

// Result of a merge.
type Result struct {
	Merged    *task.File
	Conflicts []Conflict
	Renamed   []Rename
}

type loc struct {
	task *task.Task
	key  string // task.BoardKey of the board holding it
	name string // that board's name as spelled on this side
}

// ordered is a map that remembers insertion order, like a JavaScript Map.
type ordered[V any] struct {
	keys []string
	m    map[string]V
}

func newOrdered[V any]() *ordered[V] { return &ordered[V]{m: map[string]V{}} }

func (o *ordered[V]) set(k string, v V) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *ordered[V]) get(k string) (V, bool) { v, ok := o.m[k]; return v, ok }

func (o *ordered[V]) has(k string) bool { _, ok := o.m[k]; return ok }

func index(f *task.File) *ordered[*loc] {
	m := newOrdered[*loc]()
	for _, b := range f.Boards {
		key := task.BoardKey(b.Name)
		for _, t := range b.Tasks {
			m.set(t.ID, &loc{task: t, key: key, name: b.Name})
		}
	}
	return m
}

func commentKey(c task.Comment) string {
	return c.Author + "\x00" + c.Date.String() + "\x00" + c.Text
}

func sameComments(a, b []task.Comment) bool {
	return slices.EqualFunc(a, b, func(x, y task.Comment) bool { return commentKey(x) == commentKey(y) })
}

// sameContent compares everything but the id.
func sameContent(a, b *task.Task) bool {
	return a.Title == b.Title &&
		a.Priority == b.Priority &&
		task.SameDue(a.Due, b.Due) &&
		a.Description == b.Description &&
		slices.Equal(a.Tags, b.Tags) &&
		slices.Equal(a.Links, b.Links) &&
		sameComments(a.Comments, b.Comments)
}

// minus is xs minus ys, as multisets, keeping xs' order.
func minus(xs, ys []task.Comment) []task.Comment {
	count := map[string]int{}
	for _, y := range ys {
		count[commentKey(y)]++
	}
	var out []task.Comment
	for _, x := range xs {
		k := commentKey(x)
		if count[k] == 0 {
			out = append(out, x)
			continue
		}
		count[k]--
	}
	return out
}

func isPrefix(p, xs []task.Comment) bool {
	return len(p) <= len(xs) && sameComments(p, xs[:len(p)])
}

func concat(lists ...[]task.Comment) []task.Comment {
	var out []task.Comment
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// restoreComments is o with every comment of b that o lost put back at its
// place, b and o aligned on a longest common subsequence.
func restoreComments(b, o []task.Comment) []task.Comment {
	in := interner{}
	keys := func(cs []task.Comment) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = commentKey(c)
		}
		return out
	}
	m := match(in.ids(keys(b)), in.ids(keys(o)), MaxEditLines)
	if m == nil {
		// Too far apart to align cheaply: the lost ones go at the end.
		return concat(o, minus(b, o))
	}
	var out []task.Comment
	j := 0
	for i := range b {
		if m[i] < 0 {
			out = append(out, b[i])
			continue
		}
		for j < m[i] {
			out = append(out, o[j])
			j++
		}
		out = append(out, o[j])
		j++
	}
	return append(out, o[j:]...)
}

// --- Conflict comments --------------------------------------------------------

// fence is longer than any backtick run in text, so text can't close it.
func fence(text string) string {
	longest, run := 0, 0
	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// fenced is head plus text in a fenced block. Whitespace-only lines become
// empty, because a comment can't hold them.
func fenced(head, text string) string {
	if text == "" {
		return head + " empty."
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if isBlank(l) {
			lines[i] = ""
		}
	}
	body := strings.Join(lines, "\n")
	f := fence(body)
	return head + ":\n\n" + f + "\n" + body + "\n" + f
}

func was(field string) string {
	return "Sync conflict on " + field + " — this machine's version was"
}

func renderComments(cs []task.Comment) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		lines := strings.Split(c.Text, "\n")
		out := []string{fmt.Sprintf("- **%s** (%s): %s", c.Author, c.Date, lines[0])}
		for _, l := range lines[1:] {
			if l == "" {
				out = append(out, "")
			} else {
				out = append(out, "  "+l)
			}
		}
		parts[i] = strings.Join(out, "\n")
	}
	return strings.Join(parts, "\n")
}

// --- Field rules ----------------------------------------------------------------

// pick is the field-level rule. Without a base any difference conflicts.
func pick[T any](hasBase bool, b, o, t T, eq func(x, y T) bool) (T, bool) {
	switch {
	case eq(o, t):
		return o, false
	case !hasBase:
		return t, true
	case eq(o, b):
		return t, false
	case eq(t, b):
		return o, false
	}
	return t, true
}

func eqString(a, b string) bool { return a == b }

func eqPriority(a, b task.Priority) bool { return a == b }

func dedupe[T comparable](xs []T) []T {
	seen := map[T]bool{}
	var out []T
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func set[T comparable](xs []T) map[T]bool {
	m := map[T]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// mergeSet merges a per-card set, tags or links: it is never a conflict.
func mergeSet[T comparable](hasBase bool, b, o, t []T, noDeletes bool) []T {
	if slices.Equal(o, t) {
		return o
	}
	inT := set(t)
	if !hasBase {
		var extra []T
		for _, x := range o {
			if !inT[x] {
				extra = append(extra, x)
			}
		}
		return dedupe(append(slices.Clone(t), extra...))
	}
	if slices.Equal(o, b) {
		return t
	}
	if slices.Equal(t, b) && !noDeletes {
		return o
	}
	// (base ∪ addedOurs ∪ addedTheirs) − removedOurs − removedTheirs, in
	// theirs' order with ours' new items appended.
	inB, inO := set(b), set(o)
	var keep []T
	for _, x := range t {
		if noDeletes || !inB[x] || inO[x] {
			keep = append(keep, x)
		}
	}
	for _, x := range o {
		if !inT[x] && !inB[x] {
			keep = append(keep, x)
		}
	}
	return dedupe(keep)
}

type commentMerge struct {
	list     []task.Comment
	conflict bool
	lost     []task.Comment // ours' comments that didn't make it into list
}

func mergeComments(hasBase bool, b, o, t []task.Comment, noDeletes bool) commentMerge {
	done := func(list []task.Comment) commentMerge { return commentMerge{list: list} }
	if sameComments(o, t) {
		return done(o)
	}
	// Without a base both lists count as appended to an empty one.
	if !hasBase {
		return done(concat(t, minus(o, t)))
	}
	if sameComments(o, b) {
		return done(t)
	}
	oe := o
	if noDeletes {
		oe = restoreComments(b, o)
	}
	oAppends, tAppends := isPrefix(b, oe), isPrefix(b, t)
	switch {
	case oAppends && tAppends:
		// Base prefix, theirs' new, ours' new; one copy of what both added.
		return done(concat(t, minus(oe[len(b):], t[len(b):])))
	case tAppends:
		return done(concat(oe, minus(t[len(b):], oe)))
	case oAppends:
		return done(concat(t, minus(oe[len(b):], t)))
	case sameComments(oe, t):
		return done(t)
	}
	// Both edited or deleted existing comments: theirs.
	return commentMerge{list: t, conflict: true, lost: minus(oe, t)}
}

// stableID is a deterministic new id for a colliding task: FNV-1a over the
// UTF-8 of seed, NUL and a counter, as 4 base-36 digits, skipping taken ids.
func stableID(seed string, taken map[string]bool) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	for n := 0; ; n++ {
		h := uint32(0x811c9dc5)
		for _, c := range []byte(seed + "\x00" + strconv.Itoa(n)) {
			h = (h ^ uint32(c)) * 0x01000193
		}
		id := make([]byte, 4)
		for k := 0; k < 4; k++ {
			id[k] = alphabet[h%36]
			h /= 36
		}
		if !taken[string(id)] {
			return string(id)
		}
	}
}

// mergeOrder orders the members of one container (the boards of the file,
// or the tasks of a board). If only ours reordered the items all three sides
// share, ours' order leads; otherwise theirs'. Items missing from the leading
// order go after their nearest preceding item in the other side's order that
// is already placed, or at the top.
func mergeOrder(base, ours, theirs []string, members []string) []string {
	isMember := set(members)
	inO, inT := set(ours), set(theirs)
	var common []string
	for _, x := range base {
		if inO[x] && inT[x] {
			common = append(common, x)
		}
	}
	inC := set(common)
	reordered := func(side []string) bool {
		var kept []string
		for _, x := range side {
			if inC[x] {
				kept = append(kept, x)
			}
		}
		return !slices.Equal(kept, common)
	}
	lead, other := theirs, ours
	if reordered(ours) && !reordered(theirs) {
		lead, other = ours, theirs
	}
	var out []string
	for _, x := range lead {
		if isMember[x] {
			out = append(out, x)
		}
	}
	placed := set(out)
	for i, x := range other {
		if !isMember[x] || placed[x] {
			continue
		}
		at := 0
		for k := i - 1; k >= 0; k-- {
			if placed[other[k]] {
				at = slices.Index(out, other[k]) + 1
				break
			}
		}
		out = slices.Insert(out, at, x)
		placed[x] = true
	}
	// Members on neither side's list can't occur; keep them rather than lose them.
	for _, x := range members {
		if !placed[x] {
			out = append(out, x)
		}
	}
	return out
}

// remapLinks points the links ours added to t (those not on the base card
// with t's id) at the new id of a card ours renamed after an id collision,
// keeping the order and dropping a link the remap made a duplicate. It
// returns nil when nothing changes.
func remapLinks(t *task.Task, bIdx *ordered[*loc], moved map[string]string) []task.Link {
	if len(moved) == 0 || len(t.Links) == 0 {
		return nil
	}
	var inBase map[task.Link]bool
	if b, ok := bIdx.get(t.ID); ok {
		inBase = set(b.task.Links)
	}
	changed := false
	out := make([]task.Link, len(t.Links))
	for i, l := range t.Links {
		if to, ok := moved[l.ID]; ok && !inBase[l] {
			l.ID = to
			changed = true
		}
		out[i] = l
	}
	if !changed {
		return nil
	}
	return dedupe(out)
}

// --- merge ----------------------------------------------------------------------

type merger struct {
	date      task.Date
	noDeletes bool
	conflicts []Conflict
}

// Merge is spec §8. The result is valid, canonical and deterministic.
func Merge(base, ours, theirs *task.File, opts Options) (Result, error) {
	B, err := Normalize(base)
	if err != nil {
		return Result{}, fmt.Errorf("merge: base: %w", err)
	}
	O, err := Normalize(ours)
	if err != nil {
		return Result{}, fmt.Errorf("merge: ours: %w", err)
	}
	T, err := Normalize(theirs)
	if err != nil {
		return Result{}, fmt.Errorf("merge: theirs: %w", err)
	}
	mg := &merger{date: opts.Date, noDeletes: opts.NoDeletesFromOurs}
	if mg.date.IsZero() {
		mg.date = task.Today()
	}
	baseless := opts.Baseless
	var renamed []Rename

	// File title and preamble.
	title, conflict := pick(true, B.Title, O.Title, T.Title, eqString)
	if conflict {
		mg.conflicts = append(mg.conflicts, Conflict{Kind: "field", Field: "title", Kept: "theirs", Note: was("title") + ": " + O.Title})
	}
	preamble := T.Preamble
	if pm, ok := Merge3(B.Preamble, O.Preamble, T.Preamble); ok && CheckFrame(title, pm, nil) == nil {
		preamble = pm
	} else {
		mg.conflicts = append(mg.conflicts, Conflict{Kind: "text", Field: "preamble", Kept: "theirs", Note: fenced(was("preamble"), O.Preamble)})
	}

	// Ours' ids, with collisions (same new id, different content) renamed.
	bIdx, tIdx, oRaw := index(B), index(T), index(O)
	taken := map[string]bool{}
	for _, idx := range []*ordered[*loc]{bIdx, oRaw, tIdx} {
		for _, id := range idx.keys {
			taken[id] = true
		}
	}
	reused := map[string]bool{}
	oursID := map[string]string{}
	for _, id := range oRaw.keys {
		o := oRaw.m[id]
		t, inT := tIdx.get(id)
		if baseless || bIdx.has(id) || !inT || sameContent(o.task, t.task) {
			oursID[id] = id
			continue
		}
		copyID := ""
		for _, xid := range tIdx.keys {
			x := tIdx.m[xid]
			if !bIdx.has(xid) && !oRaw.has(xid) && !reused[xid] && sameContent(x.task, o.task) {
				copyID = xid
				break
			}
		}
		if copyID != "" {
			reused[copyID] = true
			oursID[id] = copyID
			continue
		}
		nid := stableID(id+"\n"+string(markdown.WriteTask(o.task)), taken)
		taken[nid] = true
		oursID[id] = nid
		renamed = append(renamed, Rename{From: id, To: nid, Title: o.task.Title})
	}
	moved := map[string]string{}
	for _, id := range oRaw.keys {
		if oursID[id] != id {
			moved[id] = oursID[id]
		}
	}
	oIdx := newOrdered[*loc]()
	for _, id := range oRaw.keys {
		o := oRaw.m[id]
		nid := oursID[id]
		links := remapLinks(o.task, bIdx, moved)
		if nid != id || links != nil {
			c := *o.task
			c.ID = nid
			if links != nil {
				c.Links = links
			}
			o = &loc{task: &c, key: o.key, name: o.name}
		}
		oIdx.set(nid, o)
	}

	// Each task: present or not, on which board, with which content.
	type placed struct {
		task *task.Task
		key  string
	}
	out := newOrdered[placed]()
	changed := func(b, x *loc) bool { return b.key != x.key || !sameContent(b.task, x.task) }
	ids := newOrdered[bool]()
	for _, idx := range []*ordered[*loc]{bIdx, oIdx, tIdx} {
		for _, id := range idx.keys {
			ids.set(id, true)
		}
	}
	for _, id := range ids.keys {
		b, inB := bIdx.get(id)
		o, inO := oIdx.get(id)
		t, inT := tIdx.get(id)
		switch {
		case inO && inT:
			if !inB && sameContent(o.task, t.task) {
				out.set(id, placed{t.task, t.key})
			} else {
				tk, key := mg.mergeTask(b, o, t)
				out.set(id, placed{tk, key})
			}
		case inO:
			if !inB {
				out.set(id, placed{o.task, o.key})
			} else if changed(b, o) {
				out.set(id, placed{o.task, o.key})
				mg.conflicts = append(mg.conflicts, Conflict{Kind: "delete_vs_edit", Task: id, Kept: "ours", Note: "deleted by theirs but changed by ours; kept ours"})
			}
		case inT:
			if !inB || mg.noDeletes {
				out.set(id, placed{t.task, t.key})
			} else if changed(b, t) {
				out.set(id, placed{t.task, t.key})
				mg.conflicts = append(mg.conflicts, Conflict{Kind: "delete_vs_edit", Task: id, Kept: "theirs", Note: "deleted by ours but changed by theirs; kept theirs"})
			}
		}
	}

	// Boards: present on both sides, added on one, or deleted on one side
	// but still holding a merged task.
	names := func(f *task.File) *ordered[string] {
		m := newOrdered[string]()
		for _, b := range f.Boards {
			m.set(task.BoardKey(b.Name), b.Name)
		}
		return m
	}
	bB, oB, tB := names(B), names(O), names(T)
	used := map[string]bool{}
	for _, id := range out.keys {
		used[out.m[id].key] = true
	}
	boards := newOrdered[string]()
	boardKeys := newOrdered[bool]()
	for _, k := range oB.keys {
		boardKeys.set(k, true)
	}
	for _, k := range tB.keys {
		boardKeys.set(k, true)
	}
	for _, key := range boardKeys.keys {
		bn, inB := bB.get(key)
		on, inO := oB.get(key)
		tn, inT := tB.get(key)
		switch {
		case inO && inT:
			v, _ := pick(inB, bn, on, tn, eqString)
			boards.set(key, v)
		case inT:
			if !inB || mg.noDeletes {
				boards.set(key, tn)
			} else if used[key] {
				boards.set(key, tn)
				mg.conflicts = append(mg.conflicts, Conflict{Kind: "board_delete", Board: tn, Kept: "theirs", Note: "board deleted by ours but it holds tasks; kept"})
			}
		case inO:
			if !inB {
				boards.set(key, on)
			} else if used[key] {
				boards.set(key, on)
				mg.conflicts = append(mg.conflicts, Conflict{Kind: "board_delete", Board: on, Kept: "ours", Note: "board deleted by theirs but it holds tasks; kept"})
			}
		}
	}
	for key := range used {
		if !boards.has(key) {
			return Result{}, fmt.Errorf("merge: task placed on missing board %q", key)
		}
	}

	keys := func(f *task.File) []string {
		out := make([]string, len(f.Boards))
		for i, b := range f.Boards {
			out[i] = task.BoardKey(b.Name)
		}
		return out
	}
	tasksOn := func(f *task.File, key string, rename func(string) string) []string {
		for _, b := range f.Boards {
			if task.BoardKey(b.Name) == key {
				out := make([]string, len(b.Tasks))
				for i, t := range b.Tasks {
					out[i] = rename(t.ID)
				}
				return out
			}
		}
		return nil
	}
	same := func(id string) string { return id }
	members := map[string][]string{}
	for _, id := range out.keys {
		k := out.m[id].key
		members[k] = append(members[k], id)
	}

	merged := &task.File{Title: title, Preamble: preamble}
	for _, key := range mergeOrder(keys(B), keys(O), keys(T), boards.keys) {
		nb := &task.Board{Name: boards.m[key]}
		order := mergeOrder(
			tasksOn(B, key, same),
			tasksOn(O, key, func(id string) string { return oursID[id] }),
			tasksOn(T, key, same),
			members[key],
		)
		for _, id := range order {
			nb.Tasks = append(nb.Tasks, out.m[id].task)
		}
		merged.Boards = append(merged.Boards, nb)
	}
	norm, err := Normalize(merged)
	if err != nil {
		return Result{}, fmt.Errorf("merge: result: %w", err)
	}
	return Result{Merged: norm, Conflicts: mg.conflicts, Renamed: renamed}, nil
}

func (mg *merger) mergeTask(b, o, t *loc) (*task.Task, string) {
	id := t.task.ID
	var notes []string
	field := func(name, kind, comment string) {
		mg.conflicts = append(mg.conflicts, Conflict{Kind: kind, Task: id, Field: name, Kept: "theirs", Note: comment})
		notes = append(notes, comment)
	}
	hasB := b != nil
	var bt *task.Task
	bKey := ""
	if hasB {
		bt, bKey = b.task, b.key
	} else {
		bt = &task.Task{}
	}
	ot, tt := o.task, t.task

	title, c := pick(hasB, bt.Title, ot.Title, tt.Title, eqString)
	if c {
		field("title", "field", was("title")+": "+ot.Title)
	}
	key, c := pick(hasB, bKey, o.key, t.key, eqString)
	if c {
		field("board", "field", was("board")+": "+o.name)
	}
	prio, c := pick(hasB, bt.Priority, ot.Priority, tt.Priority, eqPriority)
	if c {
		field("priority", "field", was("priority")+": "+ot.Priority.String())
	}
	due, c := pick(hasB, bt.Due, ot.Due, tt.Due, task.SameDue)
	if c {
		v := "none"
		if ot.Due != nil {
			v = ot.Due.String()
		}
		field("due", "field", was("due date")+": "+v)
	}

	// Line-based diff3; without a base any difference is a conflict.
	var desc string
	var ok bool
	if hasB {
		desc, ok = Merge3(bt.Description, ot.Description, tt.Description)
	} else if ot.Description == tt.Description {
		desc, ok = tt.Description, true
	}
	description := tt.Description
	if ok {
		description = desc
	} else {
		field("description", "text", fenced(was("description"), ot.Description))
	}

	cm := mergeComments(hasB, bt.Comments, ot.Comments, tt.Comments, mg.noDeletes)
	if cm.conflict {
		note := ""
		if len(cm.lost) > 0 {
			note = fenced("Sync conflict on comments — these comments from this machine were removed", renderComments(cm.lost))
		}
		n := note
		if n == "" {
			n = "both sides edited comments; kept theirs"
		}
		mg.conflicts = append(mg.conflicts, Conflict{Kind: "field", Task: id, Field: "comments", Kept: "theirs", Note: n})
		if note != "" {
			notes = append(notes, note)
		}
	}

	var dueCopy *task.Date
	if due != nil {
		d := *due
		dueCopy = &d
	}
	out := &task.Task{
		ID:          id,
		Title:       title,
		Tags:        mergeSet(hasB, bt.Tags, ot.Tags, tt.Tags, mg.noDeletes),
		Links:       mergeSet(hasB, bt.Links, ot.Links, tt.Links, mg.noDeletes),
		Priority:    prio,
		Due:         dueCopy,
		Description: description,
		Comments:    slices.Clone(cm.list),
	}
	// A clean line merge can still produce text that doesn't read back (two
	// fences that pair up differently, say): fall back to theirs.
	if description != tt.Description {
		probe := *out
		probe.Comments = nil
		if CheckTask(&probe) != nil {
			out.Description = tt.Description
			field("description", "text", fenced(was("description"), ot.Description))
		}
	}
	for _, text := range notes {
		dup := slices.ContainsFunc(out.Comments, func(c task.Comment) bool {
			return c.Author == SyncAuthor && c.Text == text
		})
		if !dup {
			out.Comments = append(out.Comments, task.Comment{Author: SyncAuthor, Date: mg.date, Text: text})
		}
	}
	return out, key
}

package ops

import (
	"slices"
	"strconv"

	"github.com/walm/todomd/internal/task"
)

func sameComment(a, b task.Comment) bool {
	return a.Author == b.Author && a.Date == b.Date && a.Text == b.Text
}

func sameComments(a, b []task.Comment) bool {
	return slices.EqualFunc(a, b, sameComment)
}

// longestIncreasing returns the indexes (into xs) of one longest strictly
// increasing subsequence of xs, by patience sorting.
func longestIncreasing(xs []int) map[int]bool {
	var tails []int
	prev := make([]int, len(xs))
	for i := range prev {
		prev[i] = -1
	}
	for i := range xs {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) >> 1
			if xs[tails[mid]] < xs[i] {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			prev[i] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	keep := map[int]bool{}
	start := -1
	if len(tails) > 0 {
		start = tails[len(tails)-1]
	}
	for i := start; i >= 0; i = prev[i] {
		keep[i] = true
	}
	return keep
}

// changedFields lists the fields of b that differ from a.
func changedFields(a, b *task.Task) Fields {
	var s Fields
	if a.Title != b.Title {
		s.Title = strp(b.Title)
	}
	if !slices.Equal(a.Tags, b.Tags) {
		tags := slices.Clone(tagsOf(b.Tags))
		s.Tags = &tags
	}
	if a.Priority != b.Priority {
		p := b.Priority
		s.Priority = &p
	}
	if !task.SameDue(a.Due, b.Due) {
		s.HasDue = true
		if b.Due != nil {
			d := *b.Due
			s.Due = &d
		}
	}
	if a.Sticker != b.Sticker {
		s.Sticker = strp(b.Sticker)
	}
	if a.Description != b.Description {
		s.Description = strp(b.Description)
	}
	if !slices.Equal(a.Links, b.Links) {
		links := slices.Clone(b.Links)
		s.Links = &links
	}
	return s
}

func indexOf(xs []string, x string) int { return slices.Index(xs, x) }

func insertAt(xs []string, at int, x string) []string {
	return slices.Insert(xs, at, x)
}

func removeValue(xs []string, x string) []string {
	if i := slices.Index(xs, x); i >= 0 {
		return slices.Delete(xs, i, i+1)
	}
	return xs
}

type frame struct {
	name string
	ids  []string
}

type aLoc struct {
	task *task.Task
	key  string
}

// Delta returns ops that take a to b: Write(Apply(a, Delta(a, b))) equals
// Write(b). The order is one Apply can always follow — doc, board spellings,
// new boards, task deletes, task placement and field updates (board by board,
// in b's order), board deletes, board order — so a board.delete only ever
// names an empty board and every After names something that exists.
func Delta(a, b *task.File) []Op {
	var out []Op
	if a.Title != b.Title {
		out = append(out, Op{Op: DocTitle, Title: b.Title})
	}
	if a.Preamble != b.Preamble {
		out = append(out, Op{Op: DocPreamble, Preamble: b.Preamble})
	}

	// A working copy of the board frame, kept in step with the ops so far;
	// After values are computed against it.
	work := map[string]*frame{}
	var order []string
	for _, board := range a.Boards {
		key := task.BoardKey(board.Name)
		ids := make([]string, len(board.Tasks))
		for i, t := range board.Tasks {
			ids[i] = t.ID
		}
		work[key] = &frame{name: board.Name, ids: ids}
		order = append(order, key)
	}

	bKeys := map[string]bool{}
	for _, board := range b.Boards {
		bKeys[task.BoardKey(board.Name)] = true
	}
	for _, board := range b.Boards {
		if w := work[task.BoardKey(board.Name)]; w != nil && w.name != board.Name {
			out = append(out, Op{Op: BoardSpelling, Board: w.name, Name: board.Name})
			w.name = board.Name
		}
	}
	for i, board := range b.Boards {
		key := task.BoardKey(board.Name)
		if work[key] != nil {
			continue
		}
		// b's previous board exists by now: it is either old or just added.
		var after *string
		at := 0
		if i > 0 {
			after = strp(b.Boards[i-1].Name)
			at = indexOf(order, task.BoardKey(*after)) + 1
		}
		out = append(out, Op{Op: BoardAdd, Board: board.Name, After: after})
		work[key] = &frame{name: board.Name}
		order = insertAt(order, at, key)
	}

	aTasks := map[string]*aLoc{}
	var aOrder []string
	for _, board := range a.Boards {
		key := task.BoardKey(board.Name)
		for _, t := range board.Tasks {
			aTasks[t.ID] = &aLoc{task: t, key: key}
			aOrder = append(aOrder, t.ID)
		}
	}
	bIDs := map[string]bool{}
	for _, t := range b.AllTasks() {
		bIDs[t.ID] = true
	}
	for _, id := range aOrder {
		if bIDs[id] {
			continue
		}
		out = append(out, Op{Op: TaskDelete, ID: id})
		w := work[aTasks[id].key]
		w.ids = removeValue(w.ids, id)
	}

	for _, board := range b.Boards {
		key := task.BoardKey(board.Name)
		w := work[key]
		// Tasks already in this board whose relative order is a longest
		// increasing run stay put; every other task is placed after its
		// predecessor in b, so the final relative order is b's.
		target := make([]string, len(board.Tasks))
		for i, t := range board.Tasks {
			target[i] = t.ID
		}
		pos := map[string]int{}
		for i, id := range w.ids {
			pos[id] = i
		}
		var residentIdx, residentPos []int
		for i, id := range target {
			if p, ok := pos[id]; ok {
				residentIdx = append(residentIdx, i)
				residentPos = append(residentPos, p)
			}
		}
		stable := map[int]bool{}
		for k := range longestIncreasing(residentPos) {
			stable[residentIdx[k]] = true
		}

		for i, t := range board.Tasks {
			var after *string
			if i > 0 {
				after = strp(target[i-1])
			}
			old := aTasks[t.ID]
			if !stable[i] {
				if old != nil {
					from := work[old.key]
					from.ids = removeValue(from.ids, t.ID)
					out = append(out, Op{Op: TaskMove, ID: t.ID, Board: board.Name, After: after})
					old.key = key
				} else {
					out = append(out, Op{Op: TaskAdd, Board: board.Name, After: after, Task: t.Clone()})
				}
				at := 0
				if after != nil {
					at = indexOf(w.ids, *after) + 1
				}
				w.ids = insertAt(w.ids, at, t.ID)
			}
			if old == nil {
				continue
			}
			if set := changedFields(old.task, t); !set.Empty() {
				out = append(out, Op{Op: TaskUpdate, ID: t.ID, Set: set})
			}
			oc := old.task.Comments
			if sameComments(oc, t.Comments) {
				continue
			}
			if len(oc) < len(t.Comments) && sameComments(oc, t.Comments[:len(oc)]) {
				for _, c := range t.Comments[len(oc):] {
					out = append(out, Op{Op: CommentAdd, TaskID: t.ID, Comment: c})
				}
			} else {
				out = append(out, Op{Op: CommentSet, TaskID: t.ID, Comments: slices.Clone(t.Comments)})
			}
		}
	}

	for _, board := range a.Boards {
		key := task.BoardKey(board.Name)
		if bKeys[key] {
			continue
		}
		out = append(out, Op{Op: BoardDelete, Board: work[key].name})
		order = removeValue(order, key)
	}
	want := make([]string, len(b.Boards))
	names := make([]string, len(b.Boards))
	for i, board := range b.Boards {
		want[i] = task.BoardKey(board.Name)
		names[i] = board.Name
	}
	if !slices.Equal(order, want) {
		out = append(out, Op{Op: BoardOrder, Boards: names})
	}
	return out
}

// Apply replays ops onto a copy of a. It fails with an *OpError naming the
// first op that doesn't fit the model (unknown board or task, duplicate id,
// …). Field values are not validated here; see merge.CheckTask.
func Apply(a *task.File, list []Op) (*task.File, error) {
	f := a.Clone()
	home := map[string]*task.Board{}
	for _, b := range f.Boards {
		for _, t := range b.Tasks {
			home[t.ID] = b
		}
	}
	for i, op := range list {
		if err := applyOne(f, home, op); err != "" {
			return nil, &OpError{Index: i, Msg: err}
		}
	}
	return f, nil
}

func q(s string) string { return strconv.Quote(s) }

func findBoard(f *task.File, name string) *task.Board {
	key := task.BoardKey(name)
	for _, b := range f.Boards {
		if task.BoardKey(b.Name) == key {
			return b
		}
	}
	return nil
}

func taskIndex(b *task.Board, id string) int {
	return slices.IndexFunc(b.Tasks, func(t *task.Task) bool { return t.ID == id })
}

func applyOne(f *task.File, home map[string]*task.Board, op Op) string {
	insertPos := func(b *task.Board, after *string) (int, string) {
		if after == nil {
			return 0, ""
		}
		k := taskIndex(b, *after)
		if k < 0 {
			return 0, "task " + q(*after) + " is not on board " + q(b.Name)
		}
		return k + 1, ""
	}
	switch op.Op {
	case DocTitle:
		f.Title = op.Title
	case DocPreamble:
		f.Preamble = op.Preamble
	case BoardAdd:
		if findBoard(f, op.Board) != nil {
			return "board " + q(op.Board) + " already exists"
		}
		at := 0
		if op.After != nil {
			ab := findBoard(f, *op.After)
			if ab == nil {
				return "unknown board " + q(*op.After)
			}
			at = slices.Index(f.Boards, ab) + 1
		}
		f.Boards = slices.Insert(f.Boards, at, &task.Board{Name: op.Board})
	case BoardDelete:
		b := findBoard(f, op.Board)
		if b == nil {
			return "unknown board " + q(op.Board)
		}
		if len(b.Tasks) > 0 {
			return "board " + q(op.Board) + " is not empty"
		}
		i := slices.Index(f.Boards, b)
		f.Boards = slices.Delete(f.Boards, i, i+1)
	case BoardOrder:
		seen := map[string]bool{}
		for _, n := range op.Boards {
			seen[task.BoardKey(n)] = true
		}
		if len(seen) != len(op.Boards) || len(op.Boards) != len(f.Boards) {
			return "board.order must list every board exactly once"
		}
		next := make([]*task.Board, len(op.Boards))
		for i, n := range op.Boards {
			b := findBoard(f, n)
			if b == nil {
				return "unknown board " + q(n)
			}
			next[i] = b
		}
		f.Boards = next
	case BoardSpelling:
		b := findBoard(f, op.Board)
		if b == nil {
			return "unknown board " + q(op.Board)
		}
		if task.BoardKey(op.Name) != task.BoardKey(b.Name) {
			return "board.spelling may only change case"
		}
		b.Name = op.Name
	case TaskAdd:
		if op.Task == nil || !task.ValidID(op.Task.ID) {
			id := ""
			if op.Task != nil {
				id = op.Task.ID
			}
			return "malformed task id " + q(id)
		}
		if home[op.Task.ID] != nil {
			return "task " + q(op.Task.ID) + " already exists"
		}
		b := findBoard(f, op.Board)
		if b == nil {
			return "unknown board " + q(op.Board)
		}
		at, err := insertPos(b, op.After)
		if err != "" {
			return err
		}
		b.Tasks = slices.Insert(b.Tasks, at, op.Task.Clone())
		home[op.Task.ID] = b
	case TaskDelete:
		b := home[op.ID]
		if b == nil {
			return "unknown task " + q(op.ID)
		}
		i := taskIndex(b, op.ID)
		b.Tasks = slices.Delete(b.Tasks, i, i+1)
		delete(home, op.ID)
	case TaskMove:
		from := home[op.ID]
		if from == nil {
			return "unknown task " + q(op.ID)
		}
		if op.After != nil && *op.After == op.ID {
			return "a task cannot follow itself"
		}
		to := findBoard(f, op.Board)
		if to == nil {
			return "unknown board " + q(op.Board)
		}
		i := taskIndex(from, op.ID)
		t := from.Tasks[i]
		from.Tasks = slices.Delete(from.Tasks, i, i+1)
		at, err := insertPos(to, op.After)
		if err != "" {
			return err
		}
		to.Tasks = slices.Insert(to.Tasks, at, t)
		home[op.ID] = to
	case TaskUpdate:
		b := home[op.ID]
		if b == nil {
			return "unknown task " + q(op.ID)
		}
		t := b.Tasks[taskIndex(b, op.ID)]
		s := op.Set
		if s.Title != nil {
			t.Title = *s.Title
		}
		if s.Tags != nil {
			t.Tags = slices.Clone(*s.Tags)
		}
		if s.Priority != nil {
			t.Priority = *s.Priority
		}
		if s.HasDue {
			t.Due = nil
			if s.Due != nil {
				d := *s.Due
				t.Due = &d
			}
		}
		if s.Sticker != nil {
			t.Sticker = *s.Sticker
		}
		if s.Description != nil {
			t.Description = *s.Description
		}
		if s.Links != nil {
			t.Links = nil
			if len(*s.Links) > 0 {
				t.Links = slices.Clone(*s.Links)
			}
		}
	case CommentAdd:
		b := home[op.TaskID]
		if b == nil {
			return "unknown task " + q(op.TaskID)
		}
		t := b.Tasks[taskIndex(b, op.TaskID)]
		t.Comments = append(t.Comments, op.Comment)
	case CommentSet:
		b := home[op.TaskID]
		if b == nil {
			return "unknown task " + q(op.TaskID)
		}
		b.Tasks[taskIndex(b, op.TaskID)].Comments = slices.Clone(op.Comments)
	default:
		return "unknown op " + q(op.Op)
	}
	return ""
}

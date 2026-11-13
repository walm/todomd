// Package todogen generates random valid TODO.md models and random edit
// scripts for property tests of ops and merge. It is a port of
// todomd.online's test/gen.ts (same RNG, same pools of hostile and unicode
// fragments), and like internal/fixture it is only imported from tests.
package todogen

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/task"
)

// Rng is mulberry32, as in gen.ts.
type Rng struct{ s uint32 }

// New seeds an Rng.
func New(seed uint32) *Rng { return &Rng{s: seed} }

// Next returns a float in [0, 1).
func (r *Rng) Next() float64 {
	r.s += 0x6d2b79f5
	t := r.s
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t^(t>>7))*(t|61)
	return float64(t^(t>>14)) / 4294967296
}

// Int returns an int in [0, n).
func (r *Rng) Int(n int) int { return int(r.Next() * float64(n)) }

// Chance is true with probability p.
func (r *Rng) Chance(p float64) bool { return r.Next() < p }

func pick[T any](r *Rng, xs []T) T { return xs[r.Int(len(xs))] }

var words = []string{
	"fix", "Add", "parser", "sync", "ünïcödé", "İstanbul", "straße", "日本語", "🙂", "`code`", "#hash",
	"**bold**", "<!-- id:abcd -->", "\\", "- [ ] x", "###", "a\u00a0b", "x\u2028y", "p\u0085q", "\ufeffbom",
	"—", "KELVIN", "ς", "tab\there", "<<<", ">>>", "=====", "~~~", "```", "é",
}

var lines = []string{
	"plain text", "Second paragraph.", "", "", "## fake board", "### fake task", "#### Comments", "# title",
	"<!-- id:zzzz -->", "<!-- id: abcd -->", "\\## escaped", "\\\\### double", "```", "```", "````", "~~~", "```go",
	"`#tag`", "`#tag` **due:** 2026-01-01", "**priority:** high", "**due:** 2026-02-30", "- **x** (2026-01-01): y",
	"- item", "    indented", "\t tab", "\u00a0", " ", "ünïcödé 🙂", "\ufeffbom", "x\u0085y", "\u2028", "line\u2029",
	"<<<<<<< HEAD", "=======", ">>>>>>> branch", "trailing   ", "İ ı K", "> quote", "#### heading 4", "##no space",
	"#### Links", "#### Links", "\\#### Links", "\\\\#### Links", "#### Links \t", "####  Links", "- related abcd",
	"- blocks zz01", "- Blocked-By zz02", "- split  from 0000", "- duplicate of ABCD", "- bogus abcd", "- related abcd x",
	"<!-- sticker: fire -->", "<!-- sticker: red -->", "\\<!-- sticker: star -->", "\\\\<!-- sticker: bug -->",
	"<!-- sticker: banana -->", "<!-- sticker: Fire -->", "<!--sticker: fire-->", "<!-- sticker: fire --> ", "<!-- sticker: 🔥 -->",
	"<!-- reactions: 👍 walm -->", "<!-- reactions: 👍 walm,anna; 👀 agent -->", "\\<!-- reactions: ✅ ai -->",
	"\\\\<!-- reactions: 🎉 x -->", "<!-- reactions: ❤ a; ❤️ b -->", "<!-- reactions: 👍 a,a -->", "<!-- reactions: 👍 Walm -->",
	"<!-- reactions: 👍  walm -->", "<!-- reactions: 👍 walm --> ", "<!-- reactions: ok walm -->", "<!-- reactions: 👍 -->",
}

var preambleLines = []string{
	"Project notes.", "", "See [docs](https://example.com).", "- a list", "```", "code", "```", "### not a task?",
	"\\## escaped", "ünïcödé", "\u00a0", "> quoted", "<!-- id:aaaa -->", "#### Comments", "x\u2028y",
}

var boards = []string{
	"Backlog", "In Progress", "Done", "Ärger", "straße", "Σίσυφος", "Kelvin", "İstanbul", "日本", "Review", "QA",
	"won’t do", "`code` board", "ǅungla", "ſtate", "<!-- id:aaaa -->",
}

var (
	authors = []string{"ai", "andreas", "Bob Smith", "todomd-sync", "ünï", "x*y", "a (b)"}
	emoji   = []string{"👍", "❤️", "❤", "🎉", "👀", "✅", "🙏", "👍🏽", "1️⃣", "🇸🇪", "👨‍👩‍👧", "⭐"}
	names   = []string{"walm", "anna", "agent", "claude", "bob.smith", "x_y", "a-1", "0"}
	dates   = []string{"2026-11-05", "2026-01-01", "0000-01-01", "2024-02-29", "9999-12-31", "2026-07-18"}
	tags    = []string{"cli", "sync", "a-b", "x_y", "1", "core", "ui", "z"}
)

const idChars = "0123456789abcdefghijklmnopqrstuvwxyz"

// linkTargets are ids links point at half the time: the IDPool ids merge
// tests draw new cards from (to provoke the rename remap) and a few others.
var linkTargets = []string{"zz01", "zz02", "zz03", "abcd", "0000"}

func linkTo(r *Rng, self string) task.Link {
	for {
		id := pick(r, linkTargets)
		if r.Chance(0.5) {
			b := make([]byte, 4)
			for i := range b {
				b[i] = idChars[r.Int(36)]
			}
			id = string(b)
		}
		if id != self {
			return task.Link{Kind: pick(r, task.LinkKinds), ID: id}
		}
	}
}

// addLink inserts a new link at a random place, unless the card already
// has it.
func addLink(r *Rng, t *task.Task) {
	l := linkTo(r, t.ID)
	if !slices.Contains(t.Links, l) {
		t.Links = slices.Insert(t.Links, r.Int(len(t.Links)+1), l)
	}
}

func date(s string) task.Date {
	d, err := task.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func randomID(r *Rng, taken map[string]bool, pool []string) string {
	for {
		var id string
		if len(pool) > 0 && r.Chance(0.5) {
			id = pick(r, pool)
		} else {
			b := make([]byte, 4)
			for i := range b {
				b[i] = idChars[r.Int(36)]
			}
			id = string(b)
		}
		if !taken[id] {
			taken[id] = true
			return id
		}
	}
}

func wordsOf(r *Rng, max int) string {
	n := 1 + r.Int(max)
	ws := make([]string, n)
	for i := range ws {
		ws[i] = pick(r, words)
	}
	sep := ""
	if r.Chance(0.8) {
		sep = " "
	}
	return strings.Join(ws, sep)
}

func blank() *task.Task { return &task.Task{ID: "aaaa", Title: "x"} }

func title(r *Rng) string {
	for {
		t := wordsOf(r, 4)
		b := blank()
		b.Title = t
		if merge.CheckTask(b) == nil {
			return t
		}
	}
}

func linesOf(r *Rng, pool []string, max int) string {
	n := r.Int(max + 1)
	out := make([]string, n)
	for i := range out {
		if r.Chance(0.15) {
			out[i] = wordsOf(r, 3)
		} else {
			out[i] = pick(r, pool)
		}
	}
	return strings.Join(out, "\n")
}

func retry(r *Rng, draw func() string, ok func(string) bool, fallback string) string {
	for i := 0; i < 20; i++ {
		if v := draw(); ok(v) {
			return v
		}
	}
	return fallback
}

func description(r *Rng) string {
	return retry(r, func() string { return linesOf(r, lines, 6) }, func(d string) bool {
		b := blank()
		b.Description = d
		return merge.CheckTask(b) == nil
	}, "plain text")
}

func commentText(r *Rng) string {
	return retry(r, func() string {
		if r.Chance(0.1) {
			return ""
		}
		return linesOf(r, lines, 3)
	}, func(text string) bool {
		b := blank()
		b.Comments = []task.Comment{{Author: "a", Date: date("2026-01-01"), Text: text}}
		return merge.CheckTask(b) == nil
	}, "ok")
}

func comment(r *Rng) task.Comment {
	c := task.Comment{Author: pick(r, authors), Date: date(pick(r, dates)), Text: commentText(r)}
	if r.Chance(0.3) {
		for k := 1 + r.Int(4); k > 0; k-- {
			react(r, &c)
		}
	}
	return c
}

// react adds a random reaction to c, or takes one away.
func react(r *Rng, c *task.Comment) {
	if len(c.Reactions) > 0 && r.Chance(0.3) {
		x := pick(r, c.Reactions)
		_, _ = c.React(x.Emoji, pick(r, x.By), true)
		return
	}
	_, _ = c.React(pick(r, emoji), pick(r, names), false)
}

func newTask(r *Rng, taken map[string]bool, idPool []string) *task.Task {
	var tg []string
	for i := r.Int(4); i > 0; i-- {
		tg = append(tg, pick(r, tags))
	}
	t := &task.Task{
		ID:       randomID(r, taken, idPool),
		Title:    title(r),
		Tags:     tg,
		Priority: pick(r, []task.Priority{task.PriorityNormal, task.PriorityNormal, task.PriorityHigh, task.PriorityLow}),
	}
	if r.Chance(0.3) {
		d := date(pick(r, dates))
		t.Due = &d
	}
	if r.Chance(0.6) {
		t.Description = description(r)
	}
	if r.Chance(0.3) {
		for k := 1 + r.Int(3); k > 0; k-- {
			addLink(r, t)
		}
	}
	if r.Chance(0.25) {
		t.Sticker = pick(r, task.Stickers).Name
	}
	n := 0
	if r.Chance(0.5) {
		n = r.Int(4)
	}
	for ; n > 0; n-- {
		t.Comments = append(t.Comments, comment(r))
	}
	if err := merge.CheckTask(t); err != nil {
		// Duplicate tags read back fine, so this only fires on a generator bug.
		panic(err)
	}
	return t
}

func boardName(r *Rng, f *task.File) string {
	keys := map[string]bool{}
	for _, b := range f.Boards {
		keys[task.BoardKey(b.Name)] = true
	}
	for i := 0; i < 10; i++ {
		n := pick(r, boards)
		if r.Chance(0.2) {
			n = strings.ToUpper(n)
		}
		if r.Chance(0.1) {
			n += " " + strconv.Itoa(1+r.Int(9))
		}
		if !keys[task.BoardKey(n)] && merge.CheckFrame("T", "", []string{n}) == nil {
			return n
		}
	}
	return ""
}

func preamble(r *Rng) string {
	return retry(r, func() string {
		if r.Chance(0.4) {
			return ""
		}
		return linesOf(r, preambleLines, 4)
	}, func(p string) bool { return merge.CheckFrame("T", p, nil) == nil }, "")
}

func boardNames(f *task.File) []string {
	out := make([]string, len(f.Boards))
	for i, b := range f.Boards {
		out[i] = b.Name
	}
	return out
}

func normalize(f *task.File) *task.File {
	if err := merge.CheckFrame(f.Title, f.Preamble, boardNames(f)); err != nil {
		panic(err)
	}
	n, err := merge.Normalize(f)
	if err != nil {
		panic(err)
	}
	return n
}

// File returns a random valid, canonical model.
func File(r *Rng, size int) *task.File {
	f := &task.File{}
	if !r.Chance(0.3) {
		f.Title = title(r)
	}
	f.Preamble = preamble(r)
	taken := map[string]bool{}
	n := r.Int(4 * size)
	if r.Chance(0.9) {
		n++
	}
	for ; n > 0; n-- {
		name := boardName(r, f)
		if name == "" {
			continue
		}
		b := &task.Board{Name: name}
		for k := r.Int(5 * size); k > 0; k-- {
			b.Tasks = append(b.Tasks, newTask(r, taken, nil))
		}
		f.Boards = append(f.Boards, b)
	}
	return normalize(f)
}

// respell toggles the case of some letters, keeping the name fold-equal.
func respell(r *Rng, name string) string {
	chars := []rune(name)
	for i, c := range chars {
		if !r.Chance(0.5) {
			continue
		}
		alt := unicode.ToLower(c)
		if c == alt {
			alt = unicode.ToUpper(c)
		}
		if task.BoardKey(string(alt)) == task.BoardKey(string(c)) {
			chars[i] = alt
		}
	}
	return string(chars)
}

func editLines(r *Rng, text string, pool []string) string {
	var ls []string
	if text != "" {
		ls = strings.Split(text, "\n")
	}
	k := r.Int(len(ls) + 1)
	switch r.Int(3) {
	case 0:
		v := pick(r, pool)
		if r.Chance(0.2) {
			v = wordsOf(r, 3)
		}
		ls = slices.Insert(ls, k, v)
	case 1:
		if k < len(ls) {
			ls = slices.Delete(ls, k, k+1)
		}
	default:
		if k < len(ls) {
			ls[k] = pick(r, pool)
		} else {
			ls = append(ls, pick(r, pool))
		}
	}
	return strings.Join(ls, "\n")
}

// EditOptions tune Mutate.
type EditOptions struct {
	Steps  int      // 0: 1 + a random number below 6
	IDPool []string // new task ids are drawn from here half the time, to provoke collisions
}

// Mutate applies a random edit script; the result is valid and canonical.
func Mutate(r *Rng, src *task.File, opts EditOptions) *task.File {
	f := src.Clone()
	steps := opts.Steps
	if steps == 0 {
		steps = 1 + r.Int(6)
	}
	taken := map[string]bool{}
	for _, t := range f.AllTasks() {
		taken[t.ID] = true
	}
	anyTask := func() (*task.Board, int) {
		var bs []*task.Board
		for _, b := range f.Boards {
			if len(b.Tasks) > 0 {
				bs = append(bs, b)
			}
		}
		if len(bs) == 0 {
			return nil, 0
		}
		b := pick(r, bs)
		return b, r.Int(len(b.Tasks))
	}
	editTask := func(fn func(t *task.Task)) {
		b, i := anyTask()
		if b == nil {
			return
		}
		cp := b.Tasks[i].Clone()
		fn(cp)
		if merge.CheckTask(cp) == nil {
			b.Tasks[i] = cp
		}
	}

	for s := 0; s < steps; s++ {
		switch r.Int(20) {
		case 0:
			if len(f.Boards) == 0 {
				break
			}
			b := pick(r, f.Boards)
			b.Tasks = slices.Insert(b.Tasks, r.Int(len(b.Tasks)+1), newTask(r, taken, opts.IDPool))
		case 1:
			if b, i := anyTask(); b != nil {
				b.Tasks = slices.Delete(b.Tasks, i, i+1)
			}
		case 2, 3:
			b, i := anyTask()
			if b == nil {
				break
			}
			t := b.Tasks[i]
			b.Tasks = slices.Delete(b.Tasks, i, i+1)
			to := b
			if !r.Chance(0.5) {
				to = pick(r, f.Boards)
			}
			to.Tasks = slices.Insert(to.Tasks, r.Int(len(to.Tasks)+1), t)
		case 4:
			editTask(func(t *task.Task) { t.Title = title(r) })
		case 5:
			editTask(func(t *task.Task) {
				if len(t.Tags) > 0 && r.Chance(0.5) {
					k := r.Int(len(t.Tags))
					t.Tags = slices.Delete(t.Tags, k, k+1)
				} else {
					t.Tags = slices.Insert(t.Tags, r.Int(len(t.Tags)+1), pick(r, tags))
				}
			})
		case 6:
			editTask(func(t *task.Task) {
				t.Priority = pick(r, []task.Priority{task.PriorityNormal, task.PriorityHigh, task.PriorityLow})
				t.Due = nil
				if !r.Chance(0.3) {
					d := date(pick(r, dates))
					t.Due = &d
				}
			})
		case 7, 8:
			editTask(func(t *task.Task) {
				if r.Chance(0.2) {
					t.Description = description(r)
				} else {
					t.Description = editLines(r, t.Description, lines)
				}
			})
		case 9:
			editTask(func(t *task.Task) { t.Comments = append(t.Comments, comment(r)) })
		case 10:
			editTask(func(t *task.Task) {
				if len(t.Comments) == 0 {
					return
				}
				k := r.Int(len(t.Comments))
				if r.Chance(0.5) {
					t.Comments = slices.Delete(t.Comments, k, k+1)
				} else {
					t.Comments[k].Text = commentText(r)
				}
			})
		case 11:
			if name := boardName(r, f); name != "" {
				f.Boards = slices.Insert(f.Boards, r.Int(len(f.Boards)+1), &task.Board{Name: name})
			}
		case 12:
			if len(f.Boards) == 0 {
				break
			}
			k := r.Int(len(f.Boards))
			gone := f.Boards[k]
			f.Boards = slices.Delete(f.Boards, k, k+1)
			if len(f.Boards) > 0 && r.Chance(0.5) {
				to := pick(r, f.Boards)
				to.Tasks = append(to.Tasks, gone.Tasks...)
			}
		case 13:
			if len(f.Boards) == 0 {
				break
			}
			b := pick(r, f.Boards)
			b.Name = respell(r, b.Name)
		case 14:
			if len(f.Boards) < 2 {
				break
			}
			k := r.Int(len(f.Boards))
			b := f.Boards[k]
			f.Boards = slices.Delete(f.Boards, k, k+1)
			f.Boards = slices.Insert(f.Boards, r.Int(len(f.Boards)+1), b)
		case 15:
			var p string
			if r.Chance(0.2) {
				p = preamble(r)
			} else {
				p = editLines(r, f.Preamble, preambleLines)
			}
			if merge.CheckFrame(f.Title, p, boardNames(f)) == nil {
				f.Preamble = p
			}
		case 16:
			f.Title = ""
			if !r.Chance(0.2) {
				f.Title = title(r)
			}
		case 17:
			editTask(func(t *task.Task) {
				switch {
				case len(t.Links) == 0 || r.Chance(0.4):
					addLink(r, t)
				case r.Chance(0.5):
					k := r.Int(len(t.Links))
					t.Links = slices.Delete(t.Links, k, k+1)
				default:
					// Same target, another kind (may equal a link it has:
					// then CheckTask rejects the edit).
					k := r.Int(len(t.Links))
					t.Links[k].Kind = pick(r, task.LinkKinds)
				}
			})
		case 18:
			editTask(func(t *task.Task) {
				if t.Sticker != "" && r.Chance(0.4) {
					t.Sticker = ""
				} else {
					t.Sticker = pick(r, task.Stickers).Name
				}
			})
		case 19:
			editTask(func(t *task.Task) {
				if len(t.Comments) > 0 {
					react(r, &t.Comments[r.Int(len(t.Comments))])
				}
			})
		}
	}
	return normalize(f)
}

package store

import (
	"errors"
	"slices"
	"testing"

	"github.com/walm/todomd/internal/task"
)

func linkFile() *task.File {
	l := func(kind, id string) task.Link { return task.Link{Kind: kind, ID: id} }
	return &task.File{Boards: []*task.Board{
		{Name: "Backlog", Tasks: []*task.Task{
			{ID: "aaaa", Title: "A", Links: []task.Link{l("split from", "pppp"), l("related", "gone"), l("blocked by", "bbbb")}},
			{ID: "bbbb", Title: "B", Links: []task.Link{l("blocks", "aaaa")}},
		}},
		{Name: "Done", Tasks: []*task.Task{
			{ID: "pppp", Title: "P", Links: []task.Link{l("related", "aaaa")}},
			{ID: "cccc", Title: "C", Links: []task.Link{l("duplicate of", "aaaa")}},
		}},
	}}
}

func TestLinksOf(t *testing.T) {
	f := linkFile()
	got := LinksOf(f, f.Boards[0].Tasks[0])
	want := []LinkView{
		{Kind: "blocked by", ID: "bbbb", Title: "B", Board: "Backlog", StoredOn: "aaaa"}, // stored on both ends: once
		{Kind: "split from", ID: "pppp", Title: "P", Board: "Done", StoredOn: "aaaa"},
		{Kind: "duplicated by", ID: "cccc", Title: "C", Board: "Done", StoredOn: "cccc"},
		{Kind: "related", ID: "gone", StoredOn: "aaaa", Missing: true},
		{Kind: "related", ID: "pppp", Title: "P", Board: "Done", StoredOn: "pppp"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := LinksOf(f, f.Boards[1].Tasks[0]); len(got) != 2 || got[0].Kind != "split into" || got[1].Kind != "related" {
		t.Errorf("pppp: %+v", got)
	}
}

func TestAddLink(t *testing.T) {
	f := linkFile()
	tk, l, added, err := AddLink(f, "ccc", "Blocked-By", "pp")
	if err != nil || !added || tk.ID != "cccc" || l != (task.Link{Kind: "blocked by", ID: "pppp"}) {
		t.Fatalf("%v %v %v %v", tk, l, added, err)
	}
	// The same edge from the other end is already there.
	if _, _, added, err := AddLink(f, "pppp", "blocks", "cccc"); err != nil || added {
		t.Errorf("inverse duplicate: added=%v %v", added, err)
	}
	if _, _, added, _ := AddLink(f, "cccc", "blocked by", "pppp"); added {
		t.Error("duplicate added")
	}
	var nf *NotFoundError
	if _, _, _, err := AddLink(f, "cccc", "related", "zzzz"); !errors.As(err, &nf) {
		t.Errorf("missing target: %v", err)
	}
	if _, _, _, err := AddLink(f, "cccc", "related", "cccc"); err == nil {
		t.Error("self-link accepted")
	}
	if _, _, _, err := AddLink(f, "cccc", "bogus", "aaaa"); err == nil {
		t.Error("bad kind accepted")
	}
	full := f.Boards[1].Tasks[1]
	full.Links = nil
	for i := 0; i < task.MaxLinks; i++ {
		full.Links = append(full.Links, task.Link{Kind: "related", ID: string(rune('a'+i%26)) + "x" + string(rune('a'+i/26)) + "y"})
	}
	if _, _, _, err := AddLink(f, "cccc", "related", "aaaa"); err == nil {
		t.Error("link over the limit accepted")
	}
}

func TestRemoveLinks(t *testing.T) {
	f := linkFile()
	// Both ends store the aaaa/bbbb edge; rm takes both.
	got, err := RemoveLinks(f, "bbbb", "aaaa", "")
	if err != nil || len(got) != 2 || got[0].StoredOn != "bbbb" || got[1].StoredOn != "aaaa" {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.Boards[0].Tasks[1].Links) != 0 || f.Boards[0].Tasks[1].Links != nil {
		t.Errorf("bbbb still has %v", f.Boards[0].Tasks[1].Links)
	}
	// --kind reads from the first card's side.
	var lnf *LinkNotFoundError
	if _, err := RemoveLinks(f, "aaaa", "pppp", "split into"); !errors.As(err, &lnf) {
		t.Errorf("wrong kind removed something: %v", err)
	}
	if got, err := RemoveLinks(f, "pppp", "aaaa", "split into"); err != nil || len(got) != 1 || got[0].Kind != "split from" {
		t.Errorf("%+v %v", got, err)
	}
	// A card that's no longer on the board.
	if got, err := RemoveLinks(f, "aaaa", "gone", ""); err != nil || len(got) != 1 {
		t.Errorf("dangling: %+v %v", got, err)
	}
	var nf *NotFoundError
	if _, err := RemoveLinks(f, "zzzz", "yyyy", ""); !errors.As(err, &nf) {
		t.Errorf("neither on the board: %v", err)
	}
	if _, err := RemoveLinks(f, "aaaa", "x", ""); !errors.As(err, &nf) {
		t.Errorf("bad id: %v", err)
	}
}

func TestParseLinkSpec(t *testing.T) {
	if k, ref, err := ParseLinkSpec("split-from:3f2a"); err != nil || k != "split from" || ref != "3f2a" {
		t.Errorf("%q %q %v", k, ref, err)
	}
	for _, bad := range []string{"3f2a", "nope:3f2a", ":3f2a"} {
		if _, _, err := ParseLinkSpec(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

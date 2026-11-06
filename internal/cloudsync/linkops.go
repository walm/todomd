package cloudsync

import (
	"context"
	"fmt"
	"time"

	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/task"
)

// First-link modes.
const (
	LinkMerge         = "merge"          // merge this file and the project (base-less)
	LinkReplaceLocal  = "replace-local"  // overwrite this file with the project
	LinkReplaceRemote = "replace-remote" // make this file the project's new head
)

// NewLink is the link to project p on host, not yet saved.
func NewLink(host string, p cloud.Project) *Link {
	return &Link{
		Host:         host,
		ProjectID:    p.ID,
		ProjectSlug:  p.Slug,
		ProjectURL:   p.URL,
		Jurisdiction: p.Jurisdiction,
		LinkedAt:     time.Now().UTC(),
	}
}

// emptyBoard is what a project made without a local file starts as: the
// same boards `todomd init` creates.
func emptyBoard() *task.File {
	return &task.File{Title: "TODO", Boards: []*task.Board{{Name: "Backlog"}, {Name: "In Progress"}, {Name: "Done"}}}
}

// Preview is what a first link would do, for the confirmation prompt.
type Preview struct {
	HeadRev    int
	FromOnline *Result // changes the link would make to this file
	FromFile   int     // tasks this file would add or change online
	Missing    bool    // there is no local file; it would be created
}

// CreateProject makes a new project from the file (or from an empty board
// when there is no file yet), writes the canonical result back and saves the
// link with its base. s.Link must name the host; it is filled in.
func (s *Syncer) CreateProject(ctx context.Context, name, repo, jurisdiction string) (*cloud.CreatedProject, error) {
	raw, local, missing, err := s.readLocal()
	if err != nil {
		return nil, err
	}
	if missing {
		local = emptyBoard()
	}
	created, err := s.Client.CreateProject(ctx, cloud.NewProject{
		Name: name, Repo: repo, Content: string(markdown.Write(local)), Jurisdiction: jurisdiction,
	})
	if err != nil {
		return nil, err
	}
	result, err := markdown.Parse([]byte(created.Content))
	if err != nil {
		return nil, fmt.Errorf("project content: %w", err)
	}
	host, gitMode := s.Link.Host, s.Link.Git
	*s.Link = *NewLink(host, created.Project)
	s.Link.Git = gitMode
	s.im = nil // the project exists only now
	res := &Result{}
	if err := s.writeBack(raw, missing, local, result, res); err != nil {
		return nil, err
	}
	return created, s.commitFirst(result, created.Rev)
}

func (s *Syncer) commitFirst(result *task.File, rev int) error {
	if err := SaveBase(s.Path, markdown.Write(result)); err != nil {
		return err
	}
	if s.Link.Git == GitTrack {
		s.Link.GitHead = GitHead(s.Path)
	}
	s.Link.BaseRev = rev
	s.Link.LastSync = &LastSync{At: time.Now().UTC(), Rev: rev, OK: true}
	return s.Link.Save(s.Path)
}

// PreviewFirst reports what FirstLink(mode) would do, without changing
// anything here or online.
func (s *Syncer) PreviewFirst(ctx context.Context, mode string) (*Preview, error) {
	_, local, missing, err := s.readLocal()
	if err != nil {
		return nil, err
	}
	doc, err := s.Client.Document(ctx, s.Link.ProjectID, 0)
	if err != nil {
		return nil, err
	}
	head, err := markdown.Parse([]byte(doc.Content))
	if err != nil {
		return nil, err
	}
	p := &Preview{HeadRev: doc.Rev, Missing: missing}
	res := &Result{DryRun: true}
	if err := s.startImages(res); err != nil {
		return nil, err
	}
	fileForm := local
	if !missing {
		local = s.im.toWire(ctx, local, nil, s.wireMode(true))
	}
	if missing || mode == LinkReplaceLocal {
		from := local
		if from == nil {
			from = &task.File{}
		}
		p.FromOnline = &Result{Rev: doc.Rev, Pulled: diffEvents(from, head), DryRun: true}
		return p, nil
	}
	if mode == LinkReplaceRemote {
		p.FromOnline = &Result{Rev: doc.Rev, DryRun: true}
		p.FromFile = len(diffEvents(head, local))
		return p, nil
	}
	resp, err := s.firstExchange(ctx, local, mode, true)
	if err != nil {
		return nil, err
	}
	result, err := markdown.Parse([]byte(*resp.Content))
	if err != nil {
		return nil, err
	}
	res.Rev, res.Pulled, res.Conflicts, res.Renamed = resp.Rev, diffEvents(fileForm, s.im.toLocal(result)), resp.Conflicts, resp.RenamedIDs
	p.FromOnline = res
	p.FromFile = len(diffEvents(head, result))
	return p, nil
}

func (s *Syncer) firstExchange(ctx context.Context, local *task.File, mode string, dryRun bool) (*cloud.SyncResponse, error) {
	content := string(markdown.Write(local))
	req := cloud.SyncRequest{RequestID: cloud.NewRequestID(), Full: true, Content: &content, DryRun: dryRun}
	if mode == LinkReplaceRemote {
		req.Mode = "replace_remote"
	}
	resp, err := s.Client.Sync(ctx, s.Link.ProjectID, req)
	if err != nil {
		return nil, err
	}
	if resp.Content == nil {
		return nil, fmt.Errorf("first sync with %s returned no document", cloud.HostName(s.Client.Host))
	}
	return resp, nil
}

// FirstLink links the file to the existing project in s.Link: merging both
// (a base-less first sync), or replacing one side with the other. With no
// local file the project's document is written to it.
func (s *Syncer) FirstLink(ctx context.Context, mode string) (*Result, error) {
	raw, local, missing, err := s.readLocal()
	if err != nil {
		return nil, err
	}
	res := &Result{Restored: missing}
	if err := s.startImages(res); err != nil {
		return nil, err
	}
	if missing || mode == LinkReplaceLocal {
		doc, err := s.Client.Document(ctx, s.Link.ProjectID, 0)
		if err != nil {
			return nil, err
		}
		result, err := markdown.Parse([]byte(doc.Content))
		if err != nil {
			return nil, err
		}
		res.Rev = doc.Rev
		if !s.NoAttachments {
			s.im.download(ctx, result)
		}
		// Replacing: the project's document wins outright, no merge.
		if err := s.Store.Locked(func() error {
			out := s.im.toLocal(result)
			if local != nil {
				res.Pulled = diffEvents(local, out)
			} else {
				res.Pulled = diffEvents(&task.File{}, out)
			}
			return s.Store.Save(out)
		}); err != nil {
			return nil, err
		}
		return res, s.commitFirst(result, doc.Rev)
	}
	wire := s.im.toWire(ctx, local, nil, s.wireMode(false))
	resp, err := s.firstExchange(ctx, wire, mode, false)
	if err != nil {
		return nil, err
	}
	result, err := markdown.Parse([]byte(*resp.Content))
	if err != nil {
		return nil, err
	}
	if resp.ContentSHA256 != "" && sha(markdown.Write(result)) != resp.ContentSHA256 {
		return nil, fmt.Errorf("the document from %s failed its checksum; try again", cloud.HostName(s.Client.Host))
	}
	res.Rev, res.Conflicts, res.Renamed = resp.Rev, resp.Conflicts, resp.RenamedIDs
	if !s.NoAttachments {
		s.im.download(ctx, result)
	}
	if err := s.writeBack(raw, false, wire, result, res); err != nil {
		return nil, err
	}
	return res, s.commitFirst(result, resp.Rev)
}

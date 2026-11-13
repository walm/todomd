package cloudsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/walm/todomd/internal/changes"
	"github.com/walm/todomd/internal/cloud"
	"github.com/walm/todomd/internal/markdown"
	"github.com/walm/todomd/internal/merge"
	"github.com/walm/todomd/internal/ops"
	"github.com/walm/todomd/internal/store"
	"github.com/walm/todomd/internal/task"
)

// ConflictMarkersError: the file holds git conflict markers, i.e. a git
// merge is half done. Sync refuses it until they're resolved.
type ConflictMarkersError struct {
	Path string
	Line int
}

func (e *ConflictMarkersError) Error() string {
	return fmt.Sprintf("%s:%d: git conflict markers; resolve the conflict in the file first", e.Path, e.Line)
}

// Syncer runs sync rounds for one linked file.
type Syncer struct {
	Path          string
	Store         *store.Store
	Client        *cloud.Client
	Link          *Link
	Version       string // this CLI's version, for full-file mode bookkeeping
	FormatVersion int    // the server's format version, from discovery
	// NoAttachments skips image uploads and downloads; image links then stay
	// URLs in the file.
	NoAttachments bool

	im *images // the current round's image handling
}

// Options for one round.
type Options struct {
	DryRun bool
}

// Result of a round.
type Result struct {
	Rev       int
	Unchanged bool // already up to date with nothing to push
	// Pushed are the local changes sent (base → this file); Pulled what the
	// round changed in the file (this file → what was written).
	Pushed    []changes.Event
	Pulled    []changes.Event
	Conflicts []merge.Conflict
	Renamed   []merge.Rename
	DryRun    bool
	ReadOnly  bool // local edits stay in the file but couldn't be pushed
	FullFile  bool // whole files were sent instead of ops
	Restored  bool // the file was missing and was written from the project
	Import    bool // the local change came with a git HEAD move (track mode)
	Note      string

	// PushedBoards and PulledBoards are the board-level ops (board.add,
	// board.delete, board.order, board.spelling) of the same two diffs. Task
	// events don't show a board added, reordered or renamed while empty, so
	// without these such a round would read as "up to date".
	PushedBoards []ops.Op
	PulledBoards []ops.Op

	// Attachments is what the round did with images.
	Attachments AttachmentReport
}

// startImages prepares the round's image handling.
func (s *Syncer) startImages(res *Result) error {
	im, err := newImages(s.Client, s.Link, &res.Attachments)
	if err != nil {
		return err
	}
	s.im = im
	return nil
}

// wireMode is how this round may turn local image paths into URLs.
func (s *Syncer) wireMode(dryRun bool) wireMode {
	switch {
	case s.NoAttachments:
		return wireKnown
	case dryRun:
		return wireDryRun
	}
	return wireOnline
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func diffEvents(a, b *task.File) []changes.Event {
	return changes.Diff(a, b)
}

// boardOps lists the board-level ops among those that take a to b.
func boardOps(a, b *task.File) []ops.Op {
	var out []ops.Op
	for _, op := range ops.Delta(a, b) {
		switch op.Op {
		case ops.BoardAdd, ops.BoardDelete, ops.BoardOrder, ops.BoardSpelling:
			out = append(out, op)
		}
	}
	return out
}

func (r *Result) setPushed(a, b *task.File) {
	r.Pushed, r.PushedBoards = diffEvents(a, b), boardOps(a, b)
}

func (r *Result) setPulled(a, b *task.File) {
	r.Pulled, r.PulledBoards = diffEvents(a, b), boardOps(a, b)
}

// fullFile reports whether this link currently sends whole files, clearing
// a full-file mode recorded for another CLI or format version.
func (s *Syncer) fullFile() bool {
	ff := s.Link.FullFile
	if ff == nil {
		return false
	}
	if ff.CLIVersion != s.Version || ff.FormatVersion != s.FormatVersion {
		s.Link.FullFile = nil
		return false
	}
	return true
}

func (s *Syncer) enterFullFile(reason string) {
	s.Link.FullFile = &FullFileMode{Reason: reason, CLIVersion: s.Version, FormatVersion: s.FormatVersion, Since: time.Now().UTC()}
}

// readLocal reads the file for a round: IDs persisted, no conflict markers,
// and strictly valid (what the server will accept). missing is true when
// the file doesn't exist.
func (s *Syncer) readLocal() (raw []byte, f *task.File, missing bool, err error) {
	raw, f, err = s.Store.ReadPersisted()
	if errors.Is(err, store.ErrNoFile) {
		return nil, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if line := markdown.ConflictMarkerLine(string(raw)); line > 0 {
		return nil, nil, false, &ConflictMarkersError{Path: s.Path, Line: line}
	}
	if _, err := markdown.ParseStrict(raw); err != nil {
		return nil, nil, false, fmt.Errorf("%s: %w", s.Path, err)
	}
	return raw, f, false, nil
}

// Round pushes local changes and pulls the project's, in one exchange.
func (s *Syncer) Round(ctx context.Context, opts Options) (*Result, error) {
	l := s.Link
	baseText, err := LoadBase(s.Path)
	if err != nil {
		return nil, err
	}
	base, err := markdown.Parse(baseText)
	if err != nil {
		return nil, fmt.Errorf("sync base: %w", err)
	}
	raw, local, missing, err := s.readLocal()
	if err != nil {
		return nil, err
	}
	res := &Result{DryRun: opts.DryRun, Restored: missing}
	if err := s.startImages(res); err != nil {
		return nil, err
	}
	fileForm := local
	if missing {
		// A missing file is never pushed as a deletion: pull and write it back.
		local, fileForm = base, base
	} else {
		// Everything from here on is in wire form: images linked by URL.
		local = s.im.toWire(ctx, local, s.im.refs(base), s.wireMode(opts.DryRun))
	}
	localText := markdown.Write(local)
	changed := !bytes.Equal(localText, baseText)

	head := ""
	if l.Git == GitTrack {
		head = GitHead(s.Path)
	}
	res.Import = changed && l.Git == GitTrack && l.GitHead != "" && head != "" && head != l.GitHead

	full := s.fullFile()
	resp, sent, err := s.exchange(ctx, base, baseText, local, localText, changed, full, res, opts)
	if err != nil {
		return nil, err
	}
	res.Rev = resp.Rev
	res.Conflicts = resp.Conflicts
	res.Renamed = resp.RenamedIDs

	if resp.Unchanged {
		res.Unchanged = true
		if missing && !opts.DryRun {
			if err := s.writeBack(nil, true, base, base, res); err != nil {
				return nil, err
			}
			res.Unchanged = false
		}
		if !opts.DryRun {
			s.recordOK(resp.Rev, head)
			if err := l.Save(s.Path); err != nil {
				return nil, err
			}
		}
		return res, nil
	}

	result, rev, err := s.result(ctx, resp, sent, res)
	if err != nil {
		return nil, err
	}
	res.Rev = rev
	if changed && !res.ReadOnly {
		res.setPushed(base, local)
	}
	if opts.DryRun {
		res.setPulled(fileForm, s.im.toLocal(result))
		return res, nil
	}
	if !s.NoAttachments {
		s.im.download(ctx, result)
	}

	mergeBase := local
	if res.ReadOnly {
		// The answer is relative to the base: keep the unpushed edits by
		// merging them onto the result.
		mergeBase = base
	}
	if err := s.writeBack(raw, missing, mergeBase, result, res); err != nil {
		return nil, err
	}
	if err := SaveBase(s.Path, markdown.Write(result)); err != nil {
		return nil, err
	}
	s.recordOK(rev, head)
	l.ReadOnly = res.ReadOnly
	if err := l.Save(s.Path); err != nil {
		return nil, err
	}
	return res, nil
}

// exchange sends the round's request and handles the answers that call for
// a different request: read_only (pull instead), base_mismatch and
// invalid_ops (full form, and full-file mode for a mismatch), base_gone
// (full form with the base). It returns the response and the model the
// response is relative to.
func (s *Syncer) exchange(ctx context.Context, base *task.File, baseText []byte, local *task.File, localText []byte,
	changed, full bool, res *Result, opts Options) (*cloud.SyncResponse, *task.File, error) {
	l := s.Link
	rev := l.BaseRev
	build := func(full, push bool) (cloud.SyncRequest, *task.File) {
		req := cloud.SyncRequest{
			RequestID: cloud.NewRequestID(),
			BaseRev:   &rev,
			Full:      full,
			DryRun:    opts.DryRun,
			Import:    push && res.Import,
		}
		if !full {
			// The full form is the fallback for a base the two sides disagree
			// on, so it doesn't send the checksum that would fail again.
			req.BaseSHA256 = sha(baseText)
		}
		sent := base
		if push {
			sent = local
			if full {
				c := string(localText)
				req.Content = &c
			} else {
				req.Ops = ops.Delta(base, local)
			}
		}
		if full {
			b := string(baseText)
			req.BaseContent = &b
		}
		return req, sent
	}

	push := changed
	if push && !full && s.FormatVersion > 0 && s.FormatVersion < markdown.LinksFormatVersion && carriesLinks(ops.Delta(base, local)) {
		// The server predates links: its ops reader would reject the links
		// field, but it keeps a links section in a whole file as
		// description text, byte for byte.
		full = true
	}
	if push && !full && s.FormatVersion > 0 && s.FormatVersion < markdown.StickersFormatVersion && (hasStickers(base) || hasStickers(local)) {
		// The server predates stickers: its ops reader would reject the
		// sticker field, and it holds a sticker line as the first line of
		// the card's description, which a description change in an op would
		// overwrite. It keeps the line in a whole file byte for byte.
		full = true
	}
	if push && !full && s.FormatVersion > 0 && s.FormatVersion < markdown.ReactionsFormatVersion && (hasReactions(base) || hasReactions(local)) {
		// The server predates reactions: its ops reader would reject the
		// reactions field, and it holds a reactions line as the last line of
		// the comment's text, which a comment.set would rewrite escaped. It
		// keeps the line in a whole file byte for byte.
		full = true
	}
	req, sent := build(full, push)
	res.FullFile = full
	resp, err := s.Client.Sync(ctx, l.ProjectID, req)
	switch {
	case err == nil:
		return resp, sent, nil
	case cloud.IsStatus(err, 403, "read_only") && push:
		res.ReadOnly = true
		req, sent = build(full, false)
	case cloud.IsStatus(err, 409, "base_mismatch") && !full:
		s.enterFullFile("base_mismatch")
		res.FullFile = true
		res.Note = "switched this link to full-file sync after a checksum mismatch"
		req, sent = build(true, push)
	case cloud.IsStatus(err, 410, "base_gone") && !full:
		res.FullFile = true
		req, sent = build(true, push)
	case cloud.IsStatus(err, 422, "invalid_ops") && !full:
		res.FullFile = true
		res.Note = "the server rejected this CLI's change list (" + err.Error() + "); sent the whole file instead. Please report this as a todomd bug"
		req, sent = build(true, push)
	default:
		return nil, nil, err
	}
	resp, err = s.Client.Sync(ctx, l.ProjectID, req)
	if err != nil {
		return nil, nil, err
	}
	return resp, sent, nil
}

// hasStickers reports whether any card in f has a sticker.
func hasStickers(f *task.File) bool {
	for _, t := range f.AllTasks() {
		if t.Sticker != "" {
			return true
		}
	}
	return false
}

// hasReactions reports whether any comment in f has reactions.
func hasReactions(f *task.File) bool {
	for _, t := range f.AllTasks() {
		for _, c := range t.Comments {
			if len(c.Reactions) > 0 {
				return true
			}
		}
	}
	return false
}

// carriesLinks reports whether a change list sets or adds any card links.
func carriesLinks(list []ops.Op) bool {
	for _, op := range list {
		if op.Set.Links != nil || (op.Op == ops.TaskAdd && op.Task != nil && len(op.Task.Links) > 0) {
			return true
		}
	}
	return false
}

// result turns a response into the merged model, verifying its checksum.
// A result that doesn't check out is replaced by GET /document, and the link
// switches to full-file mode.
func (s *Syncer) result(ctx context.Context, resp *cloud.SyncResponse, sent *task.File, res *Result) (*task.File, int, error) {
	var r *task.File
	var err error
	if resp.Content != nil {
		r, err = markdown.Parse([]byte(*resp.Content))
	} else {
		var list []ops.Op
		if list, err = resp.Ops(); err == nil {
			r, err = ops.Apply(sent, list)
		}
	}
	if err == nil && (resp.ContentSHA256 == "" || sha(markdown.Write(r)) == resp.ContentSHA256) {
		return r, resp.Rev, nil
	}
	reason := "checksum"
	if err != nil {
		reason = "unreadable result: " + err.Error()
	}
	if !res.DryRun {
		s.enterFullFile(reason)
		res.FullFile = true
		res.Note = "the server's result didn't match locally (" + reason + "); fetched the document and switched this link to full-file sync"
	}
	doc, derr := s.Client.Document(ctx, s.Link.ProjectID, 0)
	if derr != nil {
		return nil, 0, derr
	}
	r, err = markdown.Parse([]byte(doc.Content))
	if err != nil {
		return nil, 0, fmt.Errorf("document from %s: %w", cloud.HostName(s.Client.Host), err)
	}
	return r, doc.Rev, nil
}

// writeBack writes result into the file under the store lock. If the file
// changed since it was read (raw), or edits must be kept unpushed, the
// current file is merged onto the result first, with mergeBase as the base.
//
// mergeBase and result are in wire form; the file is written in local form,
// image URLs turned into paths for the images this machine holds.
func (s *Syncer) writeBack(raw []byte, missing bool, mergeBase, result *task.File, res *Result) error {
	if s.im == nil {
		if err := s.startImages(res); err != nil {
			return err
		}
	}
	im := s.im
	return s.Store.Locked(func() error {
		cur, err := os.ReadFile(s.Path)
		if errors.Is(err, os.ErrNotExist) {
			out := im.toLocal(result)
			res.setPulled(im.toLocal(mergeBase), out)
			return s.Store.Save(out)
		}
		if err != nil {
			return err
		}
		written := result
		var prev *task.File
		if missing || !bytes.Equal(cur, raw) || res.ReadOnly {
			c, err := markdown.Parse(cur)
			if err != nil {
				return fmt.Errorf("%s changed during sync and no longer parses: %w", s.Path, err)
			}
			// No network under the lock: convert what the base or the result
			// already link to, or what this machine holds. Anything else is
			// left for the next round, which pushes it properly.
			known := im.refs(mergeBase)
			for att, name := range im.refs(result) {
				if _, ok := known[att]; !ok {
					known[att] = name
				}
			}
			m, err := merge.Merge(mergeBase, im.toWire(context.Background(), c, known, wireOffline), result, merge.Options{})
			if err != nil {
				return err
			}
			written = m.Merged
			res.Conflicts = append(res.Conflicts, m.Conflicts...)
			res.Renamed = append(res.Renamed, m.Renamed...)
			prev = c
			if p, err := markdown.Parse(raw); err == nil && raw != nil {
				prev = p
			}
		} else if prev, err = markdown.Parse(cur); err != nil {
			return err
		}
		out := im.toLocal(written)
		res.setPulled(prev, out)
		if bytes.Equal(markdown.Write(out), cur) {
			return nil
		}
		return s.Store.Save(out)
	})
}

func (s *Syncer) recordOK(rev int, head string) {
	s.Link.BaseRev = rev
	if s.Link.Git == GitTrack && head != "" {
		s.Link.GitHead = head
	}
	s.Link.LastSync = &LastSync{At: time.Now().UTC(), Rev: rev, OK: true}
}

// RecordError notes a failed round in the link (best effort).
func (s *Syncer) RecordError(err error) {
	prev := 0
	if s.Link.LastSync != nil {
		prev = s.Link.LastSync.Rev
	}
	s.Link.LastSync = &LastSync{At: time.Now().UTC(), Rev: prev, OK: false, Error: err.Error()}
	_ = s.Link.Save(s.Path)
}

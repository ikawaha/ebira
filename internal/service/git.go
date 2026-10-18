package service

import (
	"context"
	"errors"
	"path"
	"strings"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/gitsync"
	"github.com/ikawaha/ebira/internal/store"
)

// GitStatus tells whether the notes are in a git repository.
func (s *Notes) GitStatus(ctx context.Context) (*gennotes.GitStatus, error) {
	if !fromPerson(ctx) {
		return &gennotes.GitStatus{}, nil
	}
	repo, err := gitsync.Open(ctx, s.store.Dir())
	if err != nil {
		return &gennotes.GitStatus{}, nil
	}
	res := &gennotes.GitStatus{Repository: true}
	if u := repo.Upstream(ctx); u != "" {
		res.Upstream = &u
	}
	return res, nil
}

// GitSync syncs the notes with their git repository (see gitsync), first
// connecting them to the remote repository p.Remote if given, and updates the
// index. Writes of notes wait while git changes the files.
func (s *Notes) GitSync(ctx context.Context, p *gennotes.GitSyncPayload) (*gennotes.GitSyncResult, error) {
	if !fromPerson(ctx) {
		return nil, gennotes.MakeForbidden(errors.New("only people can sync with git"))
	}
	s.gitMu.Lock()
	defer s.gitMu.Unlock()
	label := "conflict " + s.now().Local().Format("2006-01-02 15:04")
	locked := func(f func() error) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return f()
	}
	resolve := func(p string, theirs []byte) (string, error) {
		return s.store.ConflictCopy(p, theirs, label)
	}
	var r *gitsync.Result
	var gitErr error
	if remote := deref(p.Remote); remote != "" {
		r, gitErr = gitsync.Connect(ctx, s.store.Dir(), remote, locked, store.IsNotePath, resolve)
	} else if repo, err := gitsync.Open(ctx, s.store.Dir()); err != nil {
		return nil, gennotes.MakeBadRequest(err)
	} else {
		r, gitErr = repo.Sync(ctx, locked, store.IsNotePath, resolve)
	}
	// Update the index even after an error, as files may have changed.
	s.mu.Lock()
	synced, err := s.sync(ctx)
	s.mu.Unlock()
	if gitErr != nil {
		return nil, gennotes.MakeBadRequest(gitErr)
	}
	if err != nil {
		return nil, err
	}
	res := &gennotes.GitSyncResult{
		Committed: r.Committed,
		Merged:    r.Merged,
		Pushed:    r.Pushed,
		Conflicts: make([]*gennotes.GitConflict, 0, len(r.Conflicts)),
		Errors:    synced.Errors,
	}
	if r.Upstream != "" {
		res.Upstream = &r.Upstream
	}
	for _, c := range r.Conflicts {
		gc := &gennotes.GitConflict{Path: c.Path}
		if store.IsNotePath(c.Path) {
			gc.NoteID = noteID(c.Path)
		}
		if c.Copy != "" {
			gc.CopyID = noteID(c.Copy)
		}
		res.Conflicts = append(res.Conflicts, gc)
	}
	return res, nil
}

// noteID returns the ID of a note by the path of its file.
func noteID(p string) *string {
	id := strings.TrimSuffix(path.Base(p), store.Ext)
	return &id
}

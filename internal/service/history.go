package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/history"
	"github.com/ikawaha/ebira/internal/index"
)

// SetHistory keeps the earlier versions of notes in h: the index reports each
// note whose content it replaces. Call it before the service is used.
func (s *Notes) SetHistory(h *history.Store) {
	s.history = h
	s.index.OnReplace(func(r index.Replaced) {
		v := history.Version{Title: r.Title, Tags: r.Tags, Body: r.Body, UpdatedAt: r.UpdatedAt}
		if err := h.Add(r.ID, v); err != nil {
			slog.Warn("cannot keep the earlier version of a note", "note", r.ID, "error", err)
		}
	})
}

// SweepHistory drops the versions of the notes that no longer exist, such as
// those deleted while the index was rebuilt, and those of the other notes
// beyond the limits of the history (see history.Store.Sweep).
func (s *Notes) SweepHistory() error {
	if s.history == nil {
		return nil
	}
	files, err := s.store.Scan()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, f := range files {
		ids[f.ID] = true
	}
	// A note made after the scan is looked up again before its versions go.
	return s.history.Sweep(func(id string) bool {
		if ids[id] {
			return true
		}
		_, err := s.store.Stat(id)
		return err == nil
	})
}

// forgetHistory drops the versions of notes deleted for good.
func (s *Notes) forgetHistory(ids ...string) {
	if s.history == nil {
		return
	}
	for _, id := range ids {
		if err := s.history.Remove(id); err != nil {
			slog.Warn("cannot remove the earlier versions of a note", "note", id, "error", err)
		}
	}
}

// History lists the earlier versions of a note. Only people see them: the
// versions of a note made private later would otherwise show its content to
// AI clients.
func (s *Notes) History(ctx context.Context, p *gennotes.HistoryPayload) ([]*gennotes.NoteVersionSummary, error) {
	if !fromPerson(ctx) {
		return nil, gennotes.MakeNotFound(errors.New("no such note"))
	}
	if err := requireVerified(ctx); err != nil {
		return nil, err
	}
	if _, err := s.store.Stat(p.ID); err != nil {
		return nil, mapErr(err)
	}
	res := []*gennotes.NoteVersionSummary{}
	if s.history == nil {
		return res, nil
	}
	vs, err := s.history.List(p.ID)
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		res = append(res, &gennotes.NoteVersionSummary{Rev: v.Rev, Title: v.Title, UpdatedAt: v.UpdatedAt.UTC().Format(time.RFC3339), ReplacedAt: v.ReplacedAt.UTC().Format(time.RFC3339)})
	}
	return res, nil
}

// HistoryVersion returns an earlier version of a note.
func (s *Notes) HistoryVersion(ctx context.Context, p *gennotes.HistoryVersionPayload) (*gennotes.NoteVersion, error) {
	if !fromPerson(ctx) {
		return nil, gennotes.MakeNotFound(errors.New("no such note"))
	}
	if err := requireVerified(ctx); err != nil {
		return nil, err
	}
	if _, err := s.store.Stat(p.ID); err != nil {
		return nil, mapErr(err)
	}
	if s.history == nil {
		return nil, gennotes.MakeNotFound(history.ErrNotFound)
	}
	v, err := s.history.Get(p.ID, p.Rev)
	if errors.Is(err, history.ErrNotFound) {
		return nil, gennotes.MakeNotFound(err)
	}
	if err != nil {
		return nil, err
	}
	return &gennotes.NoteVersion{Rev: v.Rev, Title: v.Title, Tags: v.Tags, Body: v.Body, UpdatedAt: v.UpdatedAt.UTC().Format(time.RFC3339), ReplacedAt: v.ReplacedAt.UTC().Format(time.RFC3339)}, nil
}

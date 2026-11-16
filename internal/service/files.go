package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/history"
	"github.com/ikawaha/ebira/internal/store"
)

// The files of notes, images and other attachments, are for people, but for
// AI clients adding files to notes in notebooks of the collab kind (AddAiFile) and
// sending them with a new note (ImportAiNote), in smaller sizes: a request
// without the mark of people finds no files.

var errFilesForPeople = errors.New("the files of notes are not available to AI clients")

// Limits of the files of AI clients, which come base64-encoded in one
// request: the size of one, and the number and the size in all of the images
// sent with a note.
const (
	aiMaxFileSize  = 5 << 20
	aiMaxFiles     = 10
	aiMaxTotalSize = 20 << 20
)

// AddAiFile adds a file to a note in a notebook of the collab kind for an AI
// client. A note hidden from AI clients is not found.
func (s *Notes) AddAiFile(ctx context.Context, p *gennotes.AddAiFilePayload) (*gennotes.NoteFile, error) {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return nil, err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return nil, err
	}
	if len(p.Content) > aiMaxFileSize {
		return nil, gennotes.MakeTooLarge(fmt.Errorf("the file is larger than %d MB", aiMaxFileSize>>20))
	}
	var keep func(string) error
	if p.Replace != nil && *p.Replace {
		keep = s.keepFile(p.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name, replaced, err := s.store.PutFile(p.ID, p.Name, bytes.NewReader(p.Content), keep)
	if err != nil {
		return nil, mapFileErr(err)
	}
	return noteFile(p.ID, name, replaced), nil
}

// noteFile returns the file name of note id for the API.
func noteFile(id, name string, replaced bool) *gennotes.NoteFile {
	res := &gennotes.NoteFile{Name: name, Path: store.FilePath(id, name)}
	if replaced {
		res.Replaced = ptr(true)
	}
	return res
}

// keepFile returns the function that keeps a file of note id, replaced or
// deleted, in the history, for people to put back (see history.KeepFile). It
// keeps nothing without a history, as in tests.
func (s *Notes) keepFile(id string) func(path string) error {
	return func(path string) error {
		if s.history == nil {
			return nil
		}
		return s.history.KeepFile(id, filepath.Base(path), path)
	}
}

// DeleteAiFile deletes a file of a note in a notebook of the collab kind for
// an AI client, keeping it for people to put back. A note hidden from AI
// clients is not found.
func (s *Notes) DeleteAiFile(ctx context.Context, p *gennotes.DeleteAiFilePayload) error {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return mapFileErr(s.store.DeleteFile(p.ID, p.Name, s.keepFile(p.ID)))
}

// AddFile adds a file to a note (see store.AddFile).
func (s *Notes) AddFile(ctx context.Context, p *gennotes.AddFilePayload, body io.ReadCloser) (*gennotes.NoteFile, error) {
	defer body.Close()
	if !fromPerson(ctx) {
		return nil, gennotes.MakeNotFound(errFilesForPeople)
	}
	// One byte more than allowed tells a file that is too large.
	b, err := io.ReadAll(io.LimitReader(body, store.MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name, err := s.store.AddFile(p.ID, p.Name, b)
	if err != nil {
		return nil, mapFileErr(err)
	}
	return noteFile(p.ID, name, false), nil
}

// GetFile returns a file of a note. Images, told by their content, are served
// as their type, and PDF files too, for the viewer of the browser, which does
// not run their scripts in the origin of the web UI. Other files, which might
// run scripts there if shown, such as HTML or SVG files, are served as
// downloads of no particular type.
func (s *Notes) GetFile(ctx context.Context, p *gennotes.GetFilePayload) (*gennotes.GetFileResult, io.ReadCloser, error) {
	if !fromPerson(ctx) {
		return nil, nil, gennotes.MakeNotFound(errFilesForPeople)
	}
	path, err := s.store.File(p.ID, p.Name)
	if err != nil {
		return nil, nil, mapFileErr(err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, mapFileErr(store.ErrFileNotFound)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		f.Close()
		return nil, nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, err
	}
	ct, image := store.FileType(head[:n])
	disposition := "inline"
	if !image && ct != "application/pdf" {
		ct, disposition = "application/octet-stream", "attachment"
	}
	return &gennotes.GetFileResult{
		ContentType: ct,
		Length:      fi.Size(),
		Disposition: mime.FormatMediaType(disposition, map[string]string{"filename": p.Name}),
	}, f, nil
}

// DeleteFile deletes a file of a note, keeping it to be put back.
func (s *Notes) DeleteFile(ctx context.Context, p *gennotes.DeleteFilePayload) error {
	if !fromPerson(ctx) {
		return gennotes.MakeNotFound(errFilesForPeople)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return mapFileErr(s.store.DeleteFile(p.ID, p.Name, s.keepFile(p.ID)))
}

// FileHistory lists the earlier files of a note, replaced or deleted. Only
// people see them, as the history of notes.
func (s *Notes) FileHistory(ctx context.Context, p *gennotes.FileHistoryPayload) ([]*gennotes.FileVersion, error) {
	if !fromPerson(ctx) {
		return nil, gennotes.MakeNotFound(errFilesForPeople)
	}
	if err := requireVerified(ctx); err != nil {
		return nil, err
	}
	if _, err := s.store.Stat(p.ID); err != nil {
		return nil, mapErr(err)
	}
	res := []*gennotes.FileVersion{}
	if s.history == nil {
		return res, nil
	}
	fs, err := s.history.Files(p.ID)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		_, err := s.store.File(p.ID, f.Name)
		res = append(res, &gennotes.FileVersion{Rev: f.Rev, Name: f.Name, Size: f.Size, ReplacedAt: store.FormatTime(f.ReplacedAt), Current: err == nil})
	}
	return res, nil
}

// RestoreFile puts an earlier file of a note back under its name, keeping
// the file of that name now in its place.
func (s *Notes) RestoreFile(ctx context.Context, p *gennotes.RestoreFilePayload) (*gennotes.NoteFile, error) {
	if !fromPerson(ctx) {
		return nil, gennotes.MakeNotFound(errFilesForPeople)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	var res *gennotes.NoteFile
	err := s.history.TakeFile(p.ID, p.Rev, func(f *history.File, path string) error {
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		name, replaced, err := s.store.PutFile(p.ID, f.Name, in, s.keepFile(p.ID))
		if err != nil {
			return err
		}
		res = noteFile(p.ID, name, replaced)
		return nil
	})
	if errors.Is(err, history.ErrNotFound) {
		return nil, gennotes.MakeNotFound(err)
	}
	if err != nil {
		return nil, mapFileErr(err)
	}
	return res, nil
}

// recentFile is how long a file counts as recently added, when the note it
// was added to may still be being edited.
const recentFile = time.Hour

// CheckFiles checks the files of notes against the note bodies, including
// those in the trash, and marks the unused files that earlier versions of
// notes refer to. AI clients get empty lists.
func (s *Notes) CheckFiles(ctx context.Context, _ *gennotes.CheckFilesPayload) (*gennotes.FileCheck, error) {
	res := &gennotes.FileCheck{Unused: []*gennotes.UnusedFile{}, Broken: []*gennotes.BrokenFileRef{}}
	if !fromPerson(ctx) {
		return res, nil
	}
	files, err := s.store.AllFiles()
	if err != nil {
		return nil, err
	}
	exists := make(map[store.FileRef]bool, len(files))
	for _, f := range files {
		exists[store.FileRef{NoteID: f.NoteID, Name: f.Name}] = true
	}
	used := map[store.FileRef]bool{}
	titles := map[string]string{}
	err = s.index.Bodies(ctx, func(id, title, body string) error {
		titles[id] = title
		for _, r := range store.FileRefs(body) {
			used[r] = true
			if !exists[r] {
				res.Broken = append(res.Broken, &gennotes.BrokenFileRef{NoteID: id, Title: title, Path: store.FilePath(r.NoteID, r.Name)})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	inHistory, err := s.historyFileRefs()
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, f := range files {
		r := store.FileRef{NoteID: f.NoteID, Name: f.Name}
		if used[r] {
			continue
		}
		res.Unused = append(res.Unused, &gennotes.UnusedFile{
			NoteID:    f.NoteID,
			Title:     titles[f.NoteID],
			Name:      f.Name,
			Path:      store.FilePath(f.NoteID, f.Name),
			Size:      f.Size,
			AddedAt:   f.ModTime.UTC().Format(time.RFC3339),
			Recent:    now.Sub(f.ModTime) < recentFile,
			InHistory: inHistory[r],
		})
	}
	slices.SortFunc(res.Broken, func(a, b *gennotes.BrokenFileRef) int {
		return cmp.Or(cmp.Compare(a.NoteID, b.NoteID), cmp.Compare(a.Path, b.Path))
	})
	return res, nil
}

// historyFileRefs returns the files the earlier versions of notes refer to.
func (s *Notes) historyFileRefs() (map[store.FileRef]bool, error) {
	refs := map[store.FileRef]bool{}
	if s.history == nil {
		return refs, nil
	}
	err := s.history.Each(func(_ string, v *history.Version) error {
		for _, r := range store.FileRefs(v.Body) {
			refs[r] = true
		}
		return nil
	})
	return refs, err
}

func mapFileErr(err error) error {
	switch {
	case errors.Is(err, store.ErrFileNotFound):
		return gennotes.MakeNotFound(err)
	case errors.Is(err, store.ErrFileTooLarge):
		return gennotes.MakeTooLarge(err)
	}
	return mapErr(err)
}

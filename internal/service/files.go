package service

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// The files of notes, such as images, are for people only for now: AI
// clients neither see nor add them, so a request without the mark of people
// finds no files.

var errFilesForPeople = errors.New("the files of notes are not available to AI clients")

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
	return &gennotes.NoteFile{Name: name, Path: store.FilePath(p.ID, name)}, nil
}

// GetFile returns a file of a note. Only files of the accepted types are
// served, told by their content, so that a file put in the directory by hand,
// such as an HTML or SVG file, is not served from the origin of the web UI.
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
	ct, ok := store.FileType(head[:n])
	if !ok {
		f.Close()
		return nil, nil, gennotes.MakeNotFound(store.ErrUnsupportedFile)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, err
	}
	return &gennotes.GetFileResult{ContentType: ct, Length: fi.Size()}, f, nil
}

// DeleteFile deletes a file of a note.
func (s *Notes) DeleteFile(ctx context.Context, p *gennotes.DeleteFilePayload) error {
	if !fromPerson(ctx) {
		return gennotes.MakeNotFound(errFilesForPeople)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return mapFileErr(s.store.DeleteFile(p.ID, p.Name))
}

// recentFile is how long a file counts as recently added, when the note it
// was added to may still be being edited.
const recentFile = time.Hour

// CheckFiles checks the files of notes against the note bodies, including
// those in the trash. AI clients get empty lists.
func (s *Notes) CheckFiles(ctx context.Context) (*gennotes.FileCheck, error) {
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
	now := s.now()
	for _, f := range files {
		if used[store.FileRef{NoteID: f.NoteID, Name: f.Name}] {
			continue
		}
		res.Unused = append(res.Unused, &gennotes.UnusedFile{
			NoteID:  f.NoteID,
			Title:   titles[f.NoteID],
			Name:    f.Name,
			Path:    store.FilePath(f.NoteID, f.Name),
			Size:    f.Size,
			AddedAt: f.ModTime.UTC().Format(time.RFC3339),
			Recent:  now.Sub(f.ModTime) < recentFile,
		})
	}
	slices.SortFunc(res.Broken, func(a, b *gennotes.BrokenFileRef) int {
		return cmp.Or(cmp.Compare(a.NoteID, b.NoteID), cmp.Compare(a.Path, b.Path))
	})
	return res, nil
}

func mapFileErr(err error) error {
	switch {
	case errors.Is(err, store.ErrFileNotFound):
		return gennotes.MakeNotFound(err)
	case errors.Is(err, store.ErrUnsupportedFile):
		return gennotes.MakeBadRequest(err)
	case errors.Is(err, store.ErrFileTooLarge):
		return gennotes.MakeTooLarge(err)
	}
	return mapErr(err)
}

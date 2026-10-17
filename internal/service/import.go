package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/importer"
	"github.com/ikawaha/ebira/internal/store"
)

// Import statuses of ImportResult.
const (
	importCreated = "created"
	importSkipped = "skipped"
)

// importArgs are the arguments of an import, for people and AI clients.
type importArgs struct {
	notebook, name, content string
	modTime                 time.Time
	files                   []*gennotes.ImportFile
	force                   bool
}

// Import imports a Markdown file as a new note, for people.
func (s *Notes) Import(ctx context.Context, p *gennotes.ImportPayload) (*gennotes.ImportResult, error) {
	a := importArgs{notebook: deref(p.Notebook), name: p.Name, content: p.Content, files: p.Files, force: p.Force != nil && *p.Force}
	if p.ModifiedAt != nil {
		t, err := time.Parse(time.RFC3339, *p.ModifiedAt)
		if err != nil {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("modified_at: %w", err))
		}
		a.modTime = t
	}
	return s.importNote(ctx, a, "")
}

// ImportAiNote imports a Markdown file for an AI client, into a notebook of
// the collab or drop kind as CreateAiNote does. The ID is always random.
func (s *Notes) ImportAiNote(ctx context.Context, p *gennotes.ImportAiNotePayload) (*gennotes.ImportResult, error) {
	if err := s.requireKind(p.Notebook, store.KindCollab, store.KindDrop); err != nil {
		return nil, err
	}
	return s.importNote(ctx, importArgs{notebook: p.Notebook, name: p.Name, content: p.Content}, store.CreatedByAI)
}

// importNote converts the file with the importer and creates the note, with
// the images given. A file imported before is skipped unless forced; for AI
// clients only the notebooks they can see count, so that the result does not
// tell them about notes they cannot see.
func (s *Notes) importNote(ctx context.Context, a importArgs, createdBy string) (*gennotes.ImportResult, error) {
	nb := a.notebook
	if nb == "" {
		nb = store.Inbox
	}
	if nb == store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("cannot import a note into the trash"))
	}
	if !store.ValidNotebook(nb) {
		return nil, gennotes.MakeBadRequest(store.ErrInvalidNotebook)
	}
	n, err := importer.Parse(importer.Input{Name: a.name, Content: a.content, ModTime: a.modTime, Now: s.now()})
	if errors.Is(err, importer.ErrTooLarge) {
		return nil, gennotes.MakeBadRequest(err)
	}
	if err != nil {
		return nil, err
	}
	res := &gennotes.ImportResult{Warnings: n.Warnings}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !a.force {
		hidden, err := s.hiddenNotebooks(ctx)
		if err != nil {
			return nil, err
		}
		id, ok, err := s.index.ImportedNote(ctx, n.Hash, hidden...)
		if err != nil {
			return nil, err
		}
		if ok {
			res.Status, res.DuplicateOf = importSkipped, &id
			return res, nil
		}
	}

	note := &store.Note{
		ID:           store.NewID(),
		Notebook:     nb,
		Title:        n.Title,
		Tags:         n.Tags,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		Body:         n.Body,
		CreatedBy:    createdBy,
		ImportedFrom: path.Base(strings.ReplaceAll(a.name, `\`, "/")),
		ImportHash:   n.Hash,
		Extra:        n.Extra,
	}
	if err := s.store.Create(note); err != nil {
		return nil, mapErr(err)
	}

	// The images go next to the note, and the body refers to them there.
	paths := map[string]string{}
	for _, f := range a.files {
		if !slices.Contains(n.Images, f.Ref) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("the file does not refer to the image %s; it was left out", f.Ref))
			continue
		}
		if _, ok := paths[f.Ref]; ok {
			continue
		}
		name, err := s.store.AddFile(note.ID, f.Name, f.Content)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("the image %s was left out: %v", f.Ref, err))
			continue
		}
		paths[f.Ref] = store.FilePath(note.ID, name)
	}
	var missing []string
	for _, ref := range n.Images {
		if _, ok := paths[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		res.Warnings = append(res.Warnings, "images not imported, whose links are left as they are: "+strings.Join(missing, ", "))
	}
	if len(paths) > 0 {
		note.Body = importer.RewriteImages(note.Body, paths)
		if err := s.store.Save(note); err != nil {
			return nil, mapErr(err)
		}
	}
	if err := s.reindex(ctx, note); err != nil {
		return nil, err
	}
	s.events.publish(eventCreated, note.ID)
	res.Status, res.Note = importCreated, toNote(note)
	return res, nil
}

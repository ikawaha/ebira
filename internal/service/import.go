package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/importer"
	"github.com/ikawaha/ebira/internal/store"
	goa "goa.design/goa/v3/pkg"
)

// Import statuses of ImportResult.
const (
	importCreated = "created"
	importSkipped = "skipped"
)

// importMaxImages limits the images of an import by people in all. They are
// streamed to disk, so the limit only keeps a wrong request from filling it.
// A variable for tests.
var importMaxImages int64 = 1 << 30

// importArgs are the arguments of an import, for people and AI clients.
type importArgs struct {
	notebook, name, content string
	modTime                 time.Time
	files                   nextFile
	force                   bool
}

// importFile is an image sent with a Markdown file to import: its path as the
// file refers to it, its name and its content.
type importFile struct {
	ref, name string
	content   io.Reader
}

// nextFile returns the next image of an import, or io.EOF itself after the
// last.
// The content of an image is read before the next one is asked for.
type nextFile func() (*importFile, error)

// filesOf returns the images given whole as a nextFile.
func filesOf[F any](files []F, f func(F) (ref, name string, content []byte)) nextFile {
	i := 0
	return func() (*importFile, error) {
		if i == len(files) {
			return nil, io.EOF
		}
		ref, name, content := f(files[i])
		i++
		return &importFile{ref: ref, name: name, content: bytes.NewReader(content)}, nil
	}
}

// Import imports a Markdown file as a new note, for people, from a body of
// multipart/form-data, whose images are streamed to disk, or of JSON, the
// form of earlier versions, with the images base64-encoded.
func (s *Notes) Import(ctx context.Context, p *gennotes.ImportPayload, body io.ReadCloser) (*gennotes.ImportResult, error) {
	defer body.Close()
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	a := importArgs{notebook: deref(p.Notebook)}
	var f importFields
	mt, params, _ := mime.ParseMediaType(deref(p.ContentType))
	var err error
	if mt == "multipart/form-data" {
		f, err = readImportForm(multipart.NewReader(body, params["boundary"]), &a)
	} else {
		f, err = readImportJSON(body, &a)
	}
	if err != nil {
		return nil, err
	}
	// The query gives force over the body, false as well as true.
	if p.Force != nil {
		a.force = *p.Force
	}
	if !f.content {
		return nil, gennotes.MakeBadRequest(errors.New("content: the Markdown file is missing"))
	}
	modTime := f.modTime
	if modTime != "" {
		t, err := time.Parse(time.RFC3339, modTime)
		if err != nil {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("modified_at: %w", err))
		}
		a.modTime = t
	}
	if a.name == "" || utf8.RuneCountInString(a.name) > 255 {
		return nil, gennotes.MakeBadRequest(errors.New("name: the file name must be 1 to 255 characters long"))
	}
	return s.importNote(ctx, a, "")
}

// importFields are the fields of a body of import that importArgs does not
// hold: the modification time of the file as sent, and whether the body has
// the content, which may be empty, of the file.
type importFields struct {
	modTime string
	content bool
}

// nextPart returns the next part of mr, or io.EOF itself after the last
// one. A body that ends before its closing boundary, such as a request cut
// off, is an error: the reader wraps io.EOF then, which must not pass for the
// end.
func nextPart(mr *multipart.Reader) (*multipart.Part, error) {
	p, err := mr.NextPart()
	if err == io.EOF {
		return nil, err
	}
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("the request ended before its closing boundary: %w", io.ErrUnexpectedEOF)
	}
	return p, err
}

// readImportForm reads the fields of the Markdown file from a body of
// multipart/form-data into a, which reads the images from the rest of the
// body. The fields name, content and modified_at come first; each image is a
// field ref and a file field after it.
func readImportForm(mr *multipart.Reader, a *importArgs) (importFields, error) {
	var f importFields
	var part *multipart.Part
	for {
		var err error
		part, err = nextPart(mr)
		if err == io.EOF {
			break
		}
		if err != nil {
			return f, gennotes.MakeBadRequest(fmt.Errorf("the body is not multipart/form-data: %w", err))
		}
		name := part.FormName()
		if name == "ref" {
			break
		}
		v, err := formValue(part, importer.MaxSize)
		if err != nil {
			return f, err
		}
		switch name {
		case "name":
			a.name = v
		case "content":
			a.content, f.content = v, true
		case "modified_at":
			f.modTime = v
		}
	}
	// part is the field ref of the first image, if any; the parts after it
	// are read when the images are asked for, after the content of the one
	// before has been read.
	started := false
	a.files = func() (*importFile, error) {
		if started {
			next, err := nextPart(mr)
			if err != nil {
				return nil, err
			}
			part = next
		}
		started = true
		if part == nil {
			return nil, io.EOF
		}
		if part.FormName() != "ref" {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("the field %q comes where a field ref of an image is expected", part.FormName()))
		}
		ref, err := formValue(part, 4096)
		if err != nil {
			return nil, err
		}
		file, err := nextPart(mr)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if err != nil || file.FormName() != "file" {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("the image %s has no field file after its ref", ref))
		}
		return &importFile{ref: ref, name: fileName(file), content: file}, nil
	}
	return f, nil
}

// fileName returns the file name of a file field as sent: Part.FileName keeps
// only its base name, which is all that is wanted, but drops a name of a
// path that ends with a slash.
func fileName(p *multipart.Part) string {
	if name := p.FileName(); name != "" {
		return name
	}
	return "image"
}

// formValue reads a text field of at most limit bytes.
func formValue(p *multipart.Part, limit int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(p, limit+1))
	if err != nil {
		return "", readErr(err)
	}
	if int64(len(b)) > limit {
		if p.FormName() == "content" {
			return "", gennotes.MakeBadRequest(importer.ErrTooLarge)
		}
		return "", gennotes.MakeBadRequest(fmt.Errorf("the field %s is too long", p.FormName()))
	}
	return string(b), nil
}

// importJSON is a JSON body of import, as earlier versions send.
type importJSON struct {
	Notebook   *string          `json:"notebook"`
	Name       string           `json:"name"`
	Content    *string          `json:"content"`
	ModifiedAt string           `json:"modified_at"`
	Force      *bool            `json:"force"`
	Files      []importJSONFile `json:"files"`
}

// importJSONFile is an image in a JSON body of import.
type importJSONFile struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Content []byte `json:"content"` // base64-encoded
}

// readImportJSON reads a JSON body into a. The notebook and force in the
// body count unless the query gives them.
func readImportJSON(body io.Reader, a *importArgs) (importFields, error) {
	var v importJSON
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		return importFields{}, gennotes.MakeBadRequest(fmt.Errorf("the body is not JSON: %w", err))
	}
	a.name, a.content = v.Name, deref(v.Content)
	if a.notebook == "" {
		a.notebook = deref(v.Notebook)
	}
	a.force = v.Force != nil && *v.Force
	a.files = filesOf(v.Files, func(f importJSONFile) (string, string, []byte) { return f.Ref, f.Name, f.Content })
	return importFields{modTime: v.ModifiedAt, content: v.Content != nil}, nil
}

// ImportAiNote imports a Markdown file for an AI client, into a notebook of
// the collab or drop kind as CreateAiNote does. The ID is always random.
func (s *Notes) ImportAiNote(ctx context.Context, p *gennotes.ImportAiNotePayload) (*gennotes.ImportResult, error) {
	if err := s.requireKind(p.Notebook, store.KindCollab, store.KindDrop); err != nil {
		return nil, err
	}
	// The images of AI clients are limited more than people's, as they come
	// base64-encoded in one request.
	if len(p.Files) > aiMaxFiles {
		return nil, gennotes.MakeBadRequest(fmt.Errorf("at most %d images can be sent with a note", aiMaxFiles))
	}
	total := 0
	for _, f := range p.Files {
		if len(f.Content) > aiMaxFileSize {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("the image %s is larger than %d MB", f.Ref, aiMaxFileSize>>20))
		}
		if total += len(f.Content); total > aiMaxTotalSize {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("the images are larger than %d MB in all", aiMaxTotalSize>>20))
		}
	}
	files := filesOf(p.Files, func(f *gennotes.AIImportFile) (string, string, []byte) { return f.Ref, f.Name, f.Content })
	return s.importNote(ctx, importArgs{notebook: p.Notebook, name: p.Name, content: p.Content, files: files}, store.CreatedByAI)
}

// importNote converts the file with the importer and creates the note, with
// the images given. A file imported before is skipped unless forced; for AI
// clients only the notebooks they can see count, so that the result does not
// tell them about notes they cannot see. The images are received into a stage
// first, outside the lock, and become the note's files when the note is
// created, so that a request cut off imports nothing.
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
	warnings := append([]string{}, n.Warnings...)
	skipped := func(dup *string) *gennotes.ImportResult {
		return &gennotes.ImportResult{Status: importSkipped, DuplicateOf: dup, Warnings: warnings}
	}
	// A file imported before is told before its images are received; they
	// are read through, so that the client gets the answer after sending.
	if dup, err := s.imported(ctx, a.force, n.Hash); err != nil || dup != nil {
		if err != nil {
			return nil, err
		}
		if err := drain(a.files); err != nil {
			return nil, err
		}
		return skipped(dup), nil
	}

	id := store.NewID()
	stage, err := s.store.NewStage(nb)
	if err != nil {
		return nil, err
	}
	defer stage.Remove()
	paths, imageWarnings, err := receiveImages(stage, id, n.Images, a.files)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another request may have imported the file meanwhile.
	if dup, err := s.imported(ctx, a.force, n.Hash); err != nil || dup != nil {
		if err != nil {
			return nil, err
		}
		return skipped(dup), nil
	}
	note := &store.Note{
		ID:           id,
		Notebook:     nb,
		Title:        n.Title,
		Tags:         n.Tags,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		Body:         importer.RewriteImages(n.Body, paths),
		CreatedBy:    createdBy,
		ImportedFrom: path.Base(strings.ReplaceAll(a.name, `\`, "/")),
		ImportHash:   n.Hash,
		Extra:        n.Extra,
	}
	if err := s.store.Create(note); err != nil {
		return nil, mapErr(err)
	}
	if err := stage.Adopt(note.ID); err != nil {
		s.store.Delete(note.ID) // nothing is imported without its images
		return nil, err
	}
	if err := s.reindex(ctx, note); err != nil {
		return nil, err
	}
	s.events.publish(eventCreated, note.ID)
	return &gennotes.ImportResult{Status: importCreated, Note: toNote(note), Warnings: append(warnings, imageWarnings...)}, nil
}

// drain reads the images of an import through, without keeping them.
func drain(next nextFile) error {
	for next != nil {
		f, err := next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return readErr(err)
		}
		if _, err := io.Copy(io.Discard, f.content); err != nil {
			return readErr(err)
		}
	}
	return nil
}

// imported returns the note a file with the content hash was imported as,
// or nil if none or forced.
func (s *Notes) imported(ctx context.Context, force bool, hash string) (*string, error) {
	if force {
		return nil, nil
	}
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	id, ok, err := s.index.ImportedNote(ctx, hash, hidden...)
	if err != nil || !ok {
		return nil, err
	}
	return &id, nil
}

// receiveImages receives the images of an import into stage, the images that
// the file refers to as refs, and returns the paths of those received, by
// which the body of note id refers to them, with warnings about the others.
// An image that cannot be added is left out with a warning, but an error of
// reading the request, such as when it is cut off, or images past
// importMaxImages fail the import.
func receiveImages(stage *store.Stage, id string, refs []string, next nextFile) (map[string]string, []string, error) {
	paths := map[string]string{}
	var warnings []string
	if next == nil {
		next = func() (*importFile, error) { return nil, io.EOF }
	}
	var total int64
	for {
		f, err := next()
		if err == io.EOF { // itself, not a body cut off (see nextPart)
			break
		}
		if err != nil {
			return nil, nil, readErr(err)
		}
		if !slices.Contains(refs, f.ref) {
			warnings = append(warnings, fmt.Sprintf("the file does not refer to the image %s; it was left out", f.ref))
			continue
		}
		if _, ok := paths[f.ref]; ok {
			continue
		}
		r := &countingReader{r: f.content}
		name, err := stage.Add(f.name, r)
		total += r.n
		if total > importMaxImages {
			return nil, nil, gennotes.MakeBadRequest(fmt.Errorf("the images are larger than %d MB in all", importMaxImages>>20))
		}
		if r.err != nil {
			return nil, nil, readErr(r.err)
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("the image %s was left out: %v", f.ref, err))
			continue
		}
		paths[f.ref] = store.FilePath(id, name)
	}
	var missing []string
	for _, ref := range refs {
		if _, ok := paths[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		warnings = append(warnings, "images not imported, whose links are left as they are: "+strings.Join(missing, ", "))
	}
	return paths, warnings, nil
}

// readErr maps an error of reading the request body: a body past its limit
// is a bad request.
func readErr(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return gennotes.MakeBadRequest(errors.New("the request body is too large"))
	}
	var gerr *goa.ServiceError
	if errors.As(err, &gerr) {
		return err
	}
	return fmt.Errorf("read the request: %w", err)
}

// countingReader counts the bytes read from r and keeps the error of reading
// it, other than io.EOF, to tell it from the errors of writing.
type countingReader struct {
	r   io.Reader
	n   int64
	err error
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		c.err = err
	}
	return n, err
}

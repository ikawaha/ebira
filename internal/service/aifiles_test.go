package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

func TestAddAiFile(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "chart"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ id, nb string }{{"mine", "work"}, {"secret", "diary"}} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(x.id), Notebook: ptr(x.nb), Title: x.id}); err != nil {
			t.Fatal(err)
		}
	}

	f, err := svc.AddAiFile(ai, &gennotes.AddAiFilePayload{ID: n.ID, Name: "Chart 1.png", Content: pngHead})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Chart-1.png" || f.Path != n.ID+".assets/Chart-1.png" {
		t.Errorf("AddAiFile = %+v", f)
	}
	for _, tt := range []struct {
		name, id, want string
		content        []byte
	}{
		{"a note of people", "mine", "forbidden", pngHead},
		{"a private note", "secret", "not_found", pngHead},
		{"a missing note", "missing", "not_found", pngHead},
		{"SVG", n.ID, "bad_request", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{"too large", n.ID, "too_large", append(bytes.Clone(pngHead), make([]byte, aiMaxFileSize)...)},
	} {
		if _, err := svc.AddAiFile(ai, &gennotes.AddAiFilePayload{ID: tt.id, Name: "a.png", Content: tt.content}); errorName(err) != tt.want {
			t.Errorf("AddAiFile %s: %v, want %s", tt.name, err, tt.want)
		}
	}
}

func TestImportAiNoteWithImages(t *testing.T) {
	ai := context.Background()
	svc, dir := newTestService(t)
	img := func(ref string, size int) *gennotes.AIImportFile {
		return &gennotes.AIImportFile{Ref: ref, Name: filepath.Base(ref), Content: append(bytes.Clone(pngHead), make([]byte, size)...)}
	}
	// Into the inbox, of the drop kind: the image goes with the note.
	res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: store.Inbox, Name: "report.md",
		Content: "# Report\n\n![chart](images/chart.png)\n", Files: []*gennotes.AIImportFile{img("images/chart.png", 0)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "created" || res.Note != nil && res.Note.Body != "" && !strings.Contains(res.Note.Body, res.Note.ID+".assets/chart.png") {
		t.Errorf("ImportAiNote = %+v", res)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, store.Inbox, "*.assets", "chart.png"))
	if len(matches) != 1 {
		t.Errorf("the image in the inbox: %v", matches)
	}
	if b, _ := os.ReadFile(strings.TrimSuffix(filepath.Dir(matches[0]), ".assets") + ".yaml"); !strings.Contains(string(b), ".assets/chart.png") || !strings.Contains(string(b), "created_by: ai") {
		t.Errorf("the note: %s", b)
	}

	// The limits.
	many := make([]*gennotes.AIImportFile, aiMaxFiles+1)
	for i := range many {
		many[i] = img("a.png", 0)
	}
	big := make([]*gennotes.AIImportFile, 5)
	for i := range big {
		big[i] = img("a.png", aiMaxFileSize-len(pngHead)) // 5 of 5 MB: 25 MB in all
	}
	for name, files := range map[string][]*gennotes.AIImportFile{
		"too many":         many,
		"too large":        {img("a.png", aiMaxFileSize)},
		"too large in all": big,
	} {
		if _, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: store.Inbox, Name: "x.md", Content: "x " + name, Files: files}); errorName(err) != "bad_request" {
			t.Errorf("%s: %v, want bad_request", name, err)
		}
	}
}

package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/history"
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
		{"another file", n.ID, "", []byte("%PDF-1.7\n")},
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

func TestReplaceAndDeleteAiFile(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	h, err := history.New(filepath.Join(filepath.Dir(dir), "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetHistory(h)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("mine"), Notebook: ptr("work"), Title: "mine"}); err != nil {
		t.Fatal(err)
	}
	add := func(content string, replace bool) *gennotes.NoteFile {
		t.Helper()
		f, err := svc.AddAiFile(ai, &gennotes.AddAiFilePayload{ID: n.ID, Name: "other-repos.md", Content: []byte(content), Replace: &replace})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	read := func(name string) string {
		t.Helper()
		_, rc, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: n.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		return string(b)
	}

	if f := add("v1", false); f.Name != "other-repos.md" || f.Replaced != nil {
		t.Errorf("AddAiFile = %+v", f)
	}
	if f := add("v2", false); f.Name != "other-repos-2.md" {
		t.Errorf("AddAiFile without replace = %+v", f)
	}
	if f := add("v3", true); f.Name != "other-repos.md" || f.Replaced == nil || !*f.Replaced {
		t.Errorf("AddAiFile with replace = %+v", f)
	}
	if got := read("other-repos.md"); got != "v3" {
		t.Errorf("the replaced file = %q", got)
	}
	// Replacing a file that is not there adds it.
	if f, err := svc.AddAiFile(ai, &gennotes.AddAiFilePayload{ID: n.ID, Name: "new.md", Content: []byte("new"), Replace: ptr(true)}); err != nil || f.Name != "new.md" || f.Replaced != nil {
		t.Errorf("AddAiFile with replace of a new name = %+v, %v", f, err)
	}

	if err := svc.DeleteAiFile(ai, &gennotes.DeleteAiFilePayload{ID: n.ID, Name: "other-repos-2.md"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, id, file, want string
	}{
		{"a missing file", n.ID, "other-repos-2.md", "not_found"},
		{"a note of people", "mine", "a.md", "forbidden"},
		{"a missing note", "missing", "a.md", "not_found"},
	} {
		if err := svc.DeleteAiFile(ai, &gennotes.DeleteAiFilePayload{ID: tt.id, Name: tt.file}); errorName(err) != tt.want {
			t.Errorf("DeleteAiFile %s: %v, want %s", tt.name, err, tt.want)
		}
	}

	// The replaced and the deleted files are kept, newest first, for people
	// only.
	if _, err := svc.FileHistory(ai, &gennotes.FileHistoryPayload{ID: n.ID}); errorName(err) != "not_found" {
		t.Errorf("FileHistory for AI: %v", err)
	}
	fs, err := svc.FileHistory(person, &gennotes.FileHistoryPayload{ID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 || fs[0].Name != "other-repos-2.md" || fs[0].Current || fs[1].Name != "other-repos.md" || !fs[1].Current || fs[1].Size != 2 {
		t.Fatalf("FileHistory = %+v", fs)
	}
	if _, err := svc.RestoreFile(ai, &gennotes.RestoreFilePayload{ID: n.ID, Rev: fs[1].Rev}); errorName(err) != "not_found" {
		t.Errorf("RestoreFile for AI: %v", err)
	}

	// Putting v1 back keeps v3 in its place, so that it can be undone.
	f, err := svc.RestoreFile(person, &gennotes.RestoreFilePayload{ID: n.ID, Rev: fs[1].Rev})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "other-repos.md" || f.Replaced == nil || !*f.Replaced {
		t.Errorf("RestoreFile = %+v", f)
	}
	if got := read("other-repos.md"); got != "v1" {
		t.Errorf("the restored file = %q", got)
	}
	if f, err := svc.RestoreFile(person, &gennotes.RestoreFilePayload{ID: n.ID, Rev: fs[0].Rev}); err != nil || f.Name != "other-repos-2.md" || f.Replaced != nil {
		t.Errorf("RestoreFile of a deleted file = %+v, %v", f, err)
	}
	if got := read("other-repos-2.md"); got != "v2" {
		t.Errorf("the restored deleted file = %q", got)
	}
	fs, err = svc.FileHistory(person, &gennotes.FileHistoryPayload{ID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Name != "other-repos.md" || fs[0].Size != 2 {
		t.Errorf("FileHistory after restoring = %+v", fs)
	}
	if _, err := svc.RestoreFile(person, &gennotes.RestoreFilePayload{ID: n.ID, Rev: "1"}); errorName(err) != "not_found" {
		t.Errorf("RestoreFile of a missing rev: %v", err)
	}

	// People deleting a file keep it too.
	if err := svc.DeleteFile(person, &gennotes.DeleteFilePayload{ID: n.ID, Name: "new.md"}); err != nil {
		t.Fatal(err)
	}
	if fs, err := svc.FileHistory(person, &gennotes.FileHistoryPayload{ID: n.ID}); err != nil || len(fs) != 2 || fs[0].Name != "new.md" {
		t.Errorf("FileHistory after people deleted a file = %+v, %v", fs, err)
	}
}

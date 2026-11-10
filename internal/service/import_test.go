package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// formFile is an image of an import sent in a multipart body.
type formFile struct{ ref, name, content string }

// importForm returns a multipart body of import with the fields, in order,
// and the images, and its content type.
func importForm(t *testing.T, fields [][2]string, files ...formFile) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		if err := w.WriteField("ref", f.ref); err != nil {
			t.Fatal(err)
		}
		fw, err := w.CreateFormFile("file", f.name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(fw, f.content)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

// importOf imports a Markdown file named name with content into notebook
// with a multipart body.
func importOf(ctx context.Context, t *testing.T, svc *Notes, notebook, name, content string, force bool) (*gennotes.ImportResult, error) {
	t.Helper()
	body, ct := importForm(t, [][2]string{{"name", name}, {"content", content}})
	p := &gennotes.ImportPayload{ContentType: &ct, Force: &force}
	if notebook != "" {
		p.Notebook = &notebook
	}
	return svc.Import(ctx, p, io.NopCloser(body))
}

// stages returns the stages left in the notes directory.
func stages(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, ".tmp-stage-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const kyoto = `---
title: 京都の旅
tags: [旅行]
date: 2026-04-01T09:00:00Z
author: me
created_by: person
---
一日目は清水寺。

![寺](img/tera.png)
![外](https://example.com/x.png)
![無い](img/none.png)
`

func TestImport(t *testing.T) {
	person := AsPerson(context.Background())
	svc, dir := newTestService(t)

	body, ct := importForm(t, [][2]string{{"name", "kyoto.md"}, {"content", kyoto}, {"modified_at", "2026-05-01T00:00:00Z"}},
		formFile{"img/tera.png", "tera.png", string(pngHead)},
		formFile{"img/other.png", "other.png", string(pngHead)},
	)
	res, err := svc.Import(person, &gennotes.ImportPayload{Notebook: ptr("travel"), ContentType: &ct}, io.NopCloser(body))
	if err != nil {
		t.Fatal(err)
	}
	n := res.Note
	if res.Status != "created" || n == nil || n.Notebook != "travel" || n.Title != "京都の旅" || !slices.Equal(n.Tags, []string{"旅行"}) {
		t.Fatalf("Import = %+v, note %+v", res, n)
	}
	if n.CreatedAt != "2026-04-01T09:00:00Z" || n.UpdatedAt != "2026-05-01T00:00:00Z" || n.CreatedBy != nil {
		t.Errorf("times %s, %s, created_by %v", n.CreatedAt, n.UpdatedAt, n.CreatedBy)
	}
	wantBody := "一日目は清水寺。\n\n![寺](" + n.ID + ".assets/tera.png)\n![外](https://example.com/x.png)\n![無い](img/none.png)\n"
	if n.Body != wantBody {
		t.Errorf("Body = %q, want %q", n.Body, wantBody)
	}
	if _, err := os.Stat(filepath.Join(dir, "travel", n.ID+".assets", "tera.png")); err != nil {
		t.Errorf("the image: %v", err)
	}
	warnings := strings.Join(res.Warnings, "\n")
	for _, w := range []string{"created_by", "img/other.png", "img/none.png"} {
		if !strings.Contains(warnings, w) {
			t.Errorf("warnings lack %q:\n%s", w, warnings)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "travel", n.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"author: me", "imported_from: kyoto.md", "import_hash: sha256:"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("the note file lacks %q:\n%s", s, b)
		}
	}
	if strings.Contains(string(b), "created_by") {
		t.Errorf("created_by from the front matter was kept:\n%s", b)
	}
	// The note is indexed.
	if found, err := svc.Search(person, &gennotes.SearchPayload{Q: "清水寺"}); err != nil || found.Total != 1 {
		t.Errorf("Search = %+v, %v", found, err)
	}

	// The same content again, even under another name and in the trash, is
	// skipped unless forced.
	if res, err := importOf(person, t, svc, "", "copy.md", kyoto, false); err != nil || res.Status != "skipped" || *res.DuplicateOf != n.ID || res.Note != nil {
		t.Errorf("Import again = %+v, %v", res, err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: n.ID}); err != nil {
		t.Fatal(err)
	}
	if res, err := importOf(person, t, svc, "", "copy.md", kyoto, false); err != nil || res.Status != "skipped" {
		t.Errorf("Import after deleting = %+v, %v", res, err)
	}
	if res, err := importOf(person, t, svc, "", "copy.md", kyoto, true); err != nil || res.Status != "created" || res.Note.Notebook != "inbox" {
		t.Errorf("Import with force = %+v, %v", res, err)
	}

	for name, fields := range map[string][][2]string{
		"no name":    {{"content", "x"}},
		"no content": {{"name", "a.md"}},
		"too large":  {{"name", "a.md"}, {"content", strings.Repeat("x", 5<<20+1)}},
		"bad time":   {{"name", "a.md"}, {"content", "x"}, {"modified_at", "yesterday"}},
	} {
		body, ct := importForm(t, fields)
		if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: &ct}, io.NopCloser(body)); errorName(err) != "bad_request" {
			t.Errorf("Import with %s: %v", name, err)
		}
	}
	if _, err := importOf(person, t, svc, "trash", "a.md", "x", false); errorName(err) != "bad_request" {
		t.Errorf("Import into the trash: %v", err)
	}
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("multipart/form-data; boundary=x")}, io.NopCloser(strings.NewReader("not multipart"))); errorName(err) != "bad_request" {
		t.Errorf("Import of a malformed body: %v", err)
	}
	if left := stages(t, dir); len(left) > 0 {
		t.Errorf("stages left: %v", left)
	}
}

// TestImportJSON checks the JSON body of earlier versions, with the notebook
// and force in the body.
func TestImportJSON(t *testing.T) {
	person := AsPerson(context.Background())
	svc, dir := newTestService(t)
	v := map[string]any{
		"notebook": "travel", "name": "kyoto.md", "content": kyoto, "modified_at": "2026-05-01T00:00:00Z",
		"files": []map[string]any{{"ref": "img/tera.png", "name": "tera.png", "content": pngHead}},
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json")}, io.NopCloser(bytes.NewReader(b)))
	if err != nil {
		t.Fatal(err)
	}
	if n := res.Note; n.Notebook != "travel" || !strings.Contains(n.Body, n.ID+".assets/tera.png") {
		t.Errorf("Import = %+v", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "travel", res.Note.ID+".assets", "tera.png")); err != nil {
		t.Errorf("the image: %v", err)
	}
	v["force"] = true
	b, _ = json.Marshal(v)
	if res, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json")}, io.NopCloser(bytes.NewReader(b))); err != nil || res.Status != "created" {
		t.Errorf("Import with force in the body = %+v, %v", res, err)
	}
	// force=false in the query wins over force in the body.
	if res, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json"), Force: ptr(false)}, io.NopCloser(bytes.NewReader(b))); err != nil || res.Status != "skipped" {
		t.Errorf("Import with force=false in the query = %+v, %v", res, err)
	}
	// The content is required, but may be empty.
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json")}, io.NopCloser(strings.NewReader(`{"name":"a.md"}`))); errorName(err) != "bad_request" {
		t.Errorf("Import without content: %v", err)
	}
	if res, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json")}, io.NopCloser(strings.NewReader(`{"name":"empty.md","content":""}`))); err != nil || res.Status != "created" {
		t.Errorf("Import of an empty file = %+v, %v", res, err)
	}
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: ptr("application/json")}, io.NopCloser(strings.NewReader("{"))); errorName(err) != "bad_request" {
		t.Errorf("Import of broken JSON: %v", err)
	}
}

// cutReader reads r, then fails as a request cut off does.
type cutReader struct{ r io.Reader }

func (c cutReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// TestImportAllOrNothing checks that a request cut off while its images are
// received imports nothing and leaves no files behind, and that images past
// the limit in all fail the import.
func TestImportAllOrNothing(t *testing.T) {
	person := AsPerson(context.Background())
	svc, dir := newTestService(t)
	body, ct := importForm(t, [][2]string{{"name", "kyoto.md"}, {"content", kyoto}},
		formFile{"img/tera.png", "tera.png", string(pngHead) + strings.Repeat("x", 1<<16)},
	)
	cut := body.Bytes()[:body.Len()-(1<<15)]
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: &ct}, io.NopCloser(cutReader{bytes.NewReader(cut)})); err == nil {
		t.Fatal("Import of a request cut off succeeded")
	}
	if found, err := svc.List(person, &gennotes.ListPayload{}); err != nil || found.Total != 0 {
		t.Errorf("notes after the cut = %+v, %v", found, err)
	}
	if left := stages(t, dir); len(left) > 0 {
		t.Errorf("stages left: %v", left)
	}

	// A body that ends cleanly but before its closing boundary, inside an
	// image the file does not refer to, is cut off too: the images after it
	// are missing.
	body, ct = importForm(t, [][2]string{{"name", "kyoto.md"}, {"content", kyoto}},
		formFile{"img/other.png", "other.png", string(pngHead) + strings.Repeat("x", 1<<16)},
		formFile{"img/tera.png", "tera.png", string(pngHead)},
	)
	i := bytes.LastIndex(body.Bytes(), []byte("img/tera.png")) // the field ref, after the file
	cut = body.Bytes()[:i-(1<<10)]
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: &ct}, io.NopCloser(bytes.NewReader(cut))); err == nil {
		t.Fatal("Import of a body ended early succeeded")
	}
	if found, err := svc.List(person, &gennotes.ListPayload{}); err != nil || found.Total != 0 {
		t.Errorf("notes after the early end = %+v, %v", found, err)
	}

	// An image whose ref comes without its file is a bad request too.
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	w.WriteField("name", "a.md")
	w.WriteField("content", "![x](x.png)")
	w.WriteField("ref", "x.png")
	w.WriteField("other", "y")
	w.Close()
	ct = w.FormDataContentType()
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: &ct}, io.NopCloser(&b)); errorName(err) != "bad_request" {
		t.Errorf("Import of a ref without its file: %v", err)
	}

	defer func(n int64) { importMaxImages = n }(importMaxImages)
	importMaxImages = 100
	body, ct = importForm(t, [][2]string{{"name", "kyoto.md"}, {"content", kyoto}},
		formFile{"img/tera.png", "tera.png", string(pngHead) + strings.Repeat("x", 200)},
	)
	if _, err := svc.Import(person, &gennotes.ImportPayload{ContentType: &ct}, io.NopCloser(body)); errorName(err) != "bad_request" {
		t.Errorf("Import past the limit: %v", err)
	}
	if left := stages(t, dir); len(left) > 0 {
		t.Errorf("stages left: %v", left)
	}
}

func TestImportAiNote(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	svc.now = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: shared.ID, Name: "kyoto.md", Content: kyoto})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "created" || *res.Note.CreatedBy != "ai" || res.Note.CreatedAt != "2026-04-01T09:00:00Z" {
		t.Errorf("ImportAiNote = %+v", res.Note)
	}
	// AI clients bring no images; the links stay.
	if !strings.Contains(res.Note.Body, "](img/tera.png)") || !strings.Contains(strings.Join(res.Warnings, " "), "img/tera.png") {
		t.Errorf("body %q, warnings %q", res.Note.Body, res.Warnings)
	}
	if res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: shared.ID, Name: "k.md", Content: kyoto}); err != nil || res.Status != "skipped" {
		t.Errorf("ImportAiNote again = %+v, %v", res, err)
	}

	// A note people imported into a notebook AI clients cannot see does not
	// count, so that the result does not tell them it exists.
	secret := "# 秘密\n\n見せない"
	if _, err := importOf(person, t, svc, "secret", "s.md", secret, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "secret", Kind: ptr("private")}); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: "inbox", Name: "s.md", Content: secret}); err != nil || res.Status != "created" || res.DuplicateOf != nil {
		t.Errorf("ImportAiNote of a hidden duplicate = %+v, %v", res, err)
	}

	for _, nb := range []string{"secret", "work"} {
		if _, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: nb, Name: "a.md", Content: "x"}); errorName(err) != "forbidden" {
			t.Errorf("ImportAiNote into %s: %v", nb, err)
		}
	}
}

package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

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

	res, err := svc.Import(person, &gennotes.ImportPayload{
		Notebook:   ptr("travel"),
		Name:       "kyoto.md",
		Content:    kyoto,
		ModifiedAt: ptr("2026-05-01T00:00:00Z"),
		Files: []*gennotes.ImportFile{
			{Ref: "img/tera.png", Name: "tera.png", Content: pngHead},
			{Ref: "img/other.png", Name: "other.png", Content: pngHead},
		},
	})
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
	again := &gennotes.ImportPayload{Name: "copy.md", Content: kyoto}
	if res, err := svc.Import(person, again); err != nil || res.Status != "skipped" || *res.DuplicateOf != n.ID || res.Note != nil {
		t.Errorf("Import again = %+v, %v", res, err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: n.ID}); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.Import(person, again); err != nil || res.Status != "skipped" {
		t.Errorf("Import after deleting = %+v, %v", res, err)
	}
	again.Force = ptr(true)
	if res, err := svc.Import(person, again); err != nil || res.Status != "created" || res.Note.Notebook != "inbox" {
		t.Errorf("Import with force = %+v, %v", res, err)
	}

	for _, p := range []*gennotes.ImportPayload{
		{Notebook: ptr("trash"), Name: "a.md", Content: "x"},
		{Name: "a.md", Content: strings.Repeat("x", 5<<20+1)},
		{Name: "a.md", Content: "x", ModifiedAt: ptr("yesterday")},
	} {
		if _, err := svc.Import(person, p); errorName(err) != "bad_request" {
			t.Errorf("Import(%s, %d bytes): %v", deref(p.Notebook), len(p.Content), err)
		}
	}
}

func TestImportAiNote(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	svc.now = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	if _, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: "shared", Name: "kyoto.md", Content: kyoto})
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
	if res, err := svc.ImportAiNote(ai, &gennotes.ImportAiNotePayload{Notebook: "shared", Name: "k.md", Content: kyoto}); err != nil || res.Status != "skipped" {
		t.Errorf("ImportAiNote again = %+v, %v", res, err)
	}

	// A note people imported into a notebook AI clients cannot see does not
	// count, so that the result does not tell them it exists.
	secret := "# 秘密\n\n見せない"
	if _, err := svc.Import(person, &gennotes.ImportPayload{Notebook: ptr("secret"), Name: "s.md", Content: secret}); err != nil {
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

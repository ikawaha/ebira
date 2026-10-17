package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func body(b []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(b)) }

func TestFiles(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("n1"), Notebook: ptr("work"), Title: "t"}); err != nil {
		t.Fatal(err)
	}

	f, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: "n1", Name: "Screen Shot.png"}, body(pngHead))
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Screen-Shot.png" || f.Path != "n1.assets/Screen-Shot.png" {
		t.Errorf("AddFile = %+v", f)
	}
	res, rc, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: "n1", Name: f.Name})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if res.ContentType != "image/png" || res.Length != int64(len(pngHead)) || !bytes.Equal(got, pngHead) {
		t.Errorf("GetFile = %+v, %q", res, got)
	}

	for _, tt := range []struct {
		name, want string
		ctx        context.Context
		p          *gennotes.AddFilePayload
		b          []byte
	}{
		{"AI", "not_found", ai, &gennotes.AddFilePayload{ID: "n1", Name: "a.png"}, pngHead},
		{"missing note", "not_found", person, &gennotes.AddFilePayload{ID: "missing", Name: "a.png"}, pngHead},
		{"SVG", "bad_request", person, &gennotes.AddFilePayload{ID: "n1", Name: "a.svg"}, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{"too large", "too_large", person, &gennotes.AddFilePayload{ID: "n1", Name: "a.png"}, append(bytes.Clone(pngHead), make([]byte, store.MaxFileSize)...)},
	} {
		if _, err := svc.AddFile(tt.ctx, tt.p, body(tt.b)); errorName(err) != tt.want {
			t.Errorf("AddFile %s: %v, want %s", tt.name, err, tt.want)
		}
	}
	if _, _, err := svc.GetFile(ai, &gennotes.GetFilePayload{ID: "n1", Name: f.Name}); errorName(err) != "not_found" {
		t.Errorf("GetFile by AI: %v", err)
	}
	if err := svc.DeleteFile(ai, &gennotes.DeleteFilePayload{ID: "n1", Name: f.Name}); errorName(err) != "not_found" {
		t.Errorf("DeleteFile by AI: %v", err)
	}

	// A file put in the directory by hand is served only if it is an image of
	// an accepted type.
	if err := os.WriteFile(filepath.Join(dir, "work", "n1.assets", "page.png"), []byte("<html><script>alert(1)</script>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: "n1", Name: "page.png"}); errorName(err) != "not_found" {
		t.Errorf("GetFile of HTML named .png: %v", err)
	}

	// The files go with the note to the trash and back.
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "n1"}); err != nil {
		t.Fatal(err)
	}
	if _, rc, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: "n1", Name: f.Name}); err != nil {
		t.Errorf("GetFile in the trash: %v", err)
	} else {
		rc.Close()
	}
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "n1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "n1.assets", f.Name)); err != nil {
		t.Errorf("the file after restoring: %v", err)
	}

	if err := svc.DeleteFile(person, &gennotes.DeleteFilePayload{ID: "n1", Name: f.Name}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: "n1", Name: f.Name}); errorName(err) != "not_found" {
		t.Errorf("GetFile after DeleteFile: %v", err)
	}
}

func TestCheckFiles(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	for _, n := range []struct{ id, title string }{{"n1", "One"}, {"n2", "Two"}, {"n3", "Three"}} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(n.id), Notebook: ptr("work"), Title: n.title}); err != nil {
			t.Fatal(err)
		}
	}
	add := func(id, name string, age time.Duration) {
		t.Helper()
		got, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: id, Name: name}, body(pngHead))
		if err != nil {
			t.Fatal(err)
		}
		at := now.Add(-age)
		if err := os.Chtimes(filepath.Join(dir, "work", id+".assets", got.Name), at, at); err != nil {
			t.Fatal(err)
		}
	}
	add("n1", "used.png", 48*time.Hour)
	add("n1", "old.png", 48*time.Hour)
	add("n1", "new.png", 10*time.Minute)
	add("n2", "elsewhere.png", 48*time.Hour)
	add("n3", "trash.png", 48*time.Hour)
	update := func(id, b string) {
		t.Helper()
		if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: id, Body: ptr(b)}); err != nil {
			t.Fatal(err)
		}
	}
	// n1 uses its own file and one of n2; n3, in the trash, uses its own file
	// and refers to one that is gone.
	update("n1", "![u](n1.assets/used.png)\n![e](n2.assets/elsewhere.png)")
	update("n3", "![t](n3.assets/trash.png)\n![g](n3.assets/gone.png)\n![g](n9.assets/x.png)")
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "n3"}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.CheckFiles(person)
	if err != nil {
		t.Fatal(err)
	}
	var unused, broken []string
	for _, f := range res.Unused {
		unused = append(unused, fmt.Sprintf("%s %s %s %v", f.Path, f.Title, f.AddedAt, f.Recent))
	}
	for _, b := range res.Broken {
		broken = append(broken, b.Title+" "+b.Path)
	}
	wantUnused := []string{
		"n1.assets/new.png One 2026-10-17T11:50:00Z true",
		"n1.assets/old.png One 2026-10-15T12:00:00Z false",
	}
	if !slices.Equal(unused, wantUnused) {
		t.Errorf("unused = %q, want %q", unused, wantUnused)
	}
	if want := []string{"Three n3.assets/gone.png", "Three n9.assets/x.png"}; !slices.Equal(broken, want) {
		t.Errorf("broken = %q, want %q", broken, want)
	}

	if res, err := svc.CheckFiles(ai); err != nil || len(res.Unused)+len(res.Broken) != 0 {
		t.Errorf("CheckFiles by AI = %+v, %v", res, err)
	}
}

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
	"github.com/ikawaha/ebira/internal/history"
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

	// Images and PDF files are shown; other files, also an HTML file named
	// .png, are served as downloads.
	for _, tt := range []struct{ name, content, ct, disposition string }{
		{"page.png", "<html><script>alert(1)</script>", "application/octet-stream", "attachment; filename=page.png"},
		{"a.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, "application/octet-stream", "attachment; filename=a.svg"},
		{"report.pdf", "%PDF-1.7\n", "application/pdf", "inline; filename=report.pdf"},
		{"報告.zip", "PK\x03\x04", "application/octet-stream", "attachment; filename*=utf-8''%E5%A0%B1%E5%91%8A.zip"},
	} {
		got, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: "n1", Name: tt.name}, body([]byte(tt.content)))
		if err != nil || got.Name != tt.name {
			t.Fatalf("AddFile(%q) = %+v, %v", tt.name, got, err)
		}
		res, rc, err := svc.GetFile(person, &gennotes.GetFilePayload{ID: "n1", Name: tt.name})
		if err != nil {
			t.Fatalf("GetFile(%q): %v", tt.name, err)
		}
		rc.Close()
		if res.ContentType != tt.ct || res.Disposition != tt.disposition || res.Length != int64(len(tt.content)) {
			t.Errorf("GetFile(%q) = %+v", tt.name, res)
		}
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
	h, err := history.New(filepath.Join(filepath.Dir(dir), "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetHistory(h)
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
	// n1 uses its own file and one of n2, and used old.png in an earlier
	// version; n3, in the trash, uses its own file and refers to one that is
	// gone.
	update("n1", "[o](n1.assets/old.png)")
	update("n1", "![u](n1.assets/used.png)\n![e](n2.assets/elsewhere.png)")
	update("n3", "![t](n3.assets/trash.png)\n![g](n3.assets/gone.png)\n![g](n9.assets/x.png)")
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "n3"}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.CheckFiles(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	var unused, broken []string
	for _, f := range res.Unused {
		unused = append(unused, fmt.Sprintf("%s %s %s %v %v", f.Path, f.Title, f.AddedAt, f.Recent, f.InHistory))
	}
	for _, b := range res.Broken {
		broken = append(broken, b.Title+" "+b.Path)
	}
	wantUnused := []string{
		"n1.assets/new.png One 2026-10-17T11:50:00Z true false",
		"n1.assets/old.png One 2026-10-15T12:00:00Z false true",
	}
	if !slices.Equal(unused, wantUnused) {
		t.Errorf("unused = %q, want %q", unused, wantUnused)
	}
	if want := []string{"Three n3.assets/gone.png", "Three n9.assets/x.png"}; !slices.Equal(broken, want) {
		t.Errorf("broken = %q, want %q", broken, want)
	}

	if res, err := svc.CheckFiles(ai, nil); err != nil || len(res.Unused)+len(res.Broken) != 0 {
		t.Errorf("CheckFiles by AI = %+v, %v", res, err)
	}
}

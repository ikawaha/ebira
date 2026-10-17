package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.md": "", "b.MARKDOWN": "", "c.txt": "", "sub/d.md": "", ".hidden/e.md": "", "sub/.f.md": "",
	})
	got, err := markdownFiles([]string{dir, filepath.Join(dir, "c.txt")})
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range got {
		got[i], _ = filepath.Rel(dir, f)
	}
	// A file named on the command line is taken as is.
	if want := []string{"a.md", "b.MARKDOWN", "sub/d.md", "c.txt"}; !slices.Equal(got, want) {
		t.Errorf("markdownFiles = %v, want %v", got, want)
	}
}

func TestImportFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"notes/旅行.md":        "# 京都\n\n![寺](../img/tera%201.png)\n![無い](none.png)\n![外](https://example.com/x.png)\n",
		"img/tera 1.png":     string(pngHead),
		"notes/latin1.md":    "caf\xe9",
		"notes/duplicate.md": "dup",
	})
	files := []string{filepath.Join(dir, "notes", "旅行.md"), filepath.Join(dir, "notes", "latin1.md"), filepath.Join(dir, "notes", "duplicate.md")}

	var sent []*gennotes.ImportPayload
	send := func(_ context.Context, v any) (any, error) {
		p := v.(*gennotes.ImportPayload)
		sent = append(sent, p)
		if p.Name == "duplicate.md" {
			id := "old"
			return &gennotes.ImportResult{Status: "skipped", DuplicateOf: &id, Warnings: []string{}}, nil
		}
		return &gennotes.ImportResult{Status: "created", Note: &gennotes.Note{ID: "n1", Notebook: deref(p.Notebook), Title: "京都"}, Warnings: []string{"images not imported: none.png"}}, nil
	}
	var out strings.Builder
	err := importFiles(context.Background(), files, "travel", true, send, &out)
	if err == nil || !strings.Contains(err.Error(), "1 files failed") {
		t.Errorf("err = %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d files", len(sent))
	}
	p := sent[0]
	if p.Name != "旅行.md" || deref(p.Notebook) != "travel" || p.Force == nil || !*p.Force || p.ModifiedAt == nil {
		t.Errorf("payload = %+v", p)
	}
	// The image is read relative to the file, with the path as written.
	if len(p.Files) != 1 || p.Files[0].Ref != "../img/tera%201.png" || p.Files[0].Name != "tera 1.png" || string(p.Files[0].Content) != string(pngHead) {
		t.Errorf("files = %+v", p.Files)
	}
	for _, want := range []string{"created  ", "(travel/n1, 1 images)", "    images not imported: none.png", "failed   ", "not UTF-8", "skipped  ", "(imported before as old)", "1 created, 1 skipped, 1 failed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	// A dry run sends nothing.
	out.Reset()
	if err := importFiles(context.Background(), files[:1], "", false, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `would import`) || !strings.Contains(out.String(), `"京都"`) || !strings.Contains(out.String(), "images: 1 of 2") {
		t.Errorf("dry run:\n%s", out.String())
	}
}

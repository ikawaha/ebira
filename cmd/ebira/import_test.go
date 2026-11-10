package main

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
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

	// sentImport is an import as the server reads it.
	type sentImport struct {
		p      *gennotes.ImportPayload
		fields map[string]string
		refs   []string
		names  []string
		images []string
	}
	var sent []sentImport
	send := func(_ context.Context, v any) (any, error) {
		data := v.(*gennotes.ImportRequestData)
		_, params, err := mime.ParseMediaType(deref(data.Payload.ContentType))
		if err != nil {
			t.Fatal(err)
		}
		got := sentImport{p: data.Payload, fields: map[string]string{}}
		mr := multipart.NewReader(data.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			switch part.FormName() {
			case "ref":
				got.refs = append(got.refs, string(b))
			case "file":
				got.names = append(got.names, part.FileName())
				got.images = append(got.images, string(b))
			default:
				got.fields[part.FormName()] = string(b)
			}
		}
		sent = append(sent, got)
		if got.fields["name"] == "duplicate.md" {
			id := "old"
			return &gennotes.ImportResult{Status: "skipped", DuplicateOf: &id, Warnings: []string{}}, nil
		}
		return &gennotes.ImportResult{Status: "created", Note: &gennotes.Note{ID: "n1", Notebook: deref(data.Payload.Notebook), Title: "京都"}, Warnings: []string{"images not imported: none.png"}}, nil
	}
	var out strings.Builder
	err := importFiles(context.Background(), files, "travel", true, send, &out)
	if err == nil || !strings.Contains(err.Error(), "1 files failed") {
		t.Errorf("err = %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d files", len(sent))
	}
	got := sent[0]
	if got.fields["name"] != "旅行.md" || !strings.HasPrefix(got.fields["content"], "# 京都") || got.fields["modified_at"] == "" || deref(got.p.Notebook) != "travel" || got.p.Force == nil || !*got.p.Force {
		t.Errorf("import = %+v", got)
	}
	// The image is read relative to the file, with the path as written.
	if !slices.Equal(got.refs, []string{"../img/tera%201.png"}) || !slices.Equal(got.names, []string{"tera 1.png"}) || !slices.Equal(got.images, []string{string(pngHead)}) {
		t.Errorf("images = %q %q %q", got.refs, got.names, got.images)
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

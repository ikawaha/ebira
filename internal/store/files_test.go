package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// pngHead is enough of a PNG file for its type to be detected.
var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestCleanFileName(t *testing.T) {
	tests := []struct{ name, ext, want string }{
		{"screenshot.png", ".png", "screenshot.png"},
		{"Screen Shot 2026-10-17 at 9.30.00.PNG", ".png", "Screen-Shot-2026-10-17-at-9-30-00.png"},
		{"photo.jpeg", ".jpg", "photo.jpg"},
		{"../../etc/passwd", ".png", "passwd.png"},
		{`C:\Users\me\a b.gif`, ".gif", "a-b.gif"},
		{".hidden.png", ".png", "hidden.png"},
		{"スクリーンショット 2026.png", ".png", "スクリーンショット-2026.png"},
		{"か\u3099き.png", ".png", "がき.png"}, // NFC
		{"...", ".png", "file.png"},
		{"", ".webp", "file.webp"},
	}
	for _, tt := range tests {
		if got := CleanFileName(tt.name, tt.ext); got != tt.want {
			t.Errorf("CleanFileName(%q, %q) = %q, want %q", tt.name, tt.ext, got, tt.want)
		}
	}
	for _, name := range []string{"a.png", "a-2.jpg", "画像.webp"} {
		if !validFileName(name) {
			t.Errorf("validFileName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "a.svg", "a.jpeg", "../a.png", "a/b.png", ".a.png", "a b.png", "a.b.png"} {
		if validFileName(name) {
			t.Errorf("validFileName(%q) = true", name)
		}
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(&Note{ID: "n1", Notebook: "work", Title: "t"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AddFile("missing", "a.png", pngHead); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddFile to a missing note: %v", err)
	}
	if _, err := s.AddFile("n1", "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); !errors.Is(err, ErrUnsupportedFile) {
		t.Errorf("AddFile of SVG: %v", err)
	}
	if _, err := s.AddFile("n1", "a.html", []byte("<html><script>alert(1)</script>")); !errors.Is(err, ErrUnsupportedFile) {
		t.Errorf("AddFile of HTML: %v", err)
	}
	big := append(slices.Clone(pngHead), make([]byte, MaxFileSize)...)
	if _, err := s.AddFile("n1", "a.png", big); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("AddFile of a large file: %v", err)
	}

	// The extension follows the content; a taken name gets a suffix.
	var names []string
	for _, name := range []string{"shot.png", "shot.jpg", "shot.png"} {
		got, err := s.AddFile("n1", name, pngHead)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, got)
	}
	if want := []string{"shot.png", "shot-2.png", "shot-3.png"}; !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	p, err := s.File("n1", "shot.png")
	if err != nil || p != filepath.Join(dir, "work", "n1.assets", "shot.png") {
		t.Errorf("File = %q, %v", p, err)
	}
	for _, name := range []string{"missing.png", "../n1.yaml", "n1.yaml"} {
		if _, err := s.File("n1", name); !errors.Is(err, ErrFileNotFound) {
			t.Errorf("File(%q): %v", name, err)
		}
	}
	if got := FilePath("n1", "shot.png"); got != "n1.assets/shot.png" {
		t.Errorf("FilePath = %q", got)
	}

	// The files move with the note, also into and out of the trash, and the
	// directory is not taken for a notebook in the trash.
	if err := s.Move("n1", "hobby"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.File("n1", "shot.png"); err != nil {
		t.Errorf("File after Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "n1.assets")); !os.IsNotExist(err) {
		t.Errorf("the old directory is left: %v", err)
	}
	if err := s.Move("n1", Trash); err != nil {
		t.Fatal(err)
	}
	if nbs, err := s.TrashedNotebooks(); err != nil || len(nbs) != 0 {
		t.Errorf("TrashedNotebooks = %v, %v", nbs, err)
	}
	if files, err := s.Scan(); err != nil || len(files) != 1 {
		t.Errorf("Scan = %v, %v", files, err)
	}
	if err := s.Move("n1", "work"); err != nil {
		t.Fatal(err)
	}

	// A file in the way stops the move, which leaves the note where it was.
	if err := os.MkdirAll(filepath.Join(dir, "other", "n1.assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("n1", "other"); err == nil {
		t.Error("Move onto existing files succeeded")
	}
	if n, err := s.Load("n1"); err != nil || n.Notebook != "work" {
		t.Errorf("after the failed move: %+v, %v", n, err)
	}

	if err := s.DeleteFile("n1", "shot-2.png"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile("n1", "shot-2.png"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("DeleteFile twice: %v", err)
	}
	if err := s.Delete("n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "n1.assets")); !os.IsNotExist(err) {
		t.Errorf("the files are left after Delete: %v", err)
	}
}

func TestDeleteFileRemovesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(&Note{ID: "n1", Notebook: Inbox, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	name, err := s.AddFile("n1", "a.png", pngHead)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile("n1", name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, Inbox, "n1.assets")); !os.IsNotExist(err) {
		t.Errorf("the empty directory is left: %v", err)
	}
}

func TestFileRefs(t *testing.T) {
	body := "![a](n1.assets/a.png) and again ![a](n1.assets/a.png)\n" +
		"<img src=\"n2.assets/b-2.webp\">\n" +
		"[link](other.assets/doc.jpg)\n" +
		"`n3.assets/code.gif` in code\n" +
		"![e](n1.assets/%E7%94%BB%E5%83%8F.png)\n" +
		"![j](n1.assets/画像-2.png)\n" +
		"not a file: n1.assets/a.svg n1.assets/ n1.assets/a.png.bak"
	want := []FileRef{
		{"n1", "a.png"}, {"n2", "b-2.webp"}, {"other", "doc.jpg"}, {"n3", "code.gif"},
		{"n1", "画像.png"}, {"n1", "画像-2.png"},
	}
	if got := FileRefs(body); !slices.Equal(got, want) {
		t.Errorf("FileRefs = %v, want %v", got, want)
	}
}

func TestAllFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*Note{{ID: "b", Notebook: "work", Title: "t"}, {ID: "a", Notebook: Inbox, Title: "t"}} {
		if err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct{ id, name string }{{"b", "x.png"}, {"a", "y.png"}, {"a", "x.png"}} {
		if _, err := s.AddFile(f.id, f.name, pngHead); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Move("b", Trash); err != nil {
		t.Fatal(err)
	}
	// Left out: a file with a name AddFile does not give, and the files of a
	// note that is gone.
	if err := os.WriteFile(filepath.Join(dir, Inbox, "a.assets", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "work", "gone.assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work", "gone.assets", "z.png"), pngHead, 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := s.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.NoteID+"/"+f.Name)
		if f.Size != int64(len(pngHead)) || f.ModTime.IsZero() {
			t.Errorf("%s: size %d, time %v", f.Name, f.Size, f.ModTime)
		}
	}
	if want := []string{"a/x.png", "a/y.png", "b/x.png"}; !slices.Equal(got, want) {
		t.Errorf("AllFiles = %v, want %v", got, want)
	}
}

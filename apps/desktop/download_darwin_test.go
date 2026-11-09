package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSaveDownload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "Downloads")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"report.pdf", "report 2.pdf", "report 3.pdf"} {
		got, err := saveDownload("report.pdf", []byte("%PDF"))
		if err != nil || got != want {
			t.Fatalf("saveDownload = %q, %v, want %q", got, err, want)
		}
	}
	if got, err := saveDownload(`..\..\evil.sh`, []byte("x")); err != nil || got != "evil.sh" {
		t.Errorf("saveDownload of a path = %q, %v", got, err)
	}
	for _, name := range []string{"", "..", ".hidden"} {
		if got, err := saveDownload(name, []byte("x")); err == nil {
			t.Errorf("saveDownload(%q) = %q, want an error", name, got)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "report 2.pdf"))
	if err != nil || string(b) != "%PDF" {
		t.Errorf("saved = %q, %v", b, err)
	}
	buf := make([]byte, 128)
	n, err := unix.Getxattr(filepath.Join(dir, "report.pdf"), "com.apple.quarantine", buf)
	if err != nil || !strings.HasSuffix(string(buf[:n]), ";ebira;") {
		t.Errorf("quarantine = %q, %v", buf[:n], err)
	}
}

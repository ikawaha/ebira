package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := make(chan struct{}, 10)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, dir, ".yaml", 100*time.Millisecond, func() { calls <- struct{}{} }, func(err error) { t.Error(err) })
	}()
	time.Sleep(100 * time.Millisecond) // wait for the watcher to start

	called := func(want bool, msg string) {
		t.Helper()
		select {
		case <-calls:
			if !want {
				t.Fatalf("%s: fn was called", msg)
			}
		case <-time.After(400 * time.Millisecond):
			if want {
				t.Fatalf("%s: fn was not called", msg)
			}
		}
	}
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Files with other extensions, and files directly under dir, are ignored.
	write("inbox/a.txt")
	write("top.yaml")
	called(false, "ignored files")

	// A burst of writes results in a single call.
	for range 5 {
		write("inbox/a.yaml")
	}
	called(true, "burst")
	called(false, "burst (second call)")

	// A new subdirectory is reported and then watched.
	if err := os.Mkdir(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	called(true, "new subdirectory")
	write("work/b.yaml")
	called(true, "file in new subdirectory")

	// Renaming a subdirectory is reported.
	if err := os.Rename(filepath.Join(dir, "work"), filepath.Join(dir, "job")); err != nil {
		t.Fatal(err)
	}
	called(true, "renamed subdirectory")

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestRunNested checks the subdirectories of a nested directory, such as the
// notebooks in the trash.
func TestRunNested(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"work", "trash", "trash/old"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 10)
	go Run(ctx, dir, ".yaml", 100*time.Millisecond, func() { calls <- struct{}{} }, func(err error) { t.Error(err) }, "trash")
	time.Sleep(100 * time.Millisecond)

	called := func(msg string) {
		t.Helper()
		select {
		case <-calls:
		case <-time.After(400 * time.Millisecond):
			t.Fatalf("%s: fn was not called", msg)
		}
	}
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, path), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("trash/old/a.yaml")
	called("a note in a notebook in the trash")
	write("trash/b.yaml")
	called("a note deleted on its own")

	// A notebook moved into the trash is reported and then watched.
	if err := os.Rename(filepath.Join(dir, "work"), filepath.Join(dir, "trash", "work")); err != nil {
		t.Fatal(err)
	}
	called("a notebook moved into the trash")
	time.Sleep(50 * time.Millisecond)
	write("trash/work/c.yaml")
	called("a note in the moved notebook")
}

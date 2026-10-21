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
		done <- Run(ctx, dir, ".yaml", 100*time.Millisecond, func() { calls <- struct{}{} }, func(err error) { t.Error(err) }, 1)
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

// TestRunNested checks the directories deeper down, such as the notebooks in
// the trash and the sub notebooks.
func TestRunNested(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"work", "work/sub", "trash", "trash/old", "trash/old/sub"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 10)
	go Run(ctx, dir, ".yaml", 100*time.Millisecond, func() { calls <- struct{}{} }, func(err error) { t.Error(err) }, 3)
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
	write("work/sub/s.yaml")
	called("a note in a sub notebook")
	write("trash/old/sub/s.yaml")
	called("a note in a sub notebook in the trash")
	if err := os.Mkdir(filepath.Join(dir, "work", "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	called("a new sub notebook")
	time.Sleep(50 * time.Millisecond)
	write("work/new/n.yaml")
	called("a note in the new sub notebook")
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
	write("trash/work/sub/d.yaml")
	called("a note in a sub notebook of the moved notebook")

	// The files of a note in the trash are not a notebook: neither the
	// directory nor its files are reported.
	if err := os.Mkdir(filepath.Join(dir, "trash", "n1.assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	write("trash/n1.assets/a.png")
	write("trash/n1.assets/b.yaml")
	select {
	case <-calls:
		t.Error("fn was called for the files of a note")
	case <-time.After(300 * time.Millisecond):
	}
}

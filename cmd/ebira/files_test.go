package main

import (
	"context"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestCheckFiles(t *testing.T) {
	res := &gennotes.FileCheck{
		Unused: []*gennotes.UnusedFile{
			{NoteID: "n1", Title: "One", Name: "old.png", Path: "n1.assets/old.png", Size: 2048, AddedAt: "2026-10-15T12:00:00Z"},
			{NoteID: "n1", Title: "One", Name: "new.png", Path: "n1.assets/new.png", Size: 100, AddedAt: "2026-10-17T11:50:00Z", Recent: true},
		},
		Broken: []*gennotes.BrokenFileRef{{NoteID: "n3", Title: "Three", Path: "n3.assets/gone.png"}},
	}
	check := func(context.Context, any) (any, error) { return res, nil }
	var deleted []string
	deleteFile := func(_ context.Context, p any) (any, error) {
		deleted = append(deleted, p.(*gennotes.DeleteFilePayload).Name)
		return nil, nil
	}

	var out strings.Builder
	if err := checkFiles(context.Background(), check, deleteFile, false, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Unused files: 2", "n1.assets/old.png  2.0 KB", "n1.assets/new.png  100 B", "within the last hour", "Broken references: 1", "n3.assets/gone.png  in Three (n3)", "run with -delete"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(deleted) != 0 {
		t.Errorf("deleted without -delete: %v", deleted)
	}

	out.Reset()
	if err := checkFiles(context.Background(), check, deleteFile, true, &out); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "old.png" || !strings.Contains(out.String(), "Deleted 1 files (2.0 KB)") {
		t.Errorf("deleted %v:\n%s", deleted, out.String())
	}
}

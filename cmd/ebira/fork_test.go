package main

import (
	"context"
	"slices"
	"testing"
)

func TestForkTools(t *testing.T) {
	readOnly, others, err := forkTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"search_notes", "get_note", "list_notes", "get_ai_board"} {
		if !slices.Contains(readOnly, name) {
			t.Errorf("%s does not only read: %v", name, readOnly)
		}
	}
	for _, name := range []string{"create_ai_note", "update_ai_note", "delete_ai_note", "start_ai_board", "open_ai_board"} {
		if !slices.Contains(others, name) || slices.Contains(readOnly, name) {
			t.Errorf("a fork may call %s", name)
		}
	}
}

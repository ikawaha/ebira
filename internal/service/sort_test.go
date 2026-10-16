package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestSortAndMove(t *testing.T) {
	svc, dir := newTestService(t)
	ctx := AsPerson(context.Background())
	// Notes created a minute apart: a (oldest), b, c, d (newest).
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var now time.Time
	svc.now = func() time.Time { return now }
	titles := map[string]string{"a": "beta", "b": "Alpha", "c": "ｇａｍｍａ", "d": "delta"}
	for i, id := range []string{"a", "b", "c", "d"} {
		now = base.Add(time.Duration(i) * time.Minute)
		if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: titles[id]}); err != nil {
			t.Fatal(err)
		}
	}
	// Edit a so that it is the most recently updated.
	now = base.Add(time.Hour)
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("edited")}); err != nil {
		t.Fatal(err)
	}
	list := func(sort *string) []string {
		t.Helper()
		res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Sort: sort, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, n := range res.Notes {
			ids = append(ids, n.ID)
		}
		return ids
	}
	check := func(name string, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	// The manual order starts as the creation order, newest first.
	check("default", list(nil), "d", "c", "b", "a")
	check("created", list(ptr("created")), "d", "c", "b", "a")
	check("updated", list(ptr("updated")), "a", "d", "c", "b")
	// Title: case and width are ignored ("ｇａｍｍａ" is "gamma").
	check("title", list(ptr("title")), "b", "a", "d", "c")

	// The notebook's sort setting is the default of its list.
	nb, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: "work", Sort: ptr("title")})
	if err != nil {
		t.Fatal(err)
	}
	if nb.Sort != "title" {
		t.Errorf("Notebook.Sort = %q", nb.Sort)
	}
	check("default with sort: title", list(nil), "b", "a", "d", "c")
	check("explicit order", list(ptr("order")), "d", "c", "b", "a")
	if _, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: "work", Sort: ptr("order")}); err != nil {
		t.Fatal(err)
	}

	// A move writes only the moved note, and keeps its updated_at.
	versions := map[string]string{}
	for _, id := range []string{"a", "b", "c", "d"} {
		n, _ := svc.Get(ctx, &gennotes.GetPayload{ID: id})
		versions[id] = n.Version
	}
	moved, err := svc.Move(ctx, &gennotes.MovePayload{ID: "a", After: ptr("d")})
	if err != nil {
		t.Fatal(err)
	}
	check("a after d", list(nil), "d", "a", "c", "b")
	if moved.UpdatedAt != base.Add(time.Hour).Format(time.RFC3339) {
		t.Errorf("updated_at = %s, want unchanged", moved.UpdatedAt)
	}
	for _, id := range []string{"b", "c", "d"} {
		if n, _ := svc.Get(ctx, &gennotes.GetPayload{ID: id}); n.Version != versions[id] {
			t.Errorf("note %s was written", id)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "work", "a.yaml")); !strings.Contains(string(b), "order: ") {
		t.Errorf("a.yaml has no order:\n%s", b)
	}
	// First and last.
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	check("b first", list(nil), "b", "d", "a", "c")
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "d", After: ptr("c")}); err != nil {
		t.Fatal(err)
	}
	check("d last", list(nil), "b", "a", "c", "d")
	// The order survives a rebuild of the index from the files.
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	check("after sync", list(nil), "b", "a", "c", "d")

	// Errors: a note of another notebook, the note itself, the trash.
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("x"), Title: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, after := range []string{"x", "a", "missing"} {
		if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "a", After: ptr(after)}); errorName(err) != "bad_request" {
			t.Errorf("move after %s: err = %v, want bad_request", after, err)
		}
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "x"}); errorName(err) != "bad_request" {
		t.Errorf("move in the trash: err = %v, want bad_request", err)
	}
}

// TestMoveRespace checks that the notes are spaced again when the neighbors of
// the new place leave no room between their keys.
func TestMoveRespace(t *testing.T) {
	svc, dir := newTestService(t)
	ctx := AsPerson(context.Background())
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	for id, order := range map[string]int{"p": 11, "q": 10, "r": 5} {
		src := "title: " + id + "\ntags: []\ncreated_at: 2026-10-01T09:00:00Z\nupdated_at: 2026-10-01T09:00:00Z\norder: " + strconv.Itoa(order) + "\nbody: \"\"\n"
		if err := os.WriteFile(filepath.Join(dir, "work", id+".yaml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// p (11) and q (10) have no key between them.
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "r", After: ptr("p")}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range res.Notes {
		ids = append(ids, n.ID)
	}
	if want := []string{"p", "r", "q"}; !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// TestListPages checks that a list tells the number of all notes, so that
// clients can read the next page.
func TestListPages(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct{ offset, want int }{{0, 2}, {2, 1}, {3, 0}} {
		res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Limit: 2, Offset: tt.offset})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 3 || len(res.Notes) != tt.want {
			t.Errorf("offset %d: total %d, %d notes; want 3, %d", tt.offset, res.Total, len(res.Notes), tt.want)
		}
	}
	res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Tag: ptr("none"), Limit: 2})
	if err != nil || res.Total != 0 {
		t.Errorf("tag filter: %+v, %v; want total 0", res, err)
	}
}

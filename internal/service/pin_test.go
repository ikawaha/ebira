package service

import (
	"context"
	"slices"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// The notes with the pin mark come first in the lists of their notebook, and
// move only among themselves; the others and the separators stay below them.
func TestPinnedNotes(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := AsPerson(context.Background())
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var now time.Time
	svc.now = func() time.Time { return now }
	for i, id := range []string{"a", "b", "c", "d"} {
		now = base.Add(time.Duration(i) * time.Minute)
		if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: id, Tags: []string{"x"}}); err != nil {
			t.Fatal(err)
		}
	}
	now = base.Add(time.Hour)
	check := func(name, want string) {
		t.Helper()
		if got := order(t, svc, ctx, "work"); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	check("start", "d c b a")
	for _, id := range []string{"a", "b"} {
		if _, err := svc.SetMarks(ctx, &gennotes.SetMarksPayload{ID: id, Marks: []string{"pin"}}); err != nil {
			t.Fatal(err)
		}
	}
	check("pinned", "b a d c")

	// Every sort of the notebook puts them first.
	for _, tt := range []struct {
		sort string
		want []string
	}{
		{"created", []string{"b", "a", "d", "c"}},
		{"title", []string{"a", "b", "c", "d"}},
	} {
		res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Sort: ptr(tt.sort), Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(res.Notes); !slices.Equal(got, tt.want) {
			t.Errorf("sort %s = %v, want %v", tt.sort, got, tt.want)
		}
	}
	// All the notes, a tag and a search do not.
	for name, p := range map[string]*gennotes.ListPayload{
		"all": {Limit: 50},
		"tag": {Notebook: ptr("work"), Tag: ptr("x"), Limit: 50},
	} {
		res, err := svc.List(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(res.Notes); !slices.Equal(got, []string{"d", "c", "b", "a"}) {
			t.Errorf("%s = %v", name, got)
		}
	}

	// A new note goes first among the others.
	now = base.Add(2 * time.Hour)
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("e"), Notebook: ptr("work"), Title: "e"}); err != nil {
		t.Fatal(err)
	}
	check("new", "b a e d c")

	// The pinned notes are reordered among themselves, and the others among
	// themselves; "first" is the first of the group.
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "b", After: ptr("a")}); err != nil {
		t.Fatal(err)
	}
	check("pinned moved", "a b e d c")
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	check("moved first", "a b c e d")
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "e", After: ptr("b")}); err != nil {
		t.Fatal(err)
	}
	check("after the last pinned", "a b e c d")
	for _, p := range []*gennotes.MovePayload{
		{ID: "d", After: ptr("a")}, // among the pinned notes
		{ID: "a", After: ptr("c")}, // among the others
	} {
		if _, err := svc.Move(ctx, p); errorName(err) != "bad_request" {
			t.Errorf("Move %s after %s: %v, want bad_request", p.ID, deref(p.After), err)
		}
	}
	check("refused", "a b e c d")

	// Separators stay below the pinned notes.
	if _, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("top")}); err != nil {
		t.Fatal(err)
	}
	check("separator first", "a b |top e c d")
	sep, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("mid"), After: ptr("c")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("no"), After: ptr("a")}); errorName(err) != "bad_request" {
		t.Errorf("a separator among the pinned notes: %v", err)
	}
	if _, err := svc.MoveSeparator(ctx, &gennotes.MoveSeparatorPayload{ID: sep.ID, Notebook: "work", After: ptr("a")}); errorName(err) != "bad_request" {
		t.Errorf("a separator moved among the pinned notes: %v", err)
	}
	check("separators", "a b |top e c |mid d")

	// Unpinned, a note goes back among the others by its key; pinned, it
	// goes among the pinned notes.
	if _, err := svc.SetMarks(ctx, &gennotes.SetMarksPayload{ID: "a", Marks: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMarks(ctx, &gennotes.SetMarksPayload{ID: "d", Marks: []string{"pin"}}); err != nil {
		t.Fatal(err)
	}
	check("pins changed", "d b |top e c |mid a")
}

func ids(notes []*gennotes.NoteSummary) []string {
	var res []string
	for _, n := range notes {
		res = append(res, n.ID)
	}
	return res
}

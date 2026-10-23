package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

func TestMarks(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("todo"), Notebook: ptr("work"), Title: "todo", Body: ptr("本文")})
	if err != nil {
		t.Fatal(err)
	}

	// Setting a mark keeps the update time, and the file holds it.
	got, err := svc.SetMarks(person, &gennotes.SetMarksPayload{ID: n.ID, Marks: []string{"star"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Marks, []string{"star"}) || got.UpdatedAt != n.UpdatedAt {
		t.Errorf("SetMarks = %v, updated %s (was %s)", got.Marks, got.UpdatedAt, n.UpdatedAt)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "work", "todo.yaml"))
	if !strings.Contains(string(b), "marks: [star]") {
		t.Errorf("the file:\n%s", b)
	}
	if h, err := svc.History(person, &gennotes.HistoryPayload{ID: n.ID}); err == nil && len(h) != 0 {
		t.Errorf("an earlier version was kept: %v", h)
	}
	// Lists and searches show it, and mark: finds it, by ID or by name.
	list, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr("work"), Limit: 50})
	if err != nil || len(list.Notes) != 1 || !slices.Equal(list.Notes[0].Marks, []string{"star"}) {
		t.Errorf("List = %+v, %v", list, err)
	}
	if _, err := svc.SaveMarkNames(person, &gennotes.SaveMarkNamesPayload{Names: map[string]string{"star": "あとで", "red": ""}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"mark:star", "mark:あとで", "mark:STAR 本文"} {
		res, err := svc.Search(person, &gennotes.SearchPayload{Q: q})
		if err != nil || res.Total != 1 {
			t.Errorf("Search(%q) = %+v, %v", q, res, err)
		}
	}
	for _, q := range []string{"mark:red", "mark:nothing"} {
		if res, err := svc.Search(person, &gennotes.SearchPayload{Q: q}); err != nil || res.Total != 0 {
			t.Errorf("Search(%q) = %+v, %v", q, res, err)
		}
	}
	// The names are in the settings of the notes, and listed.
	c, err := svc.store.Config()
	if err != nil || c.Marks["star"] != "あとで" || len(c.Marks) != 1 {
		t.Errorf("Config().Marks = %v, %v", c.Marks, err)
	}
	marks, _ := svc.Marks(ai, nil)
	if len(marks) != len(store.MarkIDs) || marks[7].ID != "star" || marks[7].Kind != "symbol" || marks[7].Name == nil || *marks[7].Name != "あとで" {
		t.Errorf("Marks = %+v", marks[7])
	}
	// Taking it away, and what is refused.
	if got, err := svc.SetMarks(person, &gennotes.SetMarksPayload{ID: n.ID, Marks: []string{}}); err != nil || len(got.Marks) != 0 {
		t.Errorf("SetMarks([]) = %v, %v", got, err)
	}
	if _, err := svc.SetMarks(person, &gennotes.SetMarksPayload{ID: n.ID, Marks: []string{"pink"}}); errorName(err) != "bad_request" {
		t.Errorf("an unknown mark: %v", err)
	}
	if _, err := svc.SetMarks(person, &gennotes.SetMarksPayload{ID: "missing", Marks: []string{"red"}}); errorName(err) != "not_found" {
		t.Errorf("a missing note: %v", err)
	}
}

func TestUpdateAiNoteMarks(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	// Marks alone, with the version.
	got, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: n.Version, Marks: []string{"done"}})
	if err != nil || !slices.Equal(got.Marks, []string{"done"}) || got.UpdatedAt != n.UpdatedAt {
		t.Fatalf("UpdateAiNote(marks) = %+v, %v", got, err)
	}
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: n.Version, Marks: []string{"red"}}); errorName(err) != "precondition_failed" {
		t.Errorf("an old version: %v", err)
	}
	// With the body: both change.
	got, err = svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Body: ptr("本文"), Marks: []string{"idea"}})
	if err != nil || !slices.Equal(got.Marks, []string{"idea"}) || got.Body != "本文" {
		t.Errorf("UpdateAiNote(body, marks) = %+v, %v", got, err)
	}
	// A note of people is read-only for AI clients.
	mine, err := svc.Create(person, &gennotes.CreatePayload{Notebook: ptr("work"), Title: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: mine.ID, Version: mine.Version, Marks: []string{"red"}}); errorName(err) != "forbidden" {
		t.Errorf("a note of people: %v", err)
	}
}

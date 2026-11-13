package service

import (
	"context"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// A list filtered by q keeps its order and its separators, each before the
// next note listed, and highlights the titles.
func TestListFilter(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, n := range []struct{ id, title, tag string }{ // a comes first
		{"d", "pizza dinner", "todo"},
		{"c", "rain", "todo"},
		{"b", "pizza lunch", "renovate"},
		{"a", "pizza party", "todo"},
	} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(n.id), Notebook: ptr("work"), Title: n.title, Tags: []string{n.tag}}); err != nil {
			t.Fatal(err)
		}
	}
	sep, err := svc.CreateSeparator(person, &gennotes.CreateSeparatorPayload{Notebook: "work", After: ptr("a")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr("work"), Archived: "exclude", Limit: 50, Q: ptr("pizza -tag:renovate")})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range res.Notes {
		got = append(got, n.ID)
	}
	if res.Total != 2 || len(got) != 2 || got[0] != "a" || got[1] != "d" {
		t.Errorf("notes = %v (total %d), want a and d", got, res.Total)
	}
	if len(res.Notes) > 0 && len(res.Notes[0].TitleHighlights) != 1 {
		t.Errorf("title highlights of a = %v", res.Notes[0].TitleHighlights)
	}
	// b, then c are left out: the separator goes before d.
	if len(res.Separators) != 1 || res.Separators[0].ID != sep.ID || deref(res.Separators[0].Before) != "d" {
		t.Errorf("separators = %+v", res.Separators)
	}
}

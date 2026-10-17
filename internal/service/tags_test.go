package service

import (
	"context"
	"fmt"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestTags(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	for _, n := range []struct {
		id, notebook string
		tags         []string
	}{
		{"a", "work", []string{"Go", "検索"}},
		{"b", "work", []string{"api", "Go"}},
		{"c", "hobby", []string{"検索", "go"}},
		{"d", "secret", []string{"Go", "秘密"}},
		{"e", "inbox", []string{"Go", "メモ"}},
		{"f", "work", []string{"捨てた"}},
	} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(n.id), Notebook: ptr(n.notebook), Title: n.id, Tags: n.tags}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "secret", Kind: ptr("private")}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "f"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		ctx      context.Context
		notebook *string
		want     string
	}{
		// Sorted case-insensitively; the trash is left out.
		{"person", person, nil, "api:1 Go:4 go:1 メモ:1 検索:2 秘密:1"},
		{"person in a notebook", person, ptr("work"), "api:1 Go:2 検索:1"},
		{"person in the trash", person, ptr("trash"), "捨てた:1"},
		// AI clients do not see the tags of the private and drop notebooks.
		{"AI", ai, nil, "api:1 Go:2 go:1 検索:2"},
		{"AI in a private notebook", ai, ptr("secret"), ""},
		{"AI in a drop notebook", ai, ptr("inbox"), ""},
		{"AI in the trash", ai, ptr("trash"), ""},
	}
	for _, tt := range tests {
		res, err := svc.Tags(tt.ctx, &gennotes.TagsPayload{Notebook: tt.notebook})
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got := ""
		for i, tc := range res {
			if i > 0 {
				got += " "
			}
			got += fmt.Sprintf("%s:%d", tc.Name, tc.Count)
		}
		if got != tt.want {
			t.Errorf("%s: tags = %q, want %q", tt.name, got, tt.want)
		}
	}
}

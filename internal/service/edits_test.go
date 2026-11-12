package service

import (
	"context"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestUpdateAiNoteParts(t *testing.T) {
	ai := context.Background()
	svc, _ := newTestService(t)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "作業ログ", Body: ptr("- 一つ目\n- 二つ目\n- [ ] 試す\n- [ ] 試す（2）")})
	if err != nil {
		t.Fatal(err)
	}
	// The result of create leaves out the body, which the note has.
	if n.Body != "" || n.Version == "" {
		t.Errorf("create: body %q, version %q", n.Body, n.Version)
	}
	if cur, _ := svc.Get(ai, &gennotes.GetPayload{ID: n.ID}); !strings.HasPrefix(cur.Body, "- 一つ目\n") || cur.Version != n.Version {
		t.Errorf("after create: %q (version %s, result %s)", cur.Body, cur.Version, n.Version)
	}
	edit := func(old, new string) *gennotes.NoteEdit { return &gennotes.NoteEdit{Old: old, New: new} }

	// append, on a new line as the body does not end with one.
	got, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: n.Version, Append: ptr("- 三つ目\n")})
	if err != nil || got.Body != "- 一つ目\n- 二つ目\n- [ ] 試す\n- [ ] 試す（2）\n- 三つ目\n" {
		t.Fatalf("append: %q, %v", got.Body, err)
	}
	// edits, in order: the second sees the first.
	got, err = svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Edits: []*gennotes.NoteEdit{
		edit("- 二つ目\n", ""), edit("- 一つ目", "- 最初"),
	}})
	if err != nil || !strings.HasPrefix(got.Body, "- 最初\n- [ ] 試す\n") {
		t.Fatalf("edits: %q, %v", got.Body, err)
	}
	before := got.Body
	// All or none: a missing or repeated old text changes nothing.
	for name, edits := range map[string][]*gennotes.NoteEdit{
		"missing":  {edit("- 最初", "- 一"), edit("無い文", "x")},
		"repeated": {edit("- [ ] 試す", "- [x] 試す")}, // also in "試す（2）"
	} {
		if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Edits: edits}); errorName(err) != "bad_request" {
			t.Errorf("%s: %v, want bad_request", name, err)
		}
	}
	if cur, _ := svc.Get(ai, &gennotes.GetPayload{ID: n.ID}); cur.Body != before {
		t.Errorf("the body changed: %q", cur.Body)
	}
	// brief leaves the body out of the result, not out of the note.
	brief := true
	got, err = svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Append: ptr("- brief"), Brief: &brief})
	if err != nil || got.Body != "" || got.Version == "" {
		t.Fatalf("brief: %+v, %v", got, err)
	}
	if cur, _ := svc.Get(ai, &gennotes.GetPayload{ID: n.ID}); !strings.HasSuffix(cur.Body, "- brief") || cur.Version != got.Version {
		t.Errorf("after brief: %q (version %s, result %s)", cur.Body, cur.Version, got.Version)
	}
	// edits and append together: the edits first, then the text appended.
	got, err = svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Edits: []*gennotes.NoteEdit{edit("- brief", "- 短く")}, Append: ptr("- 四つ目")})
	if err != nil || !strings.HasSuffix(got.Body, "- 短く\n- 四つ目") {
		t.Fatalf("edits and append: %q, %v", got.Body, err)
	}
	// An edit that fails leaves out the text appended too.
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Edits: []*gennotes.NoteEdit{edit("無い文", "x")}, Append: ptr("- 五つ目")}); errorName(err) != "bad_request" {
		t.Errorf("a failed edit with append: %v", err)
	}
	if cur, _ := svc.Get(ai, &gennotes.GetPayload{ID: n.ID}); strings.Contains(cur.Body, "五つ目") {
		t.Errorf("appended after a failed edit: %q", cur.Body)
	}
	// body goes alone, and the version is checked.
	for name, p := range map[string]*gennotes.UpdateAiNotePayload{
		"body and append": {ID: n.ID, Version: got.Version, Body: ptr("x"), Append: ptr("y")},
		"body and edits":  {ID: n.ID, Version: got.Version, Body: ptr("x"), Edits: []*gennotes.NoteEdit{edit("- 短く", "x")}},
	} {
		if _, err := svc.UpdateAiNote(ai, p); errorName(err) != "bad_request" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: n.Version, Append: ptr("y")}); errorName(err) != "precondition_failed" {
		t.Errorf("an old version: %v", err)
	}
}

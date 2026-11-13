package service

import (
	"context"
	"slices"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

func noteIDs(ns []*gennotes.NoteSummary) []string {
	var ids []string
	for _, n := range ns {
		ids = append(ids, n.ID)
	}
	slices.Sort(ids)
	return ids
}

// intOf shows a number that may be absent.
func intOf(n *int) any {
	if n == nil {
		return "absent"
	}
	return *n
}

// Notes and notebooks in the archive are left out of lists, searches, tags
// and counts unless asked for, keep their notebooks, and open as before.
func TestArchive(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, n := range []struct{ id, nb string }{{"live", "work"}, {"put", "work"}, {"past", "past"}, {"sub", "past/old"}} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(n.id), Notebook: ptr(n.nb), Title: "apple", Tags: []string{"fruit"}}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := svc.Get(person, &gennotes.GetPayload{ID: "put"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"put"}, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "past", Archived: ptr(true)}); err != nil {
		t.Fatal(err)
	}

	list := func(nb, archived string) []string {
		t.Helper()
		var p *string
		if nb != "" {
			p = &nb
		}
		res, err := svc.List(person, &gennotes.ListPayload{Notebook: p, Archived: archived, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		return noteIDs(res.Notes)
	}
	search := func(q, archived string) []string {
		t.Helper()
		res, err := svc.Search(person, &gennotes.SearchPayload{Q: q, Archived: archived, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		return noteIDs(res.Notes)
	}
	for _, tt := range []struct {
		name string
		got  []string
		want []string
	}{
		{"list", list("", "exclude"), []string{"live"}},
		{"list of the notebook", list("work", "exclude"), []string{"live"}},
		{"list of the archive", list("", "only"), []string{"put"}}, // those of the notebooks there by their notebooks
		{"list of a notebook in the archive", list("past", "include"), []string{"past"}},
		{"list of a sub notebook of one in the archive", list("past/old", "include"), []string{"sub"}},
		{"list of a notebook in the archive, left out", list("past", "exclude"), nil},
		{"search", search("apple", "exclude"), []string{"live"}},
		{"search with the archive", search("apple", "include"), []string{"live", "past", "put", "sub"}},
		{"search of the archive", search("apple in:archive", "exclude"), []string{"past", "put", "sub"}},
	} {
		if !slices.Equal(tt.got, tt.want) {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
	// A search that leaves the archive out tells how many match there too;
	// one that takes it in does not.
	for _, tt := range []struct {
		q, archived string
		want        *int
	}{
		{"apple", "exclude", ptr(3)},
		{"apple", "include", nil},
		{"apple", "only", nil},
		{"apple in:archive", "exclude", nil},
		{"banana", "exclude", ptr(0)},
	} {
		res, err := svc.Search(person, &gennotes.SearchPayload{Q: tt.q, Archived: tt.archived, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if got := res.ArchivedTotal; (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
			t.Errorf("archived_total of %q (%s) = %v, want %v", tt.q, tt.archived, intOf(got), intOf(tt.want))
		}
	}
	tags, err := svc.Tags(person, &gennotes.TagsPayload{Archived: "exclude"})
	if err != nil || len(tags) != 1 || tags[0].Count != 1 {
		t.Errorf("Tags = %v, %v", tags, err)
	}
	nbs, err := svc.Notebooks(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, nb := range nbs {
		switch nb.ID {
		case "work":
			if nb.Count != 1 || nb.ArchivedCount != 1 || nb.Archived != nil {
				t.Errorf("work = %+v", nb)
			}
		case "past":
			if nb.ArchivedAt == nil || nb.Archived == nil || !*nb.Archived {
				t.Errorf("past = %+v", nb)
			}
		case "past/old":
			if nb.ArchivedAt != nil || nb.Archived == nil || !*nb.Archived {
				t.Errorf("past/old = %+v", nb)
			}
		}
	}
	// The note keeps its content and update time, and opens as before.
	got, err := svc.Get(person, &gennotes.GetPayload{ID: "put"})
	if err != nil || got.Archived == nil || got.ArchivedAt == nil || got.UpdatedAt != before.UpdatedAt || got.Notebook != "work" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if got, err := svc.Get(person, &gennotes.GetPayload{ID: "sub"}); err != nil || got.Archived == nil || got.ArchivedAt != nil {
		t.Errorf("Get of a note in the archive with its notebook = %+v, %v", got, err)
	}
	// Edited, it stays in the archive.
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: "put", Body: ptr("changed")}); err != nil {
		t.Fatal(err)
	}
	if got := list("", "only"); !slices.Equal(got, []string{"put"}) {
		t.Errorf("after an edit, the archive = %v", got)
	}
	// Taken out.
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"put"}, Archived: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "past", Archived: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if got := list("", "exclude"); !slices.Equal(got, []string{"live", "past", "put", "sub"}) {
		t.Errorf("taken out = %v", got)
	}
}

// archive_notes checks all the notes first, and the inbox and the trash
// cannot be put in the archive.
func TestArchiveNotesChecks(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("a"), Notebook: ptr("work"), Title: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("gone"), Title: "gone"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "gone"}); err != nil {
		t.Fatal(err)
	}
	for ids, want := range map[string]string{"missing": "not_found", "gone": "bad_request"} {
		if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"a", ids}, Archived: true}); errorName(err) != want {
			t.Errorf("%s: %v, want %s", ids, err, want)
		}
	}
	if n, _ := svc.Get(person, &gennotes.GetPayload{ID: "a"}); n.Archived != nil {
		t.Error("a note was put in the archive though another failed")
	}
	for _, nb := range []string{store.Inbox, store.Trash} {
		if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: nb, Archived: ptr(true)}); errorName(err) != "bad_request" {
			t.Errorf("%s: %v", nb, err)
		}
	}
}

// AI clients can search the archive, and put notes and notebooks of the
// collab kind there; private notes stay hidden there too.
func TestArchiveAI(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "apple"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ai, &gennotes.GetPayload{ID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: n.ID, Version: got.Version, Archived: ptr(true)})
	if err != nil || res.Archived == nil {
		t.Fatalf("UpdateAiNote = %+v, %v", res, err)
	}
	if found, _ := svc.Search(ai, &gennotes.SearchPayload{Q: "apple", Archived: "exclude", Limit: 20}); len(found.Notes) != 0 {
		t.Errorf("found in the archive: %v", noteIDs(found.Notes))
	}
	if found, _ := svc.Search(ai, &gennotes.SearchPayload{Q: "apple", Archived: "include", Limit: 20}); len(found.Notes) != 1 {
		t.Errorf("not found with the archive: %v", noteIDs(found.Notes))
	}

	if _, err := svc.ArchiveAiNotebook(ai, &gennotes.ArchiveAiNotebookPayload{ID: shared.ID, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if nb, err := svc.ArchiveAiNotebook(ai, &gennotes.ArchiveAiNotebookPayload{ID: shared.ID, Archived: false}); err != nil || nb.Archived != nil {
		t.Errorf("taken out = %+v, %v", nb, err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work", Kind: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ArchiveAiNotebook(ai, &gennotes.ArchiveAiNotebookPayload{ID: "work", Archived: true}); errorName(err) != "forbidden" {
		t.Errorf("a notebook of the default kind: %v", err)
	}
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ArchiveAiNotebook(ai, &gennotes.ArchiveAiNotebookPayload{ID: diary.ID, Archived: true}); errorName(err) != "not_found" {
		t.Errorf("a private notebook: %v", err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "apple"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"secret"}, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if found, _ := svc.Search(ai, &gennotes.SearchPayload{Q: "in:archive", Limit: 20}); slices.Contains(noteIDs(found.Notes), "secret") {
		t.Error("a private note in the archive is found by AI")
	}
	// Nor does the number of matches in the archive count it.
	if found, _ := svc.Search(ai, &gennotes.SearchPayload{Q: "apple", Archived: "exclude", Limit: 20}); found.ArchivedTotal == nil || *found.ArchivedTotal != 1 {
		t.Errorf("archived_total for AI = %v, want 1 (the collab note alone)", intOf(found.ArchivedTotal))
	}
}

// The candidates are the notes not updated for days, oldest first, but those
// in the archive and the trash.
func TestArchiveCandidates(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	svc.now = func() time.Time { return now }
	for i, id := range []string{"oldest", "older", "put", "gone", "fresh"} {
		now = start.AddDate(0, i, 0)
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	// The inbox waits for notes to go to notebooks, not to the archive.
	now = start
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("waiting"), Title: "waiting"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"put"}, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "gone"}); err != nil {
		t.Fatal(err)
	}
	now = start.AddDate(0, 5, 0)
	res, err := svc.ArchiveCandidates(person, &gennotes.ArchiveCandidatesPayload{Days: 60, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range res.Notes {
		ids = append(ids, n.ID)
	}
	if !slices.Equal(ids, []string{"oldest", "older"}) || res.Total != 2 {
		t.Errorf("ArchiveCandidates = %v (%d)", ids, res.Total)
	}
}

// The inbox holds no notes in the archive: they cannot be put there from it,
// a note in the archive moved there leaves the archive, and archived_at left
// there by an external edit is not taken, and goes when the note leaves.
func TestArchiveInbox(t *testing.T) {
	ctx := context.Background()
	person := AsPerson(ctx)
	svc, _ := newTestService(t)
	for _, id := range []string{"waiting", "old"} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: "waiting", Notebook: ptr(store.Inbox)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"old", "waiting"}, Archived: true}); errorName(err) != "bad_request" {
		t.Errorf("put in the archive from the inbox: %v", err)
	}
	if n, _ := svc.Get(person, &gennotes.GetPayload{ID: "old"}); n.Archived != nil {
		t.Error("a note was put in the archive though another failed")
	}

	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"old"}, Archived: true}); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.Update(person, &gennotes.UpdatePayload{ID: "old", Notebook: ptr(store.Inbox)})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Archived != nil || moved.ArchivedAt != nil {
		t.Errorf("moved to the inbox, still in the archive: %+v", moved)
	}
	if n, err := svc.store.Load("old"); err != nil || n.ArchivedAt != nil {
		t.Errorf("archived_at in the file of the inbox: %v, %v", n.ArchivedAt, err)
	}

	// archived_at written in the inbox outside ebira.
	n, err := svc.store.Load("waiting")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n.ArchivedAt = &at
	if err := svc.store.Save(n); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr(store.Inbox), Archived: "exclude", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got := noteIDs(list.Notes); !slices.Contains(got, "waiting") {
		t.Errorf("inbox = %v, want waiting there", got)
	}
	if got, _ := svc.Get(person, &gennotes.GetPayload{ID: "waiting"}); got.Archived != nil || got.ArchivedAt != nil {
		t.Errorf("waiting in the archive: %+v", got)
	}
	out, err := svc.Update(person, &gennotes.UpdatePayload{ID: "waiting", Notebook: ptr("work")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Archived != nil {
		t.Error("archived_at left in the inbox put the note in the archive when it went out")
	}
}

// The separators go before the notes listed: a note in the archive between
// them is passed over.
func TestArchiveSeparators(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, id := range []string{"c", "b", "a"} { // a comes first
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("work"), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	sep, err := svc.CreateSeparator(person, &gennotes.CreateSeparatorPayload{Notebook: "work", After: ptr("a")})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveNotes(person, &gennotes.ArchiveNotesPayload{Ids: []string{"b"}, Archived: true}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr("work"), Archived: "exclude", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Separators) != 1 || res.Separators[0].ID != sep.ID || deref(res.Separators[0].Before) != "c" {
		t.Errorf("separators = %+v", res.Separators)
	}
}

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	goa "goa.design/goa/v3/pkg"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
)

func newTestService(t *testing.T) (*Notes, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(context.Background(), filepath.Join(dir, "index.sqlite"), index.Config{Mode: index.ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	svc := New(st, ix)
	t.Cleanup(svc.Close)
	return svc, filepath.Join(dir, "notes")
}

func errorName(err error) string {
	var se *goa.ServiceError
	if errors.As(err, &se) {
		return se.Name
	}
	return ""
}

func ptr[T any](v T) *T { return &v }

func TestIfMatch(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, _ := newTestService(t)
	n, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("a"), Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	stale := n.Version

	n, err = svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("one"), IfMatch: &stale})
	if err != nil {
		t.Fatalf("Update with current version: %v", err)
	}
	if n.Version == stale {
		t.Fatal("version did not change")
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("two"), IfMatch: &stale}); errorName(err) != "precondition_failed" {
		t.Errorf("Update with stale version = %v, want precondition_failed", err)
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a", IfMatch: &stale}); errorName(err) != "precondition_failed" {
		t.Errorf("Delete with stale version = %v, want precondition_failed", err)
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("three")}); err != nil {
		t.Errorf("Update without If-Match: %v", err)
	}
	got, err := svc.Get(ctx, &gennotes.GetPayload{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a", IfMatch: &got.Version}); err != nil {
		t.Errorf("Delete with current version: %v", err)
	}
}

type fakeStream struct{ ch chan *gennotes.NoteEvent }

func (s *fakeStream) Send(ev *gennotes.NoteEvent) error { s.ch <- ev; return nil }
func (s *fakeStream) SendWithContext(_ context.Context, ev *gennotes.NoteEvent) error {
	return s.Send(ev)
}
func (s *fakeStream) Close() error { return nil }

func TestEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(AsPerson(context.Background()))
	defer cancel()
	svc, dir := newTestService(t)

	stream := &fakeStream{ch: make(chan *gennotes.NoteEvent, 16)}
	done := make(chan error, 1)
	go func() { done <- svc.Events(ctx, nil, stream) }()

	next := func() string {
		t.Helper()
		select {
		case ev := <-stream.ch:
			if ev.ID == nil {
				return ev.Type
			}
			return ev.Type + " " + *ev.ID
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
			return ""
		}
	}
	if got := next(); got != "ready" {
		t.Fatalf("first event = %q, want ready", got)
	}

	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("a"), Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	// A file added outside the API (e.g. by git pull) is reported by Sync.
	if err := os.WriteFile(filepath.Join(dir, "inbox", "b.yaml"), []byte("title: B\nupdated_at: 2026-10-15T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"created a", "updated a", "created b", "updated a"} { // delete moves a to the trash
		if got := next(); got != want {
			t.Errorf("event = %q, want %q", got, want)
		}
	}

	// Close ends the stream.
	svc.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Events = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events did not return after Close")
	}
}

func TestTrash(t *testing.T) {
	ctx := AsPerson(context.Background()) // people see the trash
	svc, _ := newTestService(t)
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("a"), Notebook: ptr("work"), Title: "A"}); err != nil {
		t.Fatal(err)
	}
	notebookOf := func(id string) string {
		t.Helper()
		n, err := svc.Get(ctx, &gennotes.GetPayload{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return n.Notebook
	}
	countIn := func(nb string) int {
		t.Helper()
		res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr(nb), Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return len(res.Notes)
	}

	// The first delete moves the note to the trash and remembers where it was.
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Get(ctx, &gennotes.GetPayload{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Notebook != "trash" || n.TrashedFrom == nil || *n.TrashedFrom != "work" {
		t.Fatalf("after delete: notebook %q, trashed_from %v", n.Notebook, n.TrashedFrom)
	}
	if all, err := svc.List(ctx, &gennotes.ListPayload{Limit: 10}); err != nil || len(all.Notes) != 0 {
		t.Errorf("trashed note listed without a notebook filter: %+v, %v", all, err)
	}
	if countIn("trash") != 1 {
		t.Error("trashed note not listed in the trash")
	}

	// Restore moves it back.
	n, err = svc.Restore(ctx, &gennotes.RestorePayload{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Notebook != "work" || n.TrashedFrom != nil {
		t.Fatalf("after restore: notebook %q, trashed_from %v", n.Notebook, n.TrashedFrom)
	}
	if _, err := svc.Restore(ctx, &gennotes.RestorePayload{ID: "a"}); errorName(err) != "bad_request" {
		t.Errorf("Restore of a note not in the trash = %v, want bad_request", err)
	}

	// Moving between notebooks, but not into the trash.
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Notebook: ptr("home")}); err != nil {
		t.Fatal(err)
	}
	if nb := notebookOf("a"); nb != "home" {
		t.Errorf("notebook after move = %q, want home", nb)
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Notebook: ptr("trash")}); errorName(err) != "bad_request" {
		t.Errorf("Update into the trash = %v, want bad_request", err)
	}
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{Notebook: ptr("trash"), Title: "X"}); errorName(err) != "bad_request" {
		t.Errorf("Create in the trash = %v, want bad_request", err)
	}

	// Deleting a note in the trash deletes it permanently.
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, &gennotes.GetPayload{ID: "a"}); errorName(err) != "not_found" {
		t.Errorf("Get after permanent delete = %v, want not_found", err)
	}

	// Notebooks: inbox first, trash last, empty notebooks included.
	nbs, err := svc.Notebooks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, nb := range nbs {
		names = append(names, nb.ID)
	}
	if want := []string{"inbox", "home", "work", "trash"}; !slices.Equal(names, want) {
		t.Errorf("Notebooks = %v, want %v", names, want)
	}
}

func TestSaveNotebook(t *testing.T) {
	ctx, cancel := context.WithCancel(AsPerson(context.Background()))
	defer cancel()
	svc, dir := newTestService(t)
	if _, err := svc.Sync(ctx, nil); err != nil { // records the initial notebook state
		t.Fatal(err)
	}
	stream := &fakeStream{ch: make(chan *gennotes.NoteEvent, 16)}
	go svc.Events(ctx, nil, stream)
	<-stream.ch // ready

	names := func() []string {
		t.Helper()
		nbs, err := svc.Notebooks(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var r []string
		for _, nb := range nbs {
			r = append(r, nb.ID+"="+nb.Name)
		}
		return r
	}
	expectEvent := func() {
		t.Helper()
		select {
		case ev := <-stream.ch:
			if ev.Type != "notebooks" {
				t.Errorf("event = %s, want notebooks", ev.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no notebooks event")
		}
	}

	// Saving creates empty notebooks; without an order they sort by name.
	for _, id := range []string{"work", "home"} {
		if _, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: id}); err != nil {
			t.Fatal(err)
		}
		expectEvent()
	}
	if got, want := names(), []string{"inbox=Inbox", "home=home", "work=work", "trash=Trash"}; !slices.Equal(got, want) {
		t.Errorf("notebooks = %v, want %v", got, want)
	}

	// Display names and order.
	if _, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: "work", Name: ptr("仕事"), Order: ptr(-1)}); err != nil {
		t.Fatal(err)
	}
	expectEvent()
	if _, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: "inbox", Name: ptr("受信箱")}); err != nil {
		t.Fatal(err)
	}
	expectEvent()
	if got, want := names(), []string{"inbox=受信箱", "work=仕事", "home=home", "trash=Trash"}; !slices.Equal(got, want) {
		t.Errorf("notebooks = %v, want %v", got, want)
	}
	if _, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: "trash", Order: ptr(1)}); errorName(err) != "bad_request" {
		t.Errorf("ordering the trash = %v, want bad_request", err)
	}

	// An edit of _notebook.yaml outside the API (e.g. git pull) is noticed by Sync.
	if err := os.WriteFile(filepath.Join(dir, "home", "_notebook.yaml"), []byte("name: 家\norder: -5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	expectEvent()
	if got, want := names(), []string{"inbox=受信箱", "home=家", "work=仕事", "trash=Trash"}; !slices.Equal(got, want) {
		t.Errorf("notebooks = %v, want %v", got, want)
	}
}

func TestGetHighlights(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, _ := newTestService(t)
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("a"), Title: "東京の天気", Body: ptr("京都\n\n東京都")}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Get(ctx, &gennotes.GetPayload{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if n.TitleHighlights != nil || n.BodyHighlights != nil {
		t.Errorf("highlights without q: %v, %v", n.TitleHighlights, n.BodyHighlights)
	}
	n, err = svc.Get(ctx, &gennotes.GetPayload{ID: "a", Q: ptr("京都 天気")})
	if err != nil {
		t.Fatal(err)
	}
	ranges := func(rs []*gennotes.TextRange) (res [][2]int) {
		for _, r := range rs {
			res = append(res, [2]int{r.Start, r.End})
		}
		return res
	}
	if got, want := ranges(n.TitleHighlights), [][2]int{{3, 5}}; !slices.Equal(got, want) {
		t.Errorf("title highlights = %v, want %v", got, want)
	}
	// The body is searched to its end, across lines.
	if got, want := ranges(n.BodyHighlights), [][2]int{{0, 2}, {5, 7}}; !slices.Equal(got, want) {
		t.Errorf("body highlights = %v, want %v", got, want)
	}
}

func TestCreateNotebook(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, dir := newTestService(t)

	nb, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: " 開発メモ "})
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID != "xn--4dkc0654c57za" || nb.Name != "開発メモ" || nb.Order != 10 {
		t.Errorf("created %+v", *nb)
	}
	b, err := os.ReadFile(filepath.Join(dir, nb.ID, store.NotebookFile))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "name: 開発メモ\norder: 10\n"; got != want {
		t.Errorf("_notebook.yaml = %q, want %q", got, want)
	}

	// The next one goes after it; an ASCII name keeps a readable ID.
	nb2, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: "My Books"})
	if err != nil {
		t.Fatal(err)
	}
	if nb2.ID != "my-books" || nb2.Order != 20 {
		t.Errorf("created %+v", *nb2)
	}

	// The same display name is rejected, also against the default names.
	for _, name := range []string{"開発メモ", "my books", "Inbox"} {
		if _, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: name}); errorName(err) != "already_exists" {
			t.Errorf("CreateNotebook(%q) = %v, want already_exists", name, err)
		}
	}
	// A different name with the same ID gets a suffix.
	nb3, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: "my-books!"})
	if err != nil {
		t.Fatal(err)
	}
	if nb3.ID != "my-books-2" {
		t.Errorf("ID = %q, want my-books-2", nb3.ID)
	}
	// A name that would give a reserved ID does not take it.
	nb4, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: "trash!"})
	if err != nil {
		t.Fatal(err)
	}
	if nb4.ID != "trash-2" || nb4.Reserved {
		t.Errorf("created %+v", *nb4)
	}

	// Notes can be created in it by ID.
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{Notebook: &nb.ID, Title: "メモ"}); err != nil {
		t.Fatal(err)
	}
}

// TestNotebookNameFromID checks that a notebook without a display name in
// _notebook.yaml shows the name restored from its Punycode ID.
func TestNotebookNameFromID(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, dir := newTestService(t)
	nb, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: "開発メモ"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, nb.ID, store.NotebookFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	nbs, err := svc.Notebooks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, nb := range nbs {
		names[nb.ID] = nb.Name
	}
	if got := names[nb.ID]; got != "開発メモ" {
		t.Errorf("name of %s = %q, want 開発メモ", nb.ID, got)
	}
	if got := names["plain"]; got != "plain" {
		t.Errorf("name of plain = %q, want plain", got)
	}
	// Resetting the name also falls back to the restored one.
	empty := ""
	res, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: nb.ID, Name: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "開発メモ" {
		t.Errorf("name after reset = %q", res.Name)
	}
}

// TestAINotes checks that AI clients may write notes only in notebooks of the
// collab kind, and only with the current version.
func TestAINotes(t *testing.T) {
	ctx := context.Background() // AI clients: requests without the mark of people
	person := AsPerson(ctx)
	svc, dir := newTestService(t)
	ai, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "AI と調べもの", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	if ai.Kind != "collab" {
		t.Fatalf("kind = %q, want ai", ai.Kind)
	}
	b, err := os.ReadFile(filepath.Join(dir, ai.ID, store.NotebookFile))
	if err != nil || !strings.Contains(string(b), "kind: collab\n") {
		t.Errorf("_notebook.yaml = %q, %v", b, err)
	}
	ai2, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "AI 2", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	people, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "people"})
	if err != nil {
		t.Fatal(err)
	}
	if people.Kind != "default" {
		t.Errorf("kind = %q, want default", people.Kind)
	}
	human, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("human"), Notebook: &people.ID, Title: "人のメモ"})
	if err != nil {
		t.Fatal(err)
	}

	// Creating: only in a collab notebook (or a drop one, such as the inbox;
	// see TestDropNotebooks).
	for _, nb := range []string{people.ID, store.Trash, "missing"} {
		if _, err := svc.CreateAiNote(ctx, &gennotes.CreateAiNotePayload{Notebook: nb, Title: "x"}); errorName(err) != "forbidden" {
			t.Errorf("CreateAiNote in %s = %v, want forbidden", nb, err)
		}
	}
	n, err := svc.CreateAiNote(ctx, &gennotes.CreateAiNotePayload{ID: ptr("research"), Notebook: ai.ID, Title: "調査", Body: ptr("結果")})
	if err != nil {
		t.Fatal(err)
	}

	// Updating: the version is required, and notes of people are read-only.
	if _, err := svc.UpdateAiNote(ctx, &gennotes.UpdateAiNotePayload{ID: "research", Version: `"stale"`, Body: ptr("x")}); errorName(err) != "precondition_failed" {
		t.Errorf("UpdateAiNote with a stale version = %v, want precondition_failed", err)
	}
	if _, err := svc.UpdateAiNote(ctx, &gennotes.UpdateAiNotePayload{ID: "human", Version: human.Version, Body: ptr("x")}); errorName(err) != "forbidden" {
		t.Errorf("UpdateAiNote of a note of people = %v, want forbidden", err)
	}
	if _, err := svc.UpdateAiNote(ctx, &gennotes.UpdateAiNotePayload{ID: "research", Version: n.Version, Notebook: &people.ID}); errorName(err) != "forbidden" {
		t.Errorf("UpdateAiNote moving out of the collab notebooks = %v, want forbidden", err)
	}
	n, err = svc.UpdateAiNote(ctx, &gennotes.UpdateAiNotePayload{ID: "research", Version: n.Version, Body: ptr("結果 2"), Notebook: &ai2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "結果 2" || n.Notebook != ai2.ID {
		t.Errorf("updated %+v", *n)
	}

	// Deleting: moves to the trash, after which the note is read-only for AI.
	if err := svc.DeleteAiNote(ctx, &gennotes.DeleteAiNotePayload{ID: "human", Version: human.Version}); errorName(err) != "forbidden" {
		t.Errorf("DeleteAiNote of a note of people = %v, want forbidden", err)
	}
	if err := svc.DeleteAiNote(ctx, &gennotes.DeleteAiNotePayload{ID: "research", Version: n.Version}); err != nil {
		t.Fatal(err)
	}
	trashed, err := svc.Get(person, &gennotes.GetPayload{ID: "research"})
	if err != nil || trashed.Notebook != store.Trash {
		t.Fatalf("after DeleteAiNote: %+v, %v", trashed, err)
	}
	if err := svc.DeleteAiNote(ctx, &gennotes.DeleteAiNotePayload{ID: "research", Version: trashed.Version}); errorName(err) != "not_found" {
		t.Errorf("DeleteAiNote in the trash = %v, want not_found: the trash is hidden from AI clients", err)
	}

	// The kind can be changed, but not for inbox and trash.
	def := "default"
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: ai2.ID, Kind: &def}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAiNote(ctx, &gennotes.CreateAiNotePayload{Notebook: ai2.ID, Title: "x"}); errorName(err) != "forbidden" {
		t.Errorf("CreateAiNote after the kind was reset = %v, want forbidden", err)
	}
	aiKind := "collab"
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: store.Inbox, Kind: &aiKind}); errorName(err) != "bad_request" {
		t.Errorf("making inbox ai = %v, want bad_request", err)
	}
}

// TestPrivateNotebooks checks that AI clients (requests without the mark of
// people) cannot see private notebooks, their notes or the trash, and that they
// may create collab notebooks and make notebooks private.
func TestPrivateNotebooks(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	// The inbox, which is drop unless people change it, is made default so
	// that AI clients read its notes.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: store.Inbox, Kind: ptr("default")}); err != nil {
		t.Fatal(err)
	}
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "日記", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "秘密", Body: ptr("合言葉はひまわり")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("open"), Title: "公開", Body: ptr("ひまわりの種")}); err != nil {
		t.Fatal(err)
	}
	trashed, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("old"), Title: "ゴミ", Body: ptr("ひまわり")})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "old", IfMatch: &trashed.Version}); err != nil {
		t.Fatal(err)
	}

	ids := func(ns []*gennotes.NoteSummary) (res []string) {
		for _, n := range ns {
			res = append(res, n.ID)
		}
		slices.Sort(res)
		return res
	}
	nbIDs := func(ctx context.Context) (res []string) {
		nbs, err := svc.Notebooks(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, nb := range nbs {
			res = append(res, nb.ID)
		}
		return res
	}
	if got := nbIDs(ai); slices.Contains(got, diary.ID) || slices.Contains(got, store.Trash) {
		t.Errorf("notebooks for AI = %v, want neither %s nor trash", got, diary.ID)
	}
	if got := nbIDs(person); !slices.Contains(got, diary.ID) || !slices.Contains(got, store.Trash) {
		t.Errorf("notebooks for people = %v, want %s and trash", got, diary.ID)
	}
	for _, nb := range []*string{nil, &diary.ID, ptr(store.Trash)} {
		ls, err := svc.List(ai, &gennotes.ListPayload{Notebook: nb, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		sr, err := svc.Search(ai, &gennotes.SearchPayload{Q: "ひまわり", Notebook: nb, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		want := []string(nil)
		if nb == nil {
			want = []string{"open"}
		}
		if got := ids(ls.Notes); !slices.Equal(got, want) || ls.Total != len(want) {
			t.Errorf("List for AI in %v = %v, want %v", deref(nb), got, want)
		}
		if got := ids(sr.Notes); !slices.Equal(got, want) || sr.Total != len(want) {
			t.Errorf("Search for AI in %v = %v (total %d), want %v", deref(nb), got, sr.Total, want)
		}
	}
	if sr, _ := svc.Search(person, &gennotes.SearchPayload{Q: "ひまわり", Limit: 20}); !slices.Equal(ids(sr.Notes), []string{"open", "secret"}) {
		t.Errorf("Search for people = %v, want [open secret]", ids(sr.Notes))
	}
	for _, id := range []string{"secret", "old"} {
		if _, err := svc.Get(ai, &gennotes.GetPayload{ID: id}); errorName(err) != "not_found" {
			t.Errorf("Get(%s) for AI = %v, want not_found", id, err)
		}
	}

	// AI clients may create a collab notebook; a private name does not block it.
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "日記"})
	if err != nil {
		t.Fatal(err)
	}
	if shared.Kind != "collab" || shared.ID == diary.ID {
		t.Errorf("created %+v", *shared)
	}
	// ... and make a notebook they can see private, but not one hidden from them.
	if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: shared.ID}); err != nil {
		t.Fatal(err)
	}
	if got := nbIDs(ai); slices.Contains(got, shared.ID) {
		t.Errorf("notebooks for AI after making %s private = %v", shared.ID, got)
	}
	for _, id := range []string{diary.ID, shared.ID, store.Trash, "missing"} {
		if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: id}); errorName(err) != "not_found" {
			t.Errorf("MakeNotebookPrivate(%s) for AI = %v, want not_found", id, err)
		}
	}
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "x"}); errorName(err) != "forbidden" {
		t.Errorf("CreateAiNote in a private notebook = %v, want forbidden", err)
	}

	// The REST endpoints are from people without passkeys (auth: none). With
	// passkeys, only a request with a valid session passes, as from people;
	// one without a session or with an invalid one fails. The endpoints of
	// Notes itself, which MCP uses, are from AI whatever the session.
	sees := func(e *gennotes.Endpoints, session *string) (bool, error) {
		res, err := e.Notebooks(ai, &gennotes.NotebooksPayload{Session: session})
		if err != nil {
			return false, err
		}
		for _, nb := range res.([]*gennotes.Notebook) {
			if nb.ID == diary.ID {
				return true, nil
			}
		}
		return false, nil
	}
	valid := func(s string) (bool, bool) { return s == "good", false }
	for _, c := range []struct {
		name    string
		svc     gennotes.Service
		session *string
		want    bool
		err     string
	}{
		{"none", REST{Notes: svc}, nil, true, ""},
		{"none with a session", REST{Notes: svc}, ptr("bad"), true, ""},
		{"passkey without a session", REST{Notes: svc, Session: valid}, nil, false, "unauthorized"},
		{"passkey with a valid session", REST{Notes: svc, Session: valid}, ptr("good"), true, ""},
		{"passkey with an invalid session", REST{Notes: svc, Session: valid}, ptr("bad"), false, "unauthorized"},
		{"MCP with a session", svc, ptr("good"), false, ""},
	} {
		got, err := sees(gennotes.NewEndpoints(c.svc), c.session)
		if errorName(err) != c.err || (c.err == "" && err != nil) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.err)
		} else if got != c.want {
			t.Errorf("%s: sees the private notebook: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestTrashNotebook checks moving a notebook into the trash as a whole, and
// restoring or deleting it, or single notes of it.
func TestTrashNotebook(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "日記", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: &diary.ID, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	listIDs := func(p *gennotes.ListPayload) (res []string) {
		p.Limit = 50
		ns, err := svc.List(person, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range ns.Notes {
			res = append(res, n.ID)
		}
		slices.Sort(res)
		return res
	}
	trashed := func() map[string]*gennotes.TrashedNotebook {
		nbs, err := svc.TrashedNotebooks(person, nil)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]*gennotes.TrashedNotebook{}
		for _, nb := range nbs {
			m[nb.ID] = nb
		}
		return m
	}

	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: store.Inbox}); errorName(err) != "bad_request" {
		t.Errorf("deleting inbox = %v, want bad_request", err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, store.Trash, diary.ID, store.NotebookFile)); err != nil {
		t.Errorf("the settings did not move into the trash: %v", err)
	}
	if nb := trashed()[diary.ID]; nb == nil || nb.Name != "日記" || nb.Kind != "private" || nb.Count != 2 {
		t.Errorf("trashed notebook = %+v", nb)
	}
	if got := listIDs(&gennotes.ListPayload{Notebook: ptr(store.Trash)}); len(got) != 0 {
		t.Errorf("notes deleted on their own = %v, want none", got)
	}
	if got := listIDs(&gennotes.ListPayload{Notebook: ptr(store.Trash), TrashedNotebook: &diary.ID}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("notes of the trashed notebook = %v", got)
	}
	n, err := svc.Get(person, &gennotes.GetPayload{ID: "a"})
	if err != nil || n.Notebook != store.Trash || deref(n.TrashedNotebook) != diary.ID {
		t.Fatalf("Get(a) = %+v, %v", n, err)
	}
	if nbs, _ := svc.TrashedNotebooks(ai, nil); len(nbs) != 0 {
		t.Errorf("AI clients see the trashed notebooks: %v", nbs)
	}

	// Restoring a single note recreates the notebook with its kind.
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if m, err := svc.store.NotebookMeta(diary.ID); err != nil || m.Kind != store.KindPrivate || m.Name != "日記" {
		t.Errorf("recreated notebook = %+v, %v", m, err)
	}
	if _, err := svc.Get(ai, &gennotes.GetPayload{ID: "a"}); errorName(err) != "not_found" {
		t.Errorf("Get(a) for AI after restoring = %v, want not_found: the note must stay private", err)
	}
	if nb := trashed()[diary.ID]; nb == nil || nb.Count != 1 {
		t.Errorf("trashed notebook after restoring a = %+v", nb)
	}

	// The same ID cannot be in the trash and outside twice.
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: diary.ID}); errorName(err) != "already_exists" {
		t.Errorf("deleting while the trash holds the same ID = %v, want already_exists", err)
	}
	if _, err := svc.RestoreNotebook(person, &gennotes.RestoreNotebookPayload{ID: diary.ID}); errorName(err) != "already_exists" {
		t.Errorf("restoring while the notebook exists = %v, want already_exists", err)
	}

	// Deleting a note of a trashed notebook deletes it permanently.
	b, _ := svc.Get(person, &gennotes.GetPayload{ID: "b"})
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "b", IfMatch: &b.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(person, &gennotes.GetPayload{ID: "b"}); errorName(err) != "not_found" {
		t.Errorf("b after deleting it in the trash = %v", err)
	}
	if err := svc.DeleteTrashedNotebook(person, &gennotes.DeleteTrashedNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}
	if len(trashed()) != 0 {
		t.Errorf("trashed notebooks after deleting = %v", trashed())
	}

	// A whole notebook comes back with its notes and kind.
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}
	nb, err := svc.RestoreNotebook(person, &gennotes.RestoreNotebookPayload{ID: diary.ID})
	if err != nil {
		t.Fatal(err)
	}
	if nb.Kind != "private" || nb.Count != 1 {
		t.Errorf("restored notebook = %+v", *nb)
	}

	// A note does not go back into a notebook of another kind.
	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("w"), Notebook: &work.ID, Title: "w"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: work.ID}); err != nil {
		t.Fatal(err)
	}
	private := "private"
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: work.ID, Kind: &private}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "w"}); errorName(err) != "bad_request" {
		t.Errorf("restoring into a notebook of another kind = %v, want bad_request", err)
	}
}

// A note of a notebook in the trash can be moved to another notebook on its
// own, which takes it out of the trash and leaves the notebook there.
func TestMoveNoteOutOfTrashedNotebook(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, id := range []string{"a", "b"} {
		if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr("old"), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("c"), Notebook: ptr("work"), Title: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(ctx, &gennotes.DeleteNotebookPayload{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Notebook: ptr("work")})
	if err != nil {
		t.Fatal(err)
	}
	if n.Notebook != "work" || n.TrashedNotebook != nil || n.TrashedFrom != nil {
		t.Errorf("moved note = %+v", n)
	}
	trashed, err := svc.TrashedNotebooks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0].ID != "old" || trashed[0].Count != 1 {
		t.Errorf("trashed notebooks = %+v, want old with 1 note", trashed)
	}
	list, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr("work"), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Errorf("work has %d notes, want 2", list.Total)
	}
}

func TestEmptyTrash(t *testing.T) {
	person := AsPerson(context.Background())
	svc, dir := newTestService(t)
	old, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "old"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*gennotes.CreatePayload{
		{ID: ptr("kept"), Title: "kept"},
		{ID: ptr("single"), Title: "single"},
		{ID: ptr("inside"), Notebook: &old.ID, Title: "inside"},
	} {
		if _, err := svc.Create(person, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: "single", Name: "a.png"}, body(pngHead)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "single"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: old.ID}); err != nil {
		t.Fatal(err)
	}
	// A file ebira does not know stays.
	other := filepath.Join(dir, store.Trash, "README.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.EmptyTrash(person, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"single", "inside"} {
		if _, err := svc.Get(person, &gennotes.GetPayload{ID: id}); errorName(err) != "not_found" {
			t.Errorf("Get(%s) after emptying the trash = %v, want not_found", id, err)
		}
	}
	if _, err := svc.Get(person, &gennotes.GetPayload{ID: "kept"}); err != nil {
		t.Errorf("a note outside the trash is gone: %v", err)
	}
	if nbs, err := svc.TrashedNotebooks(person, nil); err != nil || len(nbs) != 0 {
		t.Errorf("trashed notebooks = %v, %v; want none", nbs, err)
	}
	if _, err := os.Stat(filepath.Join(dir, store.Trash, "single"+store.AssetsSuffix)); !os.IsNotExist(err) {
		t.Errorf("the files of a deleted note are left: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file ebira does not know is gone: %v", err)
	}
	// Nothing to delete is no error.
	if err := svc.EmptyTrash(person, nil); err != nil {
		t.Errorf("emptying an empty trash = %v", err)
	}
}

// TestPeopleOnly checks that the methods only people may call refuse
// requests from AI: they are not MCP tools, and the REST API refuses them
// without a session while passkeys are required, but they check the mark
// themselves too. A private notebook stays private.
func TestPeopleOnly(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("old"), Title: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: other.ID}); err != nil {
		t.Fatal(err)
	}

	calls := map[string]func(context.Context) error{
		"create": func(ctx context.Context) error {
			_, err := svc.Create(ctx, &gennotes.CreatePayload{Notebook: &diary.ID, Title: "x"})
			return err
		},
		"update": func(ctx context.Context) error {
			_, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: n.ID, Body: ptr("changed")})
			return err
		},
		"delete": func(ctx context.Context) error { return svc.Delete(ctx, &gennotes.DeletePayload{ID: n.ID}) },
		"restore": func(ctx context.Context) error {
			_, err := svc.Restore(ctx, &gennotes.RestorePayload{ID: "old"})
			return err
		},
		"move": func(ctx context.Context) error {
			_, err := svc.Move(ctx, &gennotes.MovePayload{ID: n.ID})
			return err
		},
		"save_notebook": func(ctx context.Context) error {
			_, err := svc.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: diary.ID, Kind: ptr("default")})
			return err
		},
		"create_notebook": func(ctx context.Context) error {
			_, err := svc.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: "x", Kind: "default"})
			return err
		},
		"delete_notebook": func(ctx context.Context) error {
			return svc.DeleteNotebook(ctx, &gennotes.DeleteNotebookPayload{ID: diary.ID})
		},
		"move_notes": func(ctx context.Context) error {
			return svc.MoveNotes(ctx, &gennotes.MoveNotesPayload{Ids: []string{n.ID}, Notebook: store.Inbox})
		},
		"trash_notes": func(ctx context.Context) error {
			return svc.TrashNotes(ctx, &gennotes.TrashNotesPayload{Ids: []string{n.ID}})
		},
		"move_notebook": func(ctx context.Context) error {
			_, err := svc.MoveNotebook(ctx, &gennotes.MoveNotebookPayload{ID: diary.ID, Parent: ptr("inbox")})
			return err
		},
		"restore_notebook": func(ctx context.Context) error {
			_, err := svc.RestoreNotebook(ctx, &gennotes.RestoreNotebookPayload{ID: other.ID})
			return err
		},
		"delete_trashed_notebook": func(ctx context.Context) error {
			return svc.DeleteTrashedNotebook(ctx, &gennotes.DeleteTrashedNotebookPayload{ID: other.ID})
		},
		"empty_trash": func(ctx context.Context) error { return svc.EmptyTrash(ctx, nil) },
		"import": func(ctx context.Context) error {
			_, err := svc.Import(ctx, &gennotes.ImportPayload{Notebook: &diary.ID, Name: "x.md", Content: "x"})
			return err
		},
		"preview_style": func(ctx context.Context) error {
			_, err := svc.PreviewStyle(ctx, nil)
			return err
		},
		"set_preview_style": func(ctx context.Context) error {
			_, err := svc.SetPreviewStyle(ctx, &gennotes.SetPreviewStylePayload{Style: "band"})
			return err
		},
	}
	for name, call := range calls {
		if err := call(ai); errorName(err) != "unauthorized" {
			t.Errorf("%s for AI = %v, want unauthorized", name, err)
		}
	}

	// Through the REST API without a session while passkeys are required.
	rest := gennotes.NewEndpoints(REST{Notes: svc, Session: func(string) (bool, bool) { return false, false }})
	if _, err := rest.SaveNotebook(ai, &gennotes.SaveNotebookPayload{ID: diary.ID, Kind: ptr("default")}); errorName(err) != "unauthorized" {
		t.Errorf("REST save_notebook without a session = %v, want unauthorized", err)
	}

	got, err := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	if err != nil || got.Notebook != diary.ID || got.Body != "" {
		t.Errorf("the private note changed: %+v, %v", got, err)
	}
	nbs, err := svc.Notebooks(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, nb := range nbs {
		if nb.ID == diary.ID && nb.Kind != "private" {
			t.Errorf("the private notebook became %s", nb.Kind)
		}
	}
	if trashed, _ := svc.TrashedNotebooks(person, nil); len(trashed) != 1 {
		t.Errorf("trashed notebooks = %v, want the one deleted", trashed)
	}
}

// TestVerification checks that important operations ask for a session that
// signed in with a passkey recently, while passkeys are required: making a
// private notebook visible to AI, reading the history of a note and emptying
// the trash. Other operations, and every operation without passkeys, pass.
func TestVerification(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("a"), Notebook: &work.ID, Title: "a"}); err != nil {
		t.Fatal(err)
	}
	vault, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "vault", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	endpoints := func(verified bool) *gennotes.Endpoints {
		return gennotes.NewEndpoints(REST{Notes: svc, Session: func(string) (bool, bool) { return true, verified }})
	}
	s := ptr("session")
	calls := map[string]func(*gennotes.Endpoints) error{
		"save_notebook from private": func(e *gennotes.Endpoints) error {
			_, err := e.SaveNotebook(ai, &gennotes.SaveNotebookPayload{Session: s, ID: diary.ID, Kind: ptr("default")})
			return err
		},
		"history": func(e *gennotes.Endpoints) error {
			_, err := e.History(ai, &gennotes.HistoryPayload{Session: s, ID: "a"})
			return err
		},
		"history_version": func(e *gennotes.Endpoints) error {
			_, err := e.HistoryVersion(ai, &gennotes.HistoryVersionPayload{Session: s, ID: "a", Rev: "1"})
			return err
		},
		"empty_trash": func(e *gennotes.Endpoints) error {
			_, err := e.EmptyTrash(ai, &gennotes.EmptyTrashPayload{Session: s})
			return err
		},
		"move_notebook out of private": func(e *gennotes.Endpoints) error {
			_, err := e.MoveNotebook(ai, &gennotes.MoveNotebookPayload{Session: s, ID: vault.ID, Parent: &work.ID})
			return err
		},
	}
	for name, call := range calls {
		if err := call(endpoints(false)); errorName(err) != "verification_required" {
			t.Errorf("%s without a verification = %v, want verification_required", name, err)
		}
		if err := call(endpoints(true)); errorName(err) == "verification_required" {
			t.Errorf("%s with a verification = %v", name, err)
		}
	}
	// Other operations do not ask for it.
	if _, err := endpoints(false).SaveNotebook(ai, &gennotes.SaveNotebookPayload{Session: s, ID: work.ID, Kind: ptr("collab")}); err != nil {
		t.Errorf("save_notebook of another kind: %v", err)
	}
	if _, err := endpoints(false).SaveNotebook(ai, &gennotes.SaveNotebookPayload{Session: s, ID: work.ID, Kind: ptr("private")}); err != nil {
		t.Errorf("making a notebook private: %v", err)
	}
	// Without passkeys (auth: none), nothing asks for it.
	none := gennotes.NewEndpoints(REST{Notes: svc})
	if _, err := none.EmptyTrash(ai, &gennotes.EmptyTrashPayload{}); err != nil {
		t.Errorf("empty_trash without passkeys: %v", err)
	}
}

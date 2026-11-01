package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// order returns the manual order of notebook nb as listed for ctx: note IDs,
// and separators as "|title" placed before the note they come before.
func order(t *testing.T, svc *Notes, ctx context.Context, nb string) string {
	t.Helper()
	res, err := svc.List(ctx, &gennotes.ListPayload{Notebook: ptr(nb), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]string{}
	for _, sep := range res.Separators {
		before[deref(sep.Before)] = append(before[deref(sep.Before)], "|"+deref(sep.Title))
	}
	var items []string
	for _, n := range res.Notes {
		items = append(items, before[n.ID]...)
		items = append(items, n.ID)
	}
	items = append(items, before[""]...) // after the last note
	return strings.Join(items, " ")
}

func TestSeparators(t *testing.T) {
	svc, dir := newTestService(t)
	ctx := AsPerson(context.Background())
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var now time.Time
	svc.now = func() time.Time { return now }
	for i, id := range []string{"a", "b", "c"} {
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
	check("start", "c b a")

	first, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr(" 新しい "), Date: ptr("2026-10-01"), Color: ptr("blue")})
	if err != nil {
		t.Fatal(err)
	}
	if !store.ValidSeparatorID(first.ID) || deref(first.Title) != "新しい" || deref(first.Date) != "2026-10-01" || deref(first.Color) != "blue" || deref(first.Before) != "c" {
		t.Errorf("created %+v", first)
	}
	mid, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("古い"), After: ptr("b")})
	if err != nil {
		t.Fatal(err)
	}
	last, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("末尾"), After: ptr("a")})
	if err != nil {
		t.Fatal(err)
	}
	if last.Before != nil {
		t.Errorf("the last separator comes before %v", *last.Before)
	}
	check("created", "|新しい c b |古い a |末尾")

	// A note goes after a separator, and a separator after a note or another separator.
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "c", After: &mid.ID}); err != nil {
		t.Fatal(err)
	}
	check("note moved", "|新しい b |古い c a |末尾")
	if _, err := svc.MoveSeparator(ctx, &gennotes.MoveSeparatorPayload{ID: last.ID, Notebook: "work", After: &first.ID}); err != nil {
		t.Fatal(err)
	}
	check("separator moved", "|新しい |末尾 b |古い c a")
	if _, err := svc.MoveSeparator(ctx, &gennotes.MoveSeparatorPayload{ID: first.ID, Notebook: "work", After: ptr("a")}); err != nil {
		t.Fatal(err)
	}
	check("separator moved last", "|末尾 b |古い c a |新しい")
	if _, err := svc.MoveSeparator(ctx, &gennotes.MoveSeparatorPayload{ID: first.ID, Notebook: "work", After: ptr("nope")}); errorName(err) != "bad_request" {
		t.Errorf("after a missing item: %v", err)
	}

	// The heading, the date and the color are replaced, and an omitted one is
	// taken away.
	up, err := svc.UpdateSeparator(ctx, &gennotes.UpdateSeparatorPayload{ID: first.ID, Notebook: "work", Color: ptr("red")})
	if err != nil {
		t.Fatal(err)
	}
	if up.Title != nil || up.Date != nil || deref(up.Color) != "red" {
		t.Errorf("updated %+v", up)
	}
	if _, err := svc.UpdateSeparator(ctx, &gennotes.UpdateSeparatorPayload{ID: first.ID, Notebook: "work", Title: ptr("新しい")}); err != nil {
		t.Fatal(err)
	}
	up, err = svc.UpdateSeparator(ctx, &gennotes.UpdateSeparatorPayload{ID: mid.ID, Notebook: "work", Title: ptr("中")})
	if err != nil {
		t.Fatal(err)
	}
	if deref(up.Title) != "中" || up.Date != nil || deref(up.Before) != "c" {
		t.Errorf("updated %+v", up)
	}
	if err := svc.DeleteSeparator(ctx, &gennotes.DeleteSeparatorPayload{ID: last.ID, Notebook: "work"}); err != nil {
		t.Fatal(err)
	}
	check("deleted", "b |中 c a |新しい")
	if err := svc.DeleteSeparator(ctx, &gennotes.DeleteSeparatorPayload{ID: last.ID, Notebook: "work"}); errorName(err) != "not_found" {
		t.Errorf("deleting again: %v", err)
	}

	// Separators show only in the manual order of a notebook, and are no notes.
	for name, p := range map[string]*gennotes.ListPayload{
		"by title": {Notebook: ptr("work"), Sort: ptr("title"), Limit: 50},
		"by tag":   {Notebook: ptr("work"), Tag: ptr("x"), Limit: 50},
		"all":      {Limit: 50},
	} {
		res, err := svc.List(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Separators) != 0 || res.Total != 3 {
			t.Errorf("%s: %d separators, total %d", name, len(res.Separators), res.Total)
		}
	}
	if res, err := svc.Search(ctx, &gennotes.SearchPayload{Q: "中", Limit: 50}); err != nil || res.Total != 0 {
		t.Errorf("search found a separator: %+v, %v", res, err)
	}

	// Errors.
	if _, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "missing"}); errorName(err) != "not_found" {
		t.Errorf("a missing notebook: %v", err)
	}
	if _, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: store.Trash}); errorName(err) != "bad_request" {
		t.Errorf("the trash: %v", err)
	}

	// An edit of the file elsewhere is picked up by a sync, with a notebooks event.
	events, cancel := svc.events.subscribe()
	defer cancel()
	p := filepath.Join(dir, "work", store.SeparatorsFile)
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "title: 中", "title: 書き換え", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Type != eventNotebooks {
			t.Errorf("event %s", ev.Type)
		}
	default:
		t.Error("no event for the edited separators")
	}
	check("edited elsewhere", "b |書き換え c a |新しい")
}

// TestSeparatorsRespace checks that the notes and the separators are spaced
// again when the neighbors of a new place leave no room between their keys.
func TestSeparatorsRespace(t *testing.T) {
	svc, dir := newTestService(t)
	ctx := AsPerson(context.Background())
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	for id, key := range map[string]int{"p": 11, "q": 9} {
		src := "title: " + id + "\ntags: []\ncreated_at: 2026-10-01T09:00:00Z\nupdated_at: 2026-10-01T09:00:00Z\norder: " + strconv.Itoa(key) + "\nbody: \"\"\n"
		if err := os.WriteFile(filepath.Join(dir, "work", id+".yaml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "work", store.SeparatorsFile), []byte("- id: sep-s\n  title: s\n  order: 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := order(t, svc, ctx, "work"); got != "p |s q" {
		t.Fatalf("start: %q", got)
	}
	// p (11) and the separator (10) have no key between them.
	if _, err := svc.CreateSeparator(ctx, &gennotes.CreateSeparatorPayload{Notebook: "work", Title: ptr("t"), After: ptr("p")}); err != nil {
		t.Fatal(err)
	}
	if got := order(t, svc, ctx, "work"); got != "p |t |s q" {
		t.Errorf("after the separator: %q", got)
	}
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "q", After: ptr("sep-s")}); err != nil {
		t.Fatal(err)
	}
	if got := order(t, svc, ctx, "work"); got != "p |t |s q" {
		t.Errorf("q after s, where it was: %q", got)
	}
	if _, err := svc.Move(ctx, &gennotes.MovePayload{ID: "q"}); err != nil {
		t.Fatal(err)
	}
	if got := order(t, svc, ctx, "work"); got != "q p |t |s" {
		t.Errorf("q first: %q", got)
	}
}

// TestAISeparators checks that AI clients see the separators of the
// notebooks they see, and change them in collab notebooks only.
func TestAISeparators(t *testing.T) {
	svc, _ := newTestService(t)
	person := AsPerson(context.Background())
	ai := context.Background()
	for nb, kind := range map[string]string{"shared": "collab", "people": "default", "secret": "private"} {
		if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: nb, Kind: &kind}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Create(person, &gennotes.CreatePayload{Notebook: ptr(nb), Title: "n"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateSeparator(person, &gennotes.CreateSeparatorPayload{Notebook: nb, Title: ptr(nb)}); err != nil {
			t.Fatal(err)
		}
	}
	for nb, want := range map[string]int{"shared": 1, "people": 1, "secret": 0} {
		res, err := svc.List(ai, &gennotes.ListPayload{Notebook: ptr(nb), Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Separators) != want {
			t.Errorf("AI sees %d separators of %s, want %d", len(res.Separators), nb, want)
		}
	}
	sep, err := svc.CreateAiSeparator(ai, &gennotes.CreateAiSeparatorPayload{Notebook: "shared", Title: ptr("AI")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAiSeparator(ai, &gennotes.UpdateAiSeparatorPayload{ID: sep.ID, Notebook: "shared", Title: ptr("AI 2")}); err != nil {
		t.Error(err)
	}
	if _, err := svc.MoveAiSeparator(ai, &gennotes.MoveAiSeparatorPayload{ID: sep.ID, Notebook: "shared", After: ptr("sep-none")}); errorName(err) != "bad_request" {
		t.Errorf("after a missing item: %v", err)
	}
	if err := svc.DeleteAiSeparator(ai, &gennotes.DeleteAiSeparatorPayload{ID: sep.ID, Notebook: "shared"}); err != nil {
		t.Error(err)
	}
	for nb, want := range map[string]string{"people": "forbidden", "secret": "not_found", "missing": "not_found", "inbox": "not_found"} {
		if _, err := svc.CreateAiSeparator(ai, &gennotes.CreateAiSeparatorPayload{Notebook: nb}); errorName(err) != want {
			t.Errorf("create_ai_separator in %s: %v, want %s", nb, err, want)
		}
	}
	res, _ := svc.List(person, &gennotes.ListPayload{Notebook: ptr("people"), Limit: 50})
	if err := svc.DeleteAiSeparator(ai, &gennotes.DeleteAiSeparatorPayload{ID: res.Separators[0].ID, Notebook: "people"}); errorName(err) != "forbidden" {
		t.Errorf("delete_ai_separator in a notebook of people: %v", err)
	}
}

// TestMCPSeparators checks the separators through the MCP tools: list_notes
// shows them, and create_ai_separator puts one in a collab notebook.
func TestMCPSeparators(t *testing.T) {
	svc, _ := newTestService(t)
	person := AsPerson(context.Background())
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "shared", Kind: ptr("collab")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("n1"), Notebook: ptr("shared"), Title: "n"}); err != nil {
		t.Fatal(err)
	}
	adapter := mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), nil)
	call := func(tool, args string) string {
		t.Helper()
		res, err := adapter.ToolsCall(context.Background(), &mcpnotes.ToolsCallPayload{Name: tool, Arguments: json.RawMessage(args)})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if out := call("create_ai_separator", `{"notebook":"shared","title":"AI の区切り","after":"n1"}`); !strings.Contains(out, `sep-`) || strings.Contains(out, `"isError":true`) {
		t.Fatalf("create_ai_separator: %s", out)
	}
	out := call("list_notes", `{"notebook":"shared"}`)
	if !strings.Contains(out, `separators`) || !strings.Contains(out, `AI の区切り`) {
		t.Errorf("list_notes: %s", out)
	}
}

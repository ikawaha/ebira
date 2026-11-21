package service

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/zenn"
)

func TestExportZenn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "test")
		t.Setenv(k+"_EMAIL", "test@example.com")
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(base, "init", "--quiet", "--bare", "--initial-branch=main", remote)

	// Not set: cannot.
	if st, _ := svc.ZennStatus(person, &gennotes.ZennStatusPayload{}); st.Configured {
		t.Error("configured without settings")
	}
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "x"}); errorName(err) != "bad_request" {
		t.Errorf("err = %v, want bad_request", err)
	}
	settings := zenn.Settings{Repo: remote, User: "you"}
	svc.SetZenn(filepath.Join(base, "state", "zenn"), func() (zenn.Settings, error) { return settings, nil })
	if st, _ := svc.ZennStatus(ai, &gennotes.ZennStatusPayload{}); st.Configured {
		t.Error("configured for AI")
	}
	if st, _ := svc.ZennStatus(person, &gennotes.ZennStatusPayload{}); !st.Configured || *st.User != "you" || !st.Git {
		t.Errorf("status = %+v", st)
	}
	t.Run("without git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if st, _ := svc.ZennStatus(person, &gennotes.ZennStatusPayload{}); st.Git {
			t.Error("git found without it on PATH")
		}
	})

	mk := func(id, notebook, title, body string, tags ...string) {
		t.Helper()
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: ptr(notebook), Title: title, Body: ptr(body), Tags: tags}); err != nil {
			t.Fatal(err)
		}
	}
	mk("other1234567", "work", "公開済み", "x")
	mk("article12345", "work", "記事", "![図](article12345.assets/fig.png)\n[資料](article12345.assets/doc.pdf)\n[ほか](/#note/other1234567)", "Go", "C++")
	if _, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: "article12345", Name: "fig.png"}, body(pngHead)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddFile(person, &gennotes.AddFilePayload{ID: "article12345", Name: "doc.pdf"}, body([]byte("%PDF-1.4"))); err != nil {
		t.Fatal(err)
	}

	// AI clients cannot.
	if _, err := svc.ExportZenn(ai, &gennotes.ExportZennPayload{ID: "article12345"}); errorName(err) != "unauthorized" {
		t.Errorf("AI: err = %v", err)
	}
	// A short ID is no slug.
	mk("short", "work", "短い", "x")
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short"}); errorName(err) != "bad_request" {
		t.Errorf("short ID: err = %v", err)
	}
	// It takes a slug given instead, but not one another note would take.
	for _, slug := range []string{"bad slug here", "article12345"} {
		if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short", Slug: ptr(slug)}); errorName(err) != "bad_request" {
			t.Errorf("slug %q: err = %v", slug, err)
		}
	}
	res, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short", Slug: ptr("short-article")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Note.Zenn.Slug != "short-article" {
		t.Errorf("slug = %q", res.Note.Zenn.Slug)
	}
	git(remote, "show", "main:articles/short-article.md")
	// The slug stays, given or not.
	if res, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short"}); err != nil || res.Note.Zenn.Slug != "short-article" {
		t.Errorf("again: %v", err)
	}
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short", Slug: ptr("short-article-2")}); errorName(err) != "bad_request" {
		t.Errorf("new slug: err = %v", err)
	}
	// No other note takes it.
	mk("short-article", "work", "同じ名前", "x")
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "short-article"}); errorName(err) != "bad_request" {
		t.Errorf("taken: err = %v", err)
	}
	// A note whose ID can be a slug may take another.
	mk("fits-as-a-slug", "work", "ID の合うノート", "x")
	if res, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "fits-as-a-slug", Slug: ptr("20261121-another")}); err != nil || res.Note.Zenn.Slug != "20261121-another" {
		t.Errorf("another slug for a fit ID: %v", err)
	}

	// The other note is published first, so that the link goes to it.
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "other1234567", Published: true}); err != nil {
		t.Fatal(err)
	}
	res, err = svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pushed {
		t.Error("not pushed")
	}
	z := res.Note.Zenn
	if z == nil || z.Slug != "article12345" || z.Published || *z.URL != "https://zenn.dev/you/articles/article12345" || !slices.Contains(zenn.Emoji, *z.Emoji) {
		t.Errorf("zenn = %+v", z)
	}
	kinds := ""
	for _, w := range res.Warnings {
		kinds += w.Kind + ":" + w.Name + " "
	}
	if kinds != "tag:C++ file:doc.pdf " {
		t.Errorf("warnings = %q", kinds)
	}
	article := git(remote, "show", "main:articles/article12345.md")
	for _, want := range []string{`topics: ["go"]`, "published: false", "![図](/images/article12345/fig.png)", "\n資料\n", "[ほか](https://zenn.dev/you/articles/other1234567)"} {
		if !strings.Contains(article, want) {
			t.Errorf("article lacks %q:\n%s", want, article)
		}
	}
	if img := git(remote, "show", "main:images/article12345/fig.png"); !bytes.Equal([]byte(img), pngHead) {
		t.Errorf("image = %q", img)
	}

	// The note keeps the article, without a new update time; lists tell it.
	n, err := svc.Get(person, &gennotes.GetPayload{ID: "article12345"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Zenn == nil || n.UpdatedAt != n.CreatedAt && n.UpdatedAt > res.Note.UpdatedAt {
		t.Errorf("note = %+v", n)
	}
	list, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr("work"), Archived: "exclude", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list.Notes {
		if s.ID == "article12345" {
			found = s.Zenn != nil && s.Zenn.Slug == "article12345" && !s.Zenn.Published
		}
	}
	if !found {
		t.Error("the list does not tell the article")
	}

	// Editing the note leaves the article behind, until it is written again;
	// tags make no article.
	changed := func(want bool) {
		t.Helper()
		n, err := svc.Get(person, &gennotes.GetPayload{ID: "article12345"})
		if err != nil {
			t.Fatal(err)
		}
		list, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr("work"), Archived: "exclude", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list.Notes {
			if s.ID == "article12345" && (s.Zenn.Changed != want || n.Zenn.Changed != want) {
				t.Errorf("changed = %v in the list, %v in the note, want %v", s.Zenn.Changed, n.Zenn.Changed, want)
			}
		}
	}
	changed(false)
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: "article12345", Tags: []string{"Go", "C++", "x"}}); err != nil {
		t.Fatal(err)
	}
	changed(false)
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: "article12345", Title: ptr("記事 2")}); err != nil {
		t.Fatal(err)
	}
	changed(true)

	// Published next time, with the same emoji and topics.
	res, err = svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345", Published: true, Emoji: ptr("🐦")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Note.Zenn.Published || *res.Note.Zenn.Emoji != "🐦" || len(res.Note.Zenn.Topics) != 1 {
		t.Errorf("zenn = %+v", res.Note.Zenn)
	}
	changed(false)

	// Changed on GitHub: refused unless forced.
	work := filepath.Join(base, "work")
	git(base, "clone", "--quiet", remote, work)
	if err := os.WriteFile(filepath.Join(work, "articles", "article12345.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "commit", "--quiet", "-am", "edit")
	git(work, "push", "--quiet", "origin", "main")
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345", Published: true}); errorName(err) != "article_changed" {
		t.Errorf("err = %v, want article_changed", err)
	}
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345", Published: true, Force: true}); err != nil {
		t.Fatal(err)
	}

	// A private notebook needs private.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: "work", Kind: ptr("private")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345"}); errorName(err) != "bad_request" {
		t.Errorf("private: err = %v", err)
	}
	if _, err := svc.ExportZenn(person, &gennotes.ExportZennPayload{ID: "article12345", Private: true}); err != nil {
		t.Errorf("private allowed: %v", err)
	}
}

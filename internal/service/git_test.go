package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
)

// gitClones makes a remote repository and two clones of it, each with a
// service on its notes, as on two machines.
func gitClones(t *testing.T) (a, b *Notes) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "test")
		t.Setenv(k+"_EMAIL", "test@example.com")
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	git(base, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(base, "seed")
	git(base, "clone", "--quiet", remote, seed)
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(seed, "add", ".")
	git(seed, "commit", "--quiet", "-m", "seed")
	git(seed, "push", "--quiet", "origin", "HEAD:main")
	svc := func(name string) *Notes {
		dir := filepath.Join(base, name)
		git(base, "clone", "--quiet", remote, dir)
		st, err := store.New(filepath.Join(dir, "notes"))
		if err != nil {
			t.Fatal(err)
		}
		ix, err := index.Open(context.Background(), filepath.Join(base, name+".sqlite"), index.Config{Mode: index.ModeBigram})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ix.Close() })
		s := New(st, ix)
		t.Cleanup(s.Close)
		return s
	}
	return svc("a"), svc("b")
}

func TestGitSync(t *testing.T) {
	a, b := gitClones(t)
	ctx := AsPerson(context.Background())

	if _, err := a.GitSync(context.Background(), &gennotes.GitSyncPayload{}); errorName(err) != "forbidden" {
		t.Errorf("not a person: %v", err)
	}
	if st, err := a.GitStatus(ctx); err != nil || !st.Repository || st.Upstream == nil || *st.Upstream != "origin/main" {
		t.Errorf("status: %+v, %v", st, err)
	}
	if st, _ := a.GitStatus(context.Background()); st.Repository {
		t.Error("status for others: a repository")
	}

	n, err := a.Create(ctx, &gennotes.CreatePayload{ID: ptr("shared"), Title: "Shared", Body: ptr("one")})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.GitSync(ctx, &gennotes.GitSyncPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Committed != 1 || r.Upstream == nil || *r.Upstream != "origin/main" || !r.Pushed {
		t.Errorf("a: %+v", r)
	}
	r, err = b.GitSync(ctx, &gennotes.GitSyncPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Merged != 1 {
		t.Errorf("b: %+v", r)
	}
	if got, err := b.Get(ctx, &gennotes.GetPayload{ID: "shared"}); err != nil || got.Body != "one" {
		t.Fatalf("b's note: %+v, %v", got, err)
	}

	// Both change the note: b keeps its version, and a's becomes a new note.
	if _, err := a.Update(ctx, &gennotes.UpdatePayload{ID: "shared", Body: ptr("from a"), IfMatch: &n.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GitSync(ctx, &gennotes.GitSyncPayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Update(ctx, &gennotes.UpdatePayload{ID: "shared", Body: ptr("from b")}); err != nil {
		t.Fatal(err)
	}
	r, err = b.GitSync(ctx, &gennotes.GitSyncPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].Path != "inbox/shared.yaml" || deref(r.Conflicts[0].NoteID) != "shared" || r.Conflicts[0].CopyID == nil {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
	if got, _ := b.Get(ctx, &gennotes.GetPayload{ID: "shared"}); got.Body != "from b" {
		t.Errorf("kept: %q", got.Body)
	}
	cp, err := b.Get(ctx, &gennotes.GetPayload{ID: *r.Conflicts[0].CopyID})
	if err != nil {
		t.Fatal(err)
	}
	if cp.Body != "from a" || !strings.HasPrefix(cp.Title, "Shared (conflict ") || cp.Notebook != "inbox" {
		t.Errorf("copy: %+v", cp)
	}
	// a gets both after its next sync.
	if _, err := a.GitSync(ctx, &gennotes.GitSyncPayload{}); err != nil {
		t.Fatal(err)
	}
	if got, err := a.Get(ctx, &gennotes.GetPayload{ID: cp.ID}); err != nil || got.Body != "from a" {
		t.Errorf("a's copy: %+v, %v", got, err)
	}
}

func TestGitSyncNoRepository(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	if st, err := svc.GitStatus(AsPerson(context.Background())); err != nil || st.Repository {
		t.Errorf("status: %+v, %v", st, err)
	}
	if _, err := svc.GitSync(AsPerson(context.Background()), &gennotes.GitSyncPayload{}); errorName(err) != "bad_request" || !strings.Contains(err.Error(), "git init") {
		t.Errorf("err = %v", err)
	}
}

func TestGitSyncRemote(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "test")
		t.Setenv(k+"_EMAIL", "test@example.com")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--quiet", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	ctx := AsPerson(context.Background())
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{Title: "A"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.GitSync(ctx, &gennotes.GitSyncPayload{Remote: &remote})
	if err != nil {
		t.Fatal(err)
	}
	if r.Committed != 1 || !r.Pushed || r.Upstream == nil {
		t.Errorf("result: %+v", r)
	}
	// From now on, without the remote.
	if r, err := svc.GitSync(ctx, &gennotes.GitSyncPayload{}); err != nil || r.Upstream == nil {
		t.Errorf("again: %+v, %v", r, err)
	}
}

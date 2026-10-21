package gitsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnv isolates git from the user's settings.
func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, name, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// clones makes a bare repository with one commit of notes/inbox/a.yaml and
// returns the notes directories of two clones of it.
func clones(t *testing.T) (a, b string) {
	t.Helper()
	gitEnv(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	run(t, base, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(base, "seed")
	run(t, base, "clone", "--quiet", remote, seed)
	write(t, filepath.Join(seed, "notes", "inbox", "a.yaml"), "title: A\nbody: |\n  one\n")
	write(t, filepath.Join(seed, "README"), "data\n")
	run(t, seed, "add", ".")
	run(t, seed, "commit", "--quiet", "-m", "seed")
	run(t, seed, "push", "--quiet", "origin", "HEAD:main")
	for _, name := range []string{"a", "b"} {
		run(t, base, "clone", "--quiet", remote, filepath.Join(base, name))
	}
	return filepath.Join(base, "a", "notes"), filepath.Join(base, "b", "notes")
}

func noLock(f func() error) error { return f() }

func isNote(p string) bool { return strings.HasSuffix(p, ".yaml") }

// copies is a Resolver that writes the other version as copy-of-<name>.
func copies(dir string) Resolver {
	return func(p string, theirs []byte) (string, error) {
		cp := path.Join(path.Dir(p), "copy-of-"+path.Base(p))
		return cp, os.WriteFile(filepath.Join(dir, filepath.FromSlash(cp)), theirs, 0o644)
	}
}

func sync(t *testing.T, dir string) *Result {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Sync(ctx, noLock, isNote, copies(dir))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestOpen(t *testing.T) {
	gitEnv(t)
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := Open(ctx, dir); !errors.Is(err, ErrNotRepo) {
		t.Errorf("no repository: %v", err)
	}
	run(t, dir, "init", "--quiet")
	notes := filepath.Join(dir, "notes")
	write(t, filepath.Join(notes, "inbox", "a.yaml"), "title: A\n")
	write(t, filepath.Join(dir, ".gitignore"), "/notes/\n")
	if _, err := Open(ctx, notes); !errors.Is(err, ErrNotRepo) {
		t.Errorf("ignored: %v", err)
	}
	// A repository of the notes directory itself.
	run(t, notes, "init", "--quiet")
	r, err := Open(ctx, notes)
	if err != nil {
		t.Fatal(err)
	}
	if r.rel != "." {
		t.Errorf("rel = %q", r.rel)
	}
}

func TestSyncCommitOnly(t *testing.T) {
	gitEnv(t)
	dir := t.TempDir()
	run(t, dir, "init", "--quiet")
	notes := filepath.Join(dir, "notes")
	write(t, filepath.Join(notes, "inbox", "a.yaml"), "title: A\n")
	write(t, filepath.Join(notes, "inbox", ".DS_Store"), "x")
	write(t, filepath.Join(dir, "other.txt"), "not a note\n")
	res := sync(t, notes)
	if res.Committed != 1 || res.Upstream != "" || res.Pushed {
		t.Errorf("result: %+v", res)
	}
	files := run(t, dir, "ls-files")
	if files != "notes/inbox/a.yaml\n" {
		t.Errorf("committed files:\n%s", files)
	}
	msg := run(t, dir, "log", "-1", "--format=%B")
	if !strings.HasPrefix(msg, "Update notes") || !strings.Contains(msg, "A inbox/a.yaml") {
		t.Errorf("message:\n%s", msg)
	}
	// Nothing changed: no commit.
	if res := sync(t, notes); res.Committed != 0 {
		t.Errorf("again: %+v", res)
	}
	// A deletion is committed too.
	if err := os.Remove(filepath.Join(notes, "inbox", "a.yaml")); err != nil {
		t.Fatal(err)
	}
	if res := sync(t, notes); res.Committed != 1 {
		t.Errorf("deletion: %+v", res)
	}
	if files := run(t, dir, "ls-files"); files != "" {
		t.Errorf("after the deletion:\n%s", files)
	}
}

func TestSyncPushAndPull(t *testing.T) {
	a, b := clones(t)
	write(t, filepath.Join(a, "inbox", "a.yaml"), "title: A\nbody: |\n  one\n  two\n")
	write(t, filepath.Join(a, "inbox", "n.yaml"), "title: N\n")
	res := sync(t, a)
	if res.Committed != 2 || res.Upstream != "origin/main" || !res.Pushed {
		t.Errorf("a: %+v", res)
	}
	res = sync(t, b)
	if res.Committed != 0 || res.Merged != 2 || res.Pushed || len(res.Conflicts) != 0 {
		t.Errorf("b: %+v", res)
	}
	if got := read(t, filepath.Join(b, "inbox", "n.yaml")); got != "title: N\n" {
		t.Errorf("b has %q", got)
	}
}

func TestSyncConflicts(t *testing.T) {
	a, b := clones(t)
	write(t, filepath.Join(a, "inbox", "a.yaml"), "title: A\nbody: |\n  one from a\n")
	sync(t, a)
	// b changes the same line, and deletes nothing yet.
	write(t, filepath.Join(b, "inbox", "a.yaml"), "title: A\nbody: |\n  one from b\n")
	res := sync(t, b)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "inbox/a.yaml" || res.Conflicts[0].Copy != "inbox/copy-of-a.yaml" || !res.Pushed {
		t.Fatalf("b: %+v", res)
	}
	if got := read(t, filepath.Join(b, "inbox", "a.yaml")); !strings.Contains(got, "from b") {
		t.Errorf("b's version not kept: %q", got)
	}
	if got := read(t, filepath.Join(b, "inbox", "copy-of-a.yaml")); !strings.Contains(got, "from a") {
		t.Errorf("a's version not copied: %q", got)
	}
	if st := run(t, filepath.Dir(b), "status", "--porcelain"); st != "" {
		t.Errorf("b is not clean:\n%s", st)
	}
	// a gets both.
	res = sync(t, a)
	if res.Merged != 2 || len(res.Conflicts) != 0 {
		t.Errorf("a: %+v", res)
	}

	// Deleted on one side, changed on the other: the changed version stays.
	if err := os.Remove(filepath.Join(a, "inbox", "copy-of-a.yaml")); err != nil {
		t.Fatal(err)
	}
	sync(t, a)
	write(t, filepath.Join(b, "inbox", "copy-of-a.yaml"), "title: changed\n")
	res = sync(t, b)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Copy != "" {
		t.Errorf("modify/delete: %+v", res)
	}
	if got := read(t, filepath.Join(b, "inbox", "copy-of-a.yaml")); got != "title: changed\n" {
		t.Errorf("changed version: %q", got)
	}
}

func TestSyncOutsideConflict(t *testing.T) {
	a, b := clones(t)
	write(t, filepath.Join(filepath.Dir(a), "README"), "from a\n")
	run(t, filepath.Dir(a), "commit", "--quiet", "-am", "readme")
	run(t, filepath.Dir(a), "push", "--quiet")
	write(t, filepath.Join(filepath.Dir(b), "README"), "from b\n")
	run(t, filepath.Dir(b), "commit", "--quiet", "-am", "readme")
	write(t, filepath.Join(b, "inbox", "n.yaml"), "title: N\n")

	ctx := context.Background()
	r, err := Open(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(ctx, noLock, isNote, copies(b)); err == nil || !strings.Contains(err.Error(), "README") {
		t.Errorf("err = %v", err)
	}
	// The merge was aborted; the notes were committed.
	if _, err := os.Stat(filepath.Join(filepath.Dir(b), ".git", "MERGE_HEAD")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the merge is left")
	}
	if st := run(t, filepath.Dir(b), "status", "--porcelain"); st != "" {
		t.Errorf("not clean:\n%s", st)
	}
}

func TestSyncBusy(t *testing.T) {
	a, b := clones(t)
	write(t, filepath.Join(a, "inbox", "a.yaml"), "title: A\nbody: |\n  from a\n")
	sync(t, a)
	write(t, filepath.Join(b, "inbox", "a.yaml"), "title: A\nbody: |\n  from b\n")
	run(t, filepath.Dir(b), "commit", "--quiet", "-am", "b")
	cmd := exec.Command("git", "pull", "--no-rebase", "--quiet")
	cmd.Dir = filepath.Dir(b)
	if err := cmd.Run(); err == nil {
		t.Fatal("no conflict")
	}
	ctx := context.Background()
	r, err := Open(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(ctx, noLock, isNote, copies(b)); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v", err)
	}
}

func TestConnectAndClone(t *testing.T) {
	gitEnv(t)
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	run(t, base, "init", "--quiet", "--bare", "--initial-branch=trunk", remote)
	if b, err := RemoteBranch(ctx, remote); err != nil || b != "" {
		t.Errorf("empty remote: %q, %v", b, err)
	}

	// The first machine: notes not in git yet.
	first := filepath.Join(base, "first", "notes")
	write(t, filepath.Join(first, "inbox", "a.yaml"), "title: A\n")
	write(t, filepath.Join(first, "_ebira.yaml"), "search: ja\n")
	res, err := Connect(ctx, first, remote, noLock, isNote, copies(first))
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed != 2 || !res.Pushed || !strings.HasPrefix(res.Upstream, "origin/") {
		t.Errorf("first: %+v", res)
	}
	branch := strings.TrimPrefix(res.Upstream, "origin/")
	if b, err := RemoteBranch(ctx, remote); err != nil || b != branch {
		t.Errorf("remote branch: %q, %v; want %q", b, err, branch)
	}
	// Again: nothing to do.
	if _, err := Connect(ctx, first, remote, noLock, isNote, copies(first)); err != nil {
		t.Errorf("again: %v", err)
	}

	// The second machine clones them.
	second := filepath.Join(base, "second", "notes")
	if err := Clone(ctx, remote, second); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(second, "inbox", "a.yaml")); got != "title: A\n" {
		t.Errorf("clone: %q", got)
	}
	if res := sync(t, second); res.Upstream != "origin/"+branch {
		t.Errorf("second: %+v", res)
	}

	// A third machine started notes of its own: both are kept.
	third := filepath.Join(base, "third", "notes")
	write(t, filepath.Join(third, "inbox", "c.yaml"), "title: C\n")
	res, err = Connect(ctx, third, remote, noLock, isNote, copies(third))
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed != 1 || res.Merged != 2 || !res.Pushed || res.Upstream != "origin/"+branch {
		t.Errorf("third: %+v", res)
	}
	if got := read(t, filepath.Join(third, "inbox", "a.yaml")); got != "title: A\n" {
		t.Errorf("third has %q", got)
	}
	if res := sync(t, first); res.Merged != 1 {
		t.Errorf("first gets c: %+v", res)
	}

	// Another remote is refused.
	if _, err := Connect(ctx, first, filepath.Join(base, "other.git"), noLock, isNote, copies(first)); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("other remote: %v", err)
	}
}

func TestConnectInRepository(t *testing.T) {
	gitEnv(t)
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	run(t, base, "init", "--quiet", "--bare", remote)
	data := filepath.Join(base, "data")
	run(t, base, "init", "--quiet", data)
	notes := filepath.Join(data, "notes")
	write(t, filepath.Join(notes, "inbox", "a.yaml"), "title: A\n")

	// In a repository above that keeps them: refused.
	if _, err := Connect(ctx, notes, remote, noLock, isNote, copies(notes)); err == nil || !strings.Contains(err.Error(), "set its remote") {
		t.Errorf("tracked above: %v", err)
	}
	// Ignored by the repository above: a repository of their own.
	write(t, filepath.Join(data, ".gitignore"), "/notes/\n")
	if _, err := Connect(ctx, notes, remote, noLock, isNote, copies(notes)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(notes, ".git")); err != nil {
		t.Errorf("no repository of the notes: %v", err)
	}
}

// TestConnectFailure checks that a failed Connect leaves the notes as they
// were: a wrong URL writes nothing, and a refused push undoes the repository
// or the remote it made, so that it can be tried again with another URL.
func TestConnectFailure(t *testing.T) {
	gitEnv(t)
	ctx := context.Background()
	base := t.TempDir()
	notes := filepath.Join(base, "notes")
	write(t, filepath.Join(notes, "inbox", "a.yaml"), "title: A\n")
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}

	// A wrong URL: no repository is made.
	if _, err := Connect(ctx, notes, filepath.Join(base, "nothing.git"), noLock, isNote, copies(notes)); err == nil {
		t.Fatal("Connect to a missing remote succeeded")
	}
	if exists(filepath.Join(notes, ".git")) {
		t.Error("a repository was left after a wrong URL")
	}

	// A refused push: the repository made by this call goes.
	refusing := filepath.Join(base, "refusing.git")
	run(t, base, "init", "--quiet", "--bare", refusing)
	write(t, filepath.Join(refusing, "hooks", "pre-receive"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(refusing, "hooks", "pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Connect(ctx, notes, refusing, noLock, isNote, copies(notes)); err == nil {
		t.Fatal("Connect with a refused push succeeded")
	}
	if exists(filepath.Join(notes, ".git")) {
		t.Error("a repository was left after a refused push")
	}

	// A repository of the user's own: only the remote added goes.
	run(t, notes, "init", "--quiet")
	if _, err := Connect(ctx, notes, refusing, noLock, isNote, copies(notes)); err == nil {
		t.Fatal("Connect with a refused push succeeded")
	}
	if !exists(filepath.Join(notes, ".git")) {
		t.Error("the repository of the user was removed")
	}
	if out, _ := exec.Command("git", "-C", notes, "remote").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("remotes left: %q", out)
	}

	// Then the right URL works.
	remote := filepath.Join(base, "remote.git")
	run(t, base, "init", "--quiet", "--bare", remote)
	if res, err := Connect(ctx, notes, remote, noLock, isNote, copies(notes)); err != nil || !res.Pushed {
		t.Errorf("Connect after the failures = %+v, %v", res, err)
	}
}

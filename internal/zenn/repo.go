package zenn

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikawaha/ebira/internal/gitsync"
)

// ErrChanged is returned when the article file in the repository is not the
// one ebira wrote last, as when it was edited on GitHub: writing it would
// lose that change.
var ErrChanged = errors.New("the article was changed in the repository since ebira wrote it")

// defaultBranch is the branch of a repository that has none yet.
const defaultBranch = "main"

// Repo is the clone of the repository of the articles, for ebira alone.
type Repo struct {
	dir string
	s   Settings
}

// Open returns the clone in dir of the repository of s, which is made when
// it is written to.
func Open(dir string, s Settings) *Repo { return &Repo{dir: dir, s: s} }

// Result is the result of Write.
type Result struct {
	// Hash is the hash of the article file written, to keep for the next
	// time.
	Hash string
	// Pushed reports whether a commit was pushed: false when the article and
	// its images were in the repository already as they are.
	Pushed bool
}

// Write writes the article a and its images to the repository, commits and
// pushes. last is the hash of the article file ebira wrote last, empty for a
// new article: when the file in the repository differs from it, Write fails
// with ErrChanged unless force is set. When the push is refused, as when
// another push came in meanwhile, it starts over once.
func (r *Repo) Write(ctx context.Context, a *Article, last string, force bool) (*Result, error) {
	if err := a.Check(); err != nil {
		return nil, err
	}
	file, err := a.File()
	if err != nil {
		return nil, err
	}
	for try := 0; ; try++ {
		res, err := r.write(ctx, a, file, last, force)
		if errors.Is(err, errPush) && try == 0 {
			continue
		}
		return res, err
	}
}

// errPush marks an error of git push, to start over.
var errPush = errors.New("push failed")

func (r *Repo) write(ctx context.Context, a *Article, file []byte, last string, force bool) (*Result, error) {
	branch, err := r.prepare(ctx)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(r.dir, "articles", a.Slug+".md")
	old, err := os.ReadFile(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	case Hash(old) != last && !bytes.Equal(old, file) && !force:
		return nil, ErrChanged
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, file, 0o644); err != nil {
		return nil, err
	}
	imgDir := filepath.Join(r.dir, "images", a.Slug)
	if err := os.RemoveAll(imgDir); err != nil {
		return nil, err
	}
	for _, img := range a.Images {
		if err := copyFile(img.Path, filepath.Join(imgDir, img.Name)); err != nil {
			return nil, err
		}
	}
	res := &Result{Hash: Hash(file)}
	if _, err := r.git(ctx, "add", "-A"); err != nil {
		return nil, err
	}
	status, err := r.git(ctx, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(status) == "" {
		return res, nil
	}
	if _, err := r.git(ctx, "commit", "-q", "-m", fmt.Sprintf("ebira: %s (%s)", a.Title, a.Slug)); err != nil {
		return nil, err
	}
	if _, err := r.git(ctx, "push", "-q", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return nil, fmt.Errorf("%w: %w", errPush, err)
	}
	res.Pushed = true
	return res, nil
}

// prepare makes the clone the remote branch as it is, cloning the repository
// first when there is no clone of it, and returns the branch.
func (r *Repo) prepare(ctx context.Context) (string, error) {
	if err := gitsync.CheckURL(r.s.Repo); err != nil {
		return "", err
	}
	if url, err := r.git(ctx, "remote", "get-url", "origin"); err != nil || strings.TrimSpace(url) != r.s.Repo {
		// No clone yet, a broken one, or one of another repository.
		if err := os.RemoveAll(r.dir); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(r.dir), 0o755); err != nil {
			return "", err
		}
		if _, err := gitsync.Run(ctx, filepath.Dir(r.dir), "clone", "-q", "--filter=blob:none", "--no-checkout", "--", r.s.Repo, r.dir); err != nil {
			return "", err
		}
	}
	branch := r.s.Branch
	if branch == "" {
		b, err := gitsync.RemoteBranch(ctx, r.s.Repo)
		if err != nil {
			return "", err
		}
		branch = cmp.Or(b, defaultBranch)
	}
	if _, err := r.git(ctx, "check-ref-format", "--branch", branch); err != nil || strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("invalid branch %q: the branch is the one Zenn deploys from, such as main, and the URL of the repository goes in its own setting", branch)
	}
	if _, err := r.git(ctx, "fetch", "-q", "--prune", "origin"); err != nil {
		return "", err
	}
	remote := "refs/remotes/origin/" + branch
	if _, err := r.git(ctx, "rev-parse", "-q", "--verify", remote); err == nil {
		for _, args := range [][]string{
			{"checkout", "-q", "-f", "-B", branch, remote},
			{"reset", "-q", "--hard", remote},
			{"clean", "-q", "-f", "-d", "-x"},
		} {
			if _, err := r.git(ctx, args...); err != nil {
				return "", err
			}
		}
		return branch, nil
	}
	// The remote has no such branch yet, as a new repository: start it.
	for _, args := range [][]string{
		{"symbolic-ref", "HEAD", "refs/heads/" + branch},
		{"update-ref", "-d", "refs/heads/" + branch},
		{"rm", "-r", "-q", "-f", "--cached", "--ignore-unmatch", "."},
		{"clean", "-q", "-f", "-d", "-x"},
	} {
		if _, err := r.git(ctx, args...); err != nil {
			return "", err
		}
	}
	return branch, nil
}

func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	if _, err := os.Stat(filepath.Join(r.dir, ".git")); err != nil {
		return "", err
	}
	return gitsync.Run(ctx, r.dir, args...)
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

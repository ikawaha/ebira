// Package gitsync syncs a notes directory with its git repository by running
// the git command: it commits the changes of the notes, merges those of the
// upstream branch and pushes. Using the git command, rather than a library,
// keeps the user's settings, such as credentials, SSH and Git LFS.
//
// A merge never stops in conflict. A file changed on both sides keeps this
// side's version; for a note, the other side's version is also kept, as a new
// note made by the Resolver. A file deleted on one side and changed on the
// other keeps the changed version. Nothing is lost: the merge commit has the
// other side's versions too.
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrNotRepo is returned when the notes directory is not in a git repository,
// or the repository ignores it.
var ErrNotRepo = errors.New("the notes are not in a git repository")

// ErrBusy is returned when the repository is in the middle of an operation,
// such as a merge or a rebase, or has unmerged files.
var ErrBusy = errors.New("the git repository is in the middle of an operation")

// Resolver keeps theirs, the other side's version of a note changed on both
// sides, as a new note next to it, and returns the path of the new note's
// file. Paths are relative to the notes directory, with slashes.
type Resolver func(notePath string, theirs []byte) (copyPath string, err error)

// IsNote reports whether the path of a file, relative to the notes directory,
// may be a note's file, to be given to a Resolver.
type IsNote func(path string) bool

// Result is the result of Sync.
type Result struct {
	// Committed is the number of changed files committed.
	Committed int
	// Upstream is the upstream branch, such as origin/main; empty when the
	// branch has none, and then nothing is merged or pushed.
	Upstream string
	// Merged is the number of files changed by the merge.
	Merged int
	// Pushed reports whether commits were pushed.
	Pushed bool
	// Conflicts are the files changed on both sides, or deleted on one side
	// and changed on the other.
	Conflicts []Conflict
}

// Conflict is a file changed on both sides of a merge.
type Conflict struct {
	// Path is the file, relative to the notes directory.
	Path string
	// Copy is the file of the note made from the other side's version of the
	// note; empty when none was made.
	Copy string
}

// Repo is the git repository of a notes directory.
type Repo struct {
	root string // the top of the work tree
	rel  string // the notes directory relative to root, with slashes; "." for root
	host string // for commit messages
	now  func() time.Time
	// unrelated allows merging an unrelated history, for Connect.
	unrelated bool
}

// Open returns the repository of the notes directory dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is not installed")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	r := &Repo{root: abs, rel: ".", now: time.Now}
	top, err := r.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%w: %s; connect it to a remote repository (ebira sync -remote URL), or run git init there", ErrNotRepo, abs)
	}
	r.root = strings.TrimSpace(top)
	// Compare resolved paths, as the top is reported with links resolved.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(r.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %s is outside %s", ErrNotRepo, abs, r.root)
	}
	r.rel = filepath.ToSlash(rel)
	if r.rel != "." {
		if _, err := r.git(ctx, "check-ignore", "-q", r.rel); err == nil {
			return nil, fmt.Errorf("%w: %s ignores %s", ErrNotRepo, r.root, r.rel)
		}
	}
	r.host, _ = os.Hostname()
	return r, nil
}

// RemoteBranch returns the default branch of the remote repository url, or
// "" when the repository has no branch yet.
func RemoteBranch(ctx context.Context, url string) (string, error) {
	r := &Repo{}
	out, err := r.git(ctx, "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	for l := range strings.SplitSeq(out, "\n") {
		if ref, ok := strings.CutPrefix(l, "ref: refs/heads/"); ok {
			b, _, _ := strings.Cut(ref, "\t")
			return b, nil
		}
	}
	// No default branch: take main or master, else the first.
	out, err = r.git(ctx, "ls-remote", "--heads", url)
	if err != nil {
		return "", err
	}
	var heads []string
	for l := range strings.SplitSeq(out, "\n") {
		if _, ref, ok := strings.Cut(l, "\trefs/heads/"); ok {
			heads = append(heads, ref)
		}
	}
	for _, b := range []string{"main", "master"} {
		if slices.Contains(heads, b) {
			return b, nil
		}
	}
	if len(heads) > 0 {
		return heads[0], nil
	}
	return "", nil
}

// Clone clones the remote repository url into dir, a notes directory that
// does not exist yet or is empty.
func Clone(ctx context.Context, url, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if _, err := (&Repo{root: filepath.Dir(abs)}).git(ctx, "clone", "--quiet", url, abs); err != nil {
		return err
	}
	// The default branch of the remote may not exist, such as when the
	// first push was of another branch: then check out the one there is.
	r := &Repo{root: abs}
	if _, err := r.git(ctx, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		return nil
	}
	b, err := RemoteBranch(ctx, url)
	if err != nil || b == "" {
		return err
	}
	_, err = r.git(ctx, "checkout", "--quiet", "-B", b, "--track", "origin/"+b)
	return err
}

// Connect makes the notes directory dir sync with the remote repository url,
// as its remote origin: it makes dir a repository unless it is one, commits
// the notes and pushes them, setting the upstream branch. When the remote has
// a branch already, such as of notes started on another machine, it is
// merged first, as Sync does, even with an unrelated history. Notes in a
// repository above dir are refused: set the remote of that one with git.
func Connect(ctx context.Context, dir, url string, locked func(f func() error) error, isNote IsNote, resolve Resolver) (res *Result, err error) {
	r, err := Open(ctx, dir)
	create := errors.Is(err, ErrNotRepo) // none, or one above that ignores dir
	if err != nil && !create {
		return nil, err
	}
	addOrigin := create
	if !create {
		if r.rel != "." {
			return nil, fmt.Errorf("the notes are in the repository %s; set its remote with git", r.root)
		}
		if err := r.checkIdle(ctx); err != nil {
			return nil, err
		}
		if cur, err := r.git(ctx, "remote", "get-url", "origin"); err != nil {
			addOrigin = true
		} else if cur = strings.TrimSpace(cur); cur != url {
			return nil, fmt.Errorf("the remote origin of %s is %s already; change it with git remote set-url", r.root, cur)
		}
	}
	// The remote is checked before anything is written, so that a wrong URL
	// leaves the notes as they were.
	branch, err := RemoteBranch(ctx, url)
	if err != nil {
		return nil, err
	}
	if create {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		if _, err := (&Repo{root: abs}).git(ctx, "init", "--quiet"); err != nil {
			return nil, err
		}
		if r, err = Open(ctx, dir); err != nil {
			return nil, err
		}
	}
	if addOrigin {
		if _, err := r.git(ctx, "remote", "add", "origin", url); err != nil {
			return nil, err
		}
	}
	// Failing later, such as when the push is refused, undoes what this call
	// made: the repository it created, or the remote it added. The notes
	// themselves are left alone.
	defer func() {
		switch {
		case err == nil:
		case create:
			os.RemoveAll(filepath.Join(r.root, ".git"))
		case addOrigin:
			r.git(context.WithoutCancel(ctx), "remote", "remove", "origin")
		}
	}()
	res = &Result{Conflicts: []Conflict{}}
	err = locked(func() error {
		n, err := r.commit(ctx)
		if err != nil {
			return err
		}
		res.Committed = n
		if _, err := r.git(ctx, "rev-parse", "-q", "--verify", "HEAD"); err != nil { // nothing to commit in a new repository
			_, err = r.git(ctx, "commit", "--quiet", "--allow-empty", "-m", "Start notes")
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if branch == "" { // an empty remote
		if _, err := r.git(ctx, "push", "--quiet", "--set-upstream", "origin", "HEAD"); err != nil {
			return res, err
		}
		u, err := r.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
		if err != nil {
			return res, err
		}
		res.Upstream, res.Pushed = strings.TrimSpace(u), true
		return res, nil
	}
	if _, err := r.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		return res, err
	}
	if _, err := r.git(ctx, "branch", "-M", branch); err != nil {
		return res, err
	}
	if _, err := r.git(ctx, "branch", "--set-upstream-to=origin/"+branch); err != nil {
		return res, err
	}
	r.unrelated = true
	s, err := r.Sync(ctx, locked, isNote, resolve)
	if s != nil {
		s.Committed += res.Committed
	}
	return s, err
}

// Upstream returns the upstream branch of the current branch, such as
// origin/main, or "" when it has none.
func (r *Repo) Upstream(ctx context.Context) string {
	u, err := r.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u)
}

// Sync commits the changes of the notes, and, when the branch has an upstream
// branch, fetches it, merges it and pushes. locked runs f while the notes are
// not written by others; the network is used outside it.
func (r *Repo) Sync(ctx context.Context, locked func(f func() error) error, isNote IsNote, resolve Resolver) (*Result, error) {
	if err := r.checkIdle(ctx); err != nil {
		return nil, err
	}
	res := &Result{Conflicts: []Conflict{}, Upstream: r.Upstream(ctx)}
	for try := 0; ; try++ {
		if res.Upstream != "" {
			if _, err := r.git(ctx, "fetch", "--quiet"); err != nil {
				return res, err
			}
		}
		err := locked(func() error {
			n, err := r.commit(ctx)
			if err != nil {
				return err
			}
			res.Committed += n
			if res.Upstream == "" {
				return nil
			}
			return r.merge(ctx, res, isNote, resolve)
		})
		if err != nil || res.Upstream == "" {
			return res, err
		}
		ahead, err := r.git(ctx, "rev-list", "--count", "@{upstream}..HEAD")
		if err != nil {
			return res, err
		}
		if strings.TrimSpace(ahead) == "0" {
			return res, nil
		}
		_, err = r.git(ctx, "push", "--quiet")
		if err == nil {
			res.Pushed = true
			return res, nil
		}
		// The upstream moved since the fetch: merge again, once.
		if try > 0 || !strings.Contains(err.Error(), "rejected") {
			return res, err
		}
	}
}

// checkIdle returns ErrBusy when the repository is in the middle of an
// operation, which the user is to finish first.
func (r *Repo) checkIdle(ctx context.Context) error {
	for _, ref := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"} {
		if _, err := r.git(ctx, "rev-parse", "-q", "--verify", ref); err == nil {
			return fmt.Errorf("%w (%s exists); finish or abort it in %s", ErrBusy, ref, r.root)
		}
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		p, err := r.git(ctx, "rev-parse", "--git-path", d)
		if err != nil {
			return err
		}
		p = strings.TrimSpace(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.root, p)
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%w (a rebase); finish or abort it in %s", ErrBusy, r.root)
		}
	}
	if u, err := r.git(ctx, "ls-files", "-u"); err != nil {
		return err
	} else if u != "" {
		return fmt.Errorf("%w (unmerged files); resolve them in %s", ErrBusy, r.root)
	}
	return nil
}

// commit commits the changes of the notes, and returns the number of files
// committed.
func (r *Repo) commit(ctx context.Context) (int, error) {
	if _, err := r.git(ctx, append([]string{"add", "--all", "--"}, r.pathspec()...)...); err != nil {
		return 0, err
	}
	out, err := r.git(ctx, append([]string{"diff", "--cached", "--name-status", "--no-renames", "--relative=" + r.relDir(), "--"}, r.pathspec()...)...)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if out == "" {
		return 0, nil
	}
	if _, err := r.git(ctx, append([]string{"commit", "--quiet", "-m", r.message(lines), "--"}, r.pathspec()...)...); err != nil {
		return 0, err
	}
	return len(lines), nil
}

// message returns the commit message of the changed files, given as lines of
// git diff --name-status.
func (r *Repo) message(lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Update notes")
	if r.host != "" {
		fmt.Fprintf(&b, " from %s", r.host)
	}
	b.WriteString("\n\n")
	const max = 50
	for i, l := range lines {
		if i == max {
			fmt.Fprintf(&b, "... and %d more\n", len(lines)-max)
			break
		}
		b.WriteString(strings.Replace(l, "\t", " ", 1) + "\n")
	}
	return b.String()
}

// merge merges the upstream branch, resolving the conflicts.
func (r *Repo) merge(ctx context.Context, res *Result, isNote IsNote, resolve Resolver) error {
	before, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	before = strings.TrimSpace(before)
	msg := "Merge notes from " + res.Upstream
	if r.host != "" {
		msg += " on " + r.host
	}
	args := []string{"merge", "--no-edit", "-m", msg}
	if r.unrelated {
		args = append(args, "--allow-unrelated-histories")
	}
	_, mergeErr := r.git(ctx, append(args, "@{upstream}")...)
	if mergeErr != nil {
		unmerged, err := r.unmerged(ctx)
		if err != nil || len(unmerged) == 0 {
			r.abortMerge(ctx)
			if err != nil {
				return err
			}
			return mergeErr
		}
		if err := r.resolve(ctx, unmerged, res, isNote, resolve); err != nil {
			r.abortMerge(ctx)
			return err
		}
		if _, err := r.git(ctx, "commit", "--quiet", "--no-edit"); err != nil {
			r.abortMerge(ctx)
			return err
		}
	}
	out, err := r.git(ctx, "diff", "--name-only", before, "HEAD", "--", r.rel)
	if err != nil {
		return err
	}
	if out != "" {
		res.Merged = strings.Count(out, "\n")
	}
	return nil
}

func (r *Repo) abortMerge(ctx context.Context) {
	if _, err := r.git(ctx, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err == nil {
		r.git(ctx, "merge", "--abort")
	}
}

// stages are the versions of an unmerged file: the blob of ours (stage 2)
// and of theirs (stage 3); empty when the side deleted the file.
type stages struct{ ours, theirs string }

// unmerged returns the unmerged files, by their paths relative to the top.
func (r *Repo) unmerged(ctx context.Context) (map[string]*stages, error) {
	out, err := r.git(ctx, "ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	files := map[string]*stages{}
	for e := range strings.SplitSeq(out, "\x00") {
		if e == "" {
			continue
		}
		info, p, ok := strings.Cut(e, "\t")
		f := strings.Fields(info) // mode, blob, stage
		if !ok || len(f) != 3 {
			return nil, fmt.Errorf("unexpected output of git ls-files: %q", e)
		}
		s := files[p]
		if s == nil {
			s = &stages{}
			files[p] = s
		}
		switch f[2] {
		case "2":
			s.ours = f[1]
		case "3":
			s.theirs = f[1]
		}
	}
	return files, nil
}

// resolve resolves the unmerged files of a merge, all of which must be notes'
// files.
func (r *Repo) resolve(ctx context.Context, unmerged map[string]*stages, res *Result, isNote IsNote, resolve Resolver) error {
	for p := range unmerged {
		if _, ok := r.inNotes(p); !ok {
			return fmt.Errorf("a file outside the notes changed on both sides: %s; merge it by hand in %s", p, r.root)
		}
	}
	for p, s := range unmerged {
		np, _ := r.inNotes(p)
		c := Conflict{Path: np}
		keep := s.ours // this side's version, else the changed one of theirs
		if keep == "" {
			keep = s.theirs
		}
		if keep == "" { // deleted on both sides
			if _, err := r.git(ctx, "rm", "--quiet", "--cached", "--ignore-unmatch", "--", p); err != nil {
				return err
			}
			continue
		}
		if err := r.writeBlob(ctx, keep, p); err != nil {
			return err
		}
		add := []string{p}
		if s.ours != "" && s.theirs != "" && isNote(np) {
			theirs, err := r.git(ctx, "cat-file", "blob", s.theirs)
			if err != nil {
				return err
			}
			cp, err := resolve(np, []byte(theirs))
			if err != nil {
				return fmt.Errorf("keep the other version of %s: %w", np, err)
			}
			c.Copy = cp
			add = append(add, r.fromNotes(cp))
		}
		if _, err := r.git(ctx, append([]string{"add", "--"}, add...)...); err != nil {
			return err
		}
		res.Conflicts = append(res.Conflicts, c)
	}
	return nil
}

// writeBlob writes the blob to the file p, relative to the top.
func (r *Repo) writeBlob(ctx context.Context, blob, p string) error {
	b, err := r.git(ctx, "cat-file", "blob", blob)
	if err != nil {
		return err
	}
	full := filepath.Join(r.root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(b), 0o644)
}

// pathspec returns the pathspec of the files to commit: the notes directory,
// except files that are not notes' but appear there, such as those of
// Finder on macOS and the temporary files of a write in progress.
func (r *Repo) pathspec() []string {
	return []string{r.rel, ":(exclude,glob)**/.DS_Store", ":(exclude,glob)**/.tmp-*"}
}

// relDir returns the notes directory relative to the top, for --relative.
func (r *Repo) relDir() string {
	if r.rel == "." {
		return ""
	}
	return r.rel + "/"
}

// inNotes returns p, relative to the top, relative to the notes directory.
func (r *Repo) inNotes(p string) (string, bool) {
	if r.rel == "." {
		return p, true
	}
	return strings.CutPrefix(p, r.rel+"/")
}

// fromNotes returns p, relative to the notes directory, relative to the top.
func (r *Repo) fromNotes(p string) string {
	if r.rel == "." {
		return p
	}
	return path.Join(r.rel, p)
}

// git runs git in the top of the work tree and returns its output. It never
// asks for input, such as a password, so that a server in the background
// does not hang.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotePath=false"}, args...)...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return out.String(), fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return out.String(), nil
}

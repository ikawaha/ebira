package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
	"github.com/ikawaha/ebira/internal/zenn"
)

// Notes are written as articles of Zenn to the GitHub repository Zenn
// deploys from (see internal/zenn), by people only, as it publishes them.
// The note keeps the article it was written as (store.Note.Zenn), which
// changes no content, as with marks.

// zennMaxImage is the largest image Zenn takes.
const zennMaxImage = 3 << 20

// zennConfig tells where notes are written as articles of Zenn.
type zennConfig struct {
	mu       sync.Mutex // serializes writes to the clone
	dir      string
	settings func() (zenn.Settings, error)
}

// SetZenn sets the directory of the clone of the repository of the articles,
// for ebira alone, and how to read the settings of Zenn, which are read at
// each use, so that a change applies at once. Without it, notes cannot be
// written to Zenn.
func (s *Notes) SetZenn(dir string, settings func() (zenn.Settings, error)) {
	s.zenn.dir, s.zenn.settings = dir, settings
}

// zennSettings returns the settings of Zenn, empty when they cannot be read.
func (s *Notes) zennSettings() zenn.Settings {
	if s.zenn.settings == nil {
		return zenn.Settings{}
	}
	st, err := s.zenn.settings()
	if err != nil {
		return zenn.Settings{}
	}
	return st
}

// ZennStatus tells whether notes can be written to Zenn. For people only.
func (s *Notes) ZennStatus(ctx context.Context, _ *gennotes.ZennStatusPayload) (*gennotes.ZennStatus, error) {
	if !fromPerson(ctx) || s.zenn.dir == "" {
		return &gennotes.ZennStatus{}, nil
	}
	st := s.zennSettings()
	_, err := exec.LookPath("git")
	res := &gennotes.ZennStatus{Configured: st.Configured(), Git: err == nil}
	if st.Repo != "" {
		res.Repo = &st.Repo
	}
	if st.Branch != "" {
		res.Branch = &st.Branch
	}
	if st.User != "" {
		res.User = &st.User
	}
	return res, nil
}

// ExportZenn writes a note to Zenn as an article. For people only, signed in
// with a passkey recently while passkeys are required, as it publishes the
// note.
func (s *Notes) ExportZenn(ctx context.Context, p *gennotes.ExportZennPayload) (*gennotes.ZennExport, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	if err := requireVerified(ctx); err != nil {
		return nil, err
	}
	st := s.zennSettings()
	if !st.Configured() || s.zenn.dir == "" {
		return nil, gennotes.MakeBadRequest(errors.New("the repository of Zenn is not set: set zenn_repo in the settings"))
	}
	a, last, warnings, err := s.zennArticle(ctx, p, st)
	if err != nil {
		slog.Warn("cannot write the note to Zenn", "note", p.ID, "error", err)
		return nil, err
	}

	s.zenn.mu.Lock()
	res, err := zenn.Open(s.zenn.dir, st).Write(ctx, a, last, p.Force)
	s.zenn.mu.Unlock()
	if err != nil {
		// git tells more than the UI shows, such as of a push refused.
		slog.Warn("cannot write the note to Zenn", "note", p.ID, "repo", st.Repo, "branch", st.Branch, "error", err)
	}
	if errors.Is(err, zenn.ErrChanged) {
		return nil, gennotes.MakeArticleChanged(err)
	}
	if err != nil {
		return nil, gennotes.MakeBadRequest(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(p.ID, nil)
	if err != nil {
		return nil, err
	}
	n.Zenn = &store.Zenn{Slug: a.Slug, Emoji: a.Emoji, Type: a.Type, Topics: a.Topics, Published: a.Published, Hash: res.Hash, Source: a.Source}
	if err := s.write(ctx, n, true, n.Notebook); err != nil {
		return nil, err
	}
	out := &gennotes.ZennExport{Note: s.note(n), Pushed: res.Pushed, Warnings: make([]*gennotes.ZennWarning, 0, len(warnings))}
	for _, w := range warnings {
		out.Warnings = append(out.Warnings, &gennotes.ZennWarning{Kind: w.Kind, Name: w.Name})
	}
	return out, nil
}

// zennArticle makes the article of the note of p, and returns it with the
// hash of the article file written last and what was left out of it.
func (s *Notes) zennArticle(ctx context.Context, p *gennotes.ExportZennPayload, st zenn.Settings) (*zenn.Article, string, []zenn.Warning, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(p.ID, nil)
	if err != nil {
		return nil, "", nil, err
	}
	if n.Notebook == store.Trash {
		return nil, "", nil, gennotes.MakeBadRequest(errors.New("a note in the trash cannot be written to Zenn"))
	}
	if !p.Private {
		if m, err := s.store.NotebookMeta(n.Notebook); err != nil || m.Kind == store.KindPrivate {
			return nil, "", nil, gennotes.MakeBadRequest(errors.New("the note is in a private notebook: allow it with private"))
		}
	}
	old := n.Zenn
	if old == nil {
		old = &store.Zenn{}
	}
	slug, err := s.zennSlug(ctx, n.ID, old.Slug, deref(p.Slug))
	if err != nil {
		return nil, "", nil, err
	}
	a := &zenn.Article{
		Slug:      slug,
		Title:     n.Title,
		Emoji:     cmp.Or(deref(p.Emoji), old.Emoji, zenn.RandomEmoji()),
		Type:      cmp.Or(deref(p.Type), old.Type, zenn.DefaultType),
		Published: p.Published,
		Source:    store.ZennSource(n.Title, n.Body),
	}
	var warnings []zenn.Warning
	switch {
	case p.Topics != nil:
		a.Topics = p.Topics
	case old.Slug != "":
		a.Topics = old.Topics
	default:
		var dropped []string
		a.Topics, dropped = zenn.Topics(n.Tags)
		for _, t := range dropped {
			warnings = append(warnings, zenn.Warning{Kind: zenn.WarnTag, Name: t})
		}
	}
	paths := map[string]string{}
	isImage := func(name string) bool {
		if _, ok := paths[name]; ok {
			return true
		}
		path, err := s.store.File(n.ID, name)
		if err != nil || !imageName(name) {
			return false
		}
		paths[name] = path
		return true
	}
	c := zenn.Convert(n.Body, n.ID, a.Slug, isImage, func(id string) string {
		o, err := s.store.Load(id)
		if err != nil || o.Zenn == nil || o.Zenn.Slug == "" || !o.Zenn.Published {
			return "" // a draft is not seen by others
		}
		return st.ArticleURL(o.Zenn.Slug)
	})
	a.Body = c.Body
	warnings = append(warnings, c.Warnings...)
	for _, name := range c.Images {
		if err := checkZennImage(paths[name]); err != nil {
			return nil, "", nil, gennotes.MakeBadRequest(fmt.Errorf("image %s: %w", name, err))
		}
		a.Images = append(a.Images, zenn.Image{Name: name, Path: paths[name]})
	}
	if err := a.Check(); err != nil {
		return nil, "", nil, gennotes.MakeBadRequest(err)
	}
	last := ""
	if old.Slug == a.Slug {
		last = old.Hash
	}
	return a, last, warnings, nil
}

// zennSlug returns the slug of the article of note id, written before as the
// article with slug last (empty for none), given slug asked (empty for
// none). The slug is the ID of the note; one that cannot be a slug takes the
// slug asked instead. The slug of an article never changes, as Zenn takes
// another slug for another article, and no two notes share one. The caller
// holds s.mu.
func (s *Notes) zennSlug(ctx context.Context, id, last, asked string) (string, error) {
	if last != "" {
		if asked != "" && asked != last {
			return "", gennotes.MakeBadRequest(fmt.Errorf("the note was written to Zenn as %s, and the slug of an article does not change", last))
		}
		return last, nil
	}
	slug := id
	switch {
	case zenn.ValidSlug(id):
		if asked != "" && asked != id {
			return "", gennotes.MakeBadRequest(fmt.Errorf("the slug of the article is the ID of the note, %s", id))
		}
	case asked == "":
		return "", gennotes.MakeBadRequest(fmt.Errorf("the ID of the note, %s, cannot be the slug of an article, which is 12 to 50 of lower case letters, digits, hyphens and underscores: give a slug", id))
	case !zenn.ValidSlug(asked):
		return "", gennotes.MakeBadRequest(fmt.Errorf("slug %q is not 12 to 50 of lower case letters, digits, hyphens and underscores", asked))
	default:
		// Another note would take its ID as the slug.
		if _, err := s.store.Load(asked); err == nil {
			return "", gennotes.MakeBadRequest(fmt.Errorf("slug %s is the ID of another note", asked))
		}
		slug = asked
	}
	other, ok, err := s.index.ZennNote(ctx, slug, id)
	if err != nil {
		return "", err
	}
	if ok {
		return "", gennotes.MakeBadRequest(fmt.Errorf("note %s was written to Zenn as %s", other, slug))
	}
	return slug, nil
}

// imageName reports whether name is that of an image as notes keep them,
// with the extension of its type (see store.AddFile).
func imageName(name string) bool {
	for _, ext := range []string{".png", ".jpg", ".gif", ".webp"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// checkZennImage tells why the image at path cannot go to Zenn: it is too
// large, or of a type Zenn does not take.
func checkZennImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() > zennMaxImage {
		return fmt.Errorf("larger than %d MB, which Zenn does not take", zennMaxImage>>20)
	}
	head := make([]byte, 512)
	k, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	if _, ok := store.FileType(head[:k]); !ok {
		return errors.New("not an image of a type Zenn takes (PNG, JPEG, GIF or WebP)")
	}
	return nil
}

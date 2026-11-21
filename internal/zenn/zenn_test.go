package zenn

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTopics(t *testing.T) {
	topics, dropped := Topics([]string{"Go", "ＴＵＩ", "go", "C++", "a b", "検索", "toolongtopicnamemorethan18", "x", "y", "z", "w"})
	if want := []string{"go", "tui", "検索", "x", "y"}; !slices.Equal(topics, want) {
		t.Errorf("topics = %q, want %q", topics, want)
	}
	if want := []string{"C++", "a b", "toolongtopicnamemorethan18", "z", "w"}; !slices.Equal(dropped, want) {
		t.Errorf("dropped = %q, want %q", dropped, want)
	}
}

func TestEmoji(t *testing.T) {
	for _, e := range Emoji {
		if err := CheckEmoji(e); err != nil {
			t.Error(err)
		}
	}
	if !slices.Contains(Emoji, RandomEmoji()) {
		t.Error("RandomEmoji is not of Emoji")
	}
}

func TestCheck(t *testing.T) {
	ok := Article{Slug: "5d22fdc56c57a0", Title: "題名", Emoji: "📝", Type: "tech", Topics: []string{"go"}}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]func(a *Article){
		"short slug": func(a *Article) { a.Slug = "abc" },
		"upper slug": func(a *Article) { a.Slug = "ABCDEFGHIJKLMN" },
		"no title":   func(a *Article) { a.Title = " " },
		"long title": func(a *Article) { a.Title = strings.Repeat("あ", MaxTitle+1) },
		"text emoji": func(a *Article) { a.Emoji = "a" },
		"two words":  func(a *Article) { a.Emoji = "📝 x" },
		"type":       func(a *Article) { a.Type = "news" },
		"topic":      func(a *Article) { a.Topics = []string{"c++"} },
		"topics":     func(a *Article) { a.Topics = []string{"a", "b", "c", "d", "e", "f"} },
	} {
		a := ok
		f(&a)
		if a.Check() == nil {
			t.Errorf("%s: no error", name)
		}
	}
	a := ok
	a.Title = strings.Repeat("あ", MaxTitle)
	if err := a.Check(); err != nil {
		t.Errorf("title of %d characters: %v", MaxTitle, err)
	}
}

func TestFile(t *testing.T) {
	a := Article{Slug: "5d22fdc56c57a0", Title: `"引用" と: 記号`, Emoji: "🐦", Type: "idea", Body: "本文\n\n"}
	b, err := a.File()
	if err != nil {
		t.Fatal(err)
	}
	var fm struct {
		Title  string   `yaml:"title"`
		Emoji  string   `yaml:"emoji"`
		Topics []string `yaml:"topics"`
	}
	a.Topics = []string{"go", "検索"}
	b2, _ := a.File()
	if err := yaml.Unmarshal(bytes.Split(b2, []byte("---\n"))[1], &fm); err != nil || fm.Title != a.Title || fm.Emoji != a.Emoji || !slices.Equal(fm.Topics, a.Topics) {
		t.Errorf("front matter = %+v, %v", fm, err)
	}
	want := "---\ntitle: \"\\\"引用\\\" と: 記号\"\nemoji: \"🐦\"\ntype: \"idea\"\ntopics: []\npublished: false\n---\n\n本文\n"
	if string(b) != want {
		t.Errorf("file =\n%s\nwant\n%s", b, want)
	}
}

func TestConvert(t *testing.T) {
	body := strings.Join([]string{
		"![図](n1234567890ab.assets/fig.png) and ![](./n1234567890ab.assets/%E5%9B%B3.png)",
		"[資料](n1234567890ab.assets/doc.pdf) ![](n1234567890ab.assets/doc.pdf)",
		"[ほか](other123.assets/x.png)",
		"[公開](/#note/pub123) [下書き](http://localhost:13579/#note/draft1 \"t\")",
		`<img src="n1234567890ab.assets/fig.png" width="100">`,
		"`n1234567890ab.assets/fig.png` [x](https://example.com)",
		"```",
		"![図](n1234567890ab.assets/fig.png)",
		"```",
		"",
	}, "\n")
	isImage := func(name string) bool { return strings.HasSuffix(name, ".png") }
	articleURL := func(id string) string {
		if id == "pub123" {
			return "https://zenn.dev/u/articles/pub123abcdefg"
		}
		return ""
	}
	c := Convert(body, "n1234567890ab", "n1234567890ab", isImage, articleURL)
	want := strings.Join([]string{
		"![図](/images/n1234567890ab/fig.png) and ![](/images/n1234567890ab/%E5%9B%B3.png)",
		"資料 ",
		"ほか",
		"[公開](https://zenn.dev/u/articles/pub123abcdefg) 下書き",
		`<img src="/images/n1234567890ab/fig.png" width="100">`,
		"`n1234567890ab.assets/fig.png` [x](https://example.com)",
		"```",
		"![図](n1234567890ab.assets/fig.png)",
		"```",
		"",
	}, "\n")
	if c.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", c.Body, want)
	}
	if want := []string{"fig.png", "図.png"}; !slices.Equal(c.Images, want) {
		t.Errorf("images = %q, want %q", c.Images, want)
	}
	if want := []Warning{{WarnFile, "doc.pdf"}, {WarnFile, "x.png"}, {WarnNote, "draft1"}}; !slices.Equal(c.Warnings, want) {
		t.Errorf("warnings = %v, want %v", c.Warnings, want)
	}
}

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

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// show returns a file of the branch main of the bare repository remote.
func show(t *testing.T, remote, path string) string {
	t.Helper()
	return git(t, remote, "show", "main:"+path)
}

func TestWrite(t *testing.T) {
	gitEnv(t)
	ctx := context.Background()
	tmp := t.TempDir()
	remote := filepath.Join(tmp, "remote.git")
	git(t, tmp, "init", "-q", "--bare", "-b", "main", remote)
	img := filepath.Join(tmp, "fig.png")
	if err := os.WriteFile(img, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Open(filepath.Join(tmp, "state", "zenn"), Settings{Repo: remote})
	a := &Article{Slug: "n1234567890ab", Title: "題名", Emoji: "📝", Type: "tech", Body: "![](/images/n1234567890ab/fig.png)", Images: []Image{{Name: "fig.png", Path: img}}}

	// A new repository without a branch.
	res, err := r.Write(ctx, a, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pushed {
		t.Error("not pushed")
	}
	if got := show(t, remote, "images/n1234567890ab/fig.png"); got != "png" {
		t.Errorf("image = %q", got)
	}
	first := res.Hash

	// Nothing changed: no commit.
	res, err = r.Write(ctx, a, first, false)
	if err != nil || res.Pushed || res.Hash != first {
		t.Fatalf("again = %+v, %v", res, err)
	}

	// Another clone pushes meanwhile, editing the article.
	other := filepath.Join(tmp, "other")
	git(t, tmp, "clone", "-q", remote, other)
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "-A")
	git(t, other, "commit", "-q", "-m", "readme")
	git(t, other, "push", "-q", "origin", "main")

	// The image is gone from the article: so from the repository.
	a.Images, a.Body, a.Published = nil, "本文", true
	res, err = r.Write(ctx, a, first, false)
	if err != nil || !res.Pushed {
		t.Fatalf("update = %+v, %v", res, err)
	}
	if got := show(t, remote, "articles/n1234567890ab.md"); !strings.Contains(got, "published: true") || !strings.HasSuffix(got, "本文\n") {
		t.Errorf("article = %q", got)
	}
	if got := show(t, remote, "README.md"); got != "hi" {
		t.Errorf("README = %q", got)
	}
	if out := git(t, remote, "ls-tree", "-r", "--name-only", "main"); strings.Contains(out, "images/") {
		t.Errorf("images left: %s", out)
	}
	second := res.Hash

	// Edited on GitHub: refused unless forced.
	git(t, other, "pull", "-q")
	if err := os.WriteFile(filepath.Join(other, "articles", "n1234567890ab.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, other, "commit", "-q", "-am", "edit")
	git(t, other, "push", "-q", "origin", "main")
	a.Body = "本文 2"
	if _, err := r.Write(ctx, a, second, false); !errors.Is(err, ErrChanged) {
		t.Fatalf("err = %v, want ErrChanged", err)
	}
	if _, err := r.Write(ctx, a, second, true); err != nil {
		t.Fatal(err)
	}
	if got := show(t, remote, "articles/n1234567890ab.md"); !strings.HasSuffix(got, "本文 2\n") {
		t.Errorf("article = %q", got)
	}

	// A new article cannot take the slug of an article ebira did not write,
	// nor of the images of one, even forced; the same file is no change.
	made := &Article{Slug: "made-by-hand-1", Title: "手で作った記事", Emoji: "📝", Type: "tech", Body: "x"}
	git(t, other, "pull", "-q")
	if err := os.MkdirAll(filepath.Join(other, "articles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "articles", "made-by-hand-1.md"), []byte("by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(other, "images", "made-by-hand-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "images", "made-by-hand-2", "fig.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "-A")
	git(t, other, "commit", "-q", "-m", "by hand")
	git(t, other, "push", "-q", "origin", "main")
	for _, slug := range []string{"made-by-hand-1", "made-by-hand-2"} {
		made.Slug = slug
		if _, err := r.Write(ctx, made, "", true); !errors.Is(err, ErrExists) {
			t.Errorf("%s: err = %v, want ErrExists", slug, err)
		}
	}
	if got := show(t, remote, "images/made-by-hand-2/fig.png"); got != "png" {
		t.Errorf("image = %q", got)
	}

	// Another repository: cloned anew.
	remote2 := filepath.Join(tmp, "remote2.git")
	git(t, tmp, "init", "-q", "--bare", "-b", "main", remote2)
	r2 := Open(filepath.Join(tmp, "state", "zenn"), Settings{Repo: remote2})
	if _, err := r2.Write(ctx, a, "", false); err != nil {
		t.Fatal(err)
	}
	if got := show(t, remote2, "articles/n1234567890ab.md"); !strings.HasSuffix(got, "本文 2\n") {
		t.Errorf("article = %q", got)
	}
}

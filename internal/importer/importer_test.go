package importer

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	now   = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	mtime = time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
)

func parse(t *testing.T, name, content string, mod time.Time) *Note {
	t.Helper()
	n, err := Parse(Input{Name: name, Content: content, ModTime: mod, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func extraKeys(n *Note) []string {
	var keys []string
	for i := 0; i+1 < len(n.Extra); i += 2 {
		keys = append(keys, n.Extra[i].Value+"="+n.Extra[i+1].Value)
	}
	return keys
}

func TestFrontMatter(t *testing.T) {
	src := "\uFEFF---\r\n" +
		"title: 京都の旅\r\n" +
		"tags: [旅行, \"#京都\", 旅行]\r\n" +
		"date: 2026-04-01\r\n" +
		"updated: 2026-04-03T10:00:00+09:00\r\n" +
		"author: me\r\n" +
		"created_by: person\r\n" +
		"order: 5\r\n" +
		"aliases: [kyoto]\r\n" +
		"---\r\n" +
		"# 京都の旅\r\n" +
		"\r\n" +
		"一日目は**清水寺**。\r\n"
	n := parse(t, "kyoto.md", src, mtime)
	if n.Title != "京都の旅" {
		t.Errorf("Title = %q", n.Title)
	}
	if !slices.Equal(n.Tags, []string{"旅行", "京都"}) {
		t.Errorf("Tags = %q", n.Tags)
	}
	if want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local).UTC(); !n.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", n.CreatedAt, want)
	}
	if want := time.Date(2026, 4, 3, 1, 0, 0, 0, time.UTC); !n.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", n.UpdatedAt, want)
	}
	// The heading repeating the title is left out; CRLF and the BOM are gone.
	if n.Body != "一日目は**清水寺**。\n" {
		t.Errorf("Body = %q", n.Body)
	}
	if got := extraKeys(n); !slices.Equal(got, []string{"author=me", "aliases="}) {
		t.Errorf("Extra = %q", got)
	}
	if len(n.Warnings) != 1 || !strings.Contains(n.Warnings[0], "created_by, order") {
		t.Errorf("Warnings = %q", n.Warnings)
	}
	// The hash does not depend on the BOM and line endings.
	plain := strings.ReplaceAll(strings.TrimPrefix(src, "\uFEFF"), "\r\n", "\n")
	if n2 := parse(t, "other.md", plain, time.Time{}); n2.Hash != n.Hash || !strings.HasPrefix(n.Hash, "sha256:") {
		t.Errorf("Hash = %q and %q", n.Hash, n2.Hash)
	}
}

func TestTitle(t *testing.T) {
	tests := []struct {
		name, file, content, title, body string
	}{
		{"heading", "a.md", "\n# 見出し #\n\n本文\n", "見出し", "本文\n"},
		{"file name", "メモ 2026.md", "本文だけ\n", "メモ 2026", "本文だけ\n"},
		{"file name in a path", `C:\notes\memo.markdown`, "x", "memo", "x"},
		{"level 2 is not the title", "a.md", "## 小見出し\n本文", "a", "## 小見出し\n本文"},
		{"a heading not at the top", "a.md", "前置き\n# 見出し\n## 次\n# 二つ目\n", "見出し", "前置き\n# 見出し\n## 次\n# 二つ目\n"},
		{"not a heading in code", "a.md", "前置き\n```sh\n# コメント\n```\n# 見出し\n", "見出し", "前置き\n```sh\n# コメント\n```\n# 見出し\n"},
		{"only in code", "a.md", "~~~\n# コメント\n```\n# まだ\n~~~\n", "a", "~~~\n# コメント\n```\n# まだ\n~~~\n"},
		{"front matter title and another heading", "a.md", "---\ntitle: T\n---\n# H\nbody", "T", "# H\nbody"},
		{"empty front matter", "a.md", "---\n---\n# H\nbody", "H", "body"},
		{"no name", "", "", "Untitled", ""},
		{"NFC", "か\u3099.md", "", "が", ""},
	}
	for _, tt := range tests {
		n := parse(t, tt.file, tt.content, time.Time{})
		if n.Title != tt.title || n.Body != tt.body {
			t.Errorf("%s: title %q, body %q; want %q, %q", tt.name, n.Title, n.Body, tt.title, tt.body)
		}
	}
}

func TestBrokenFrontMatter(t *testing.T) {
	for _, src := range []string{"---\ntitle: [unclosed\n---\nbody", "---\n- a list\n---\nbody"} {
		n := parse(t, "a.md", src, time.Time{})
		if n.Body != src || len(n.Warnings) != 1 {
			t.Errorf("%q: body %q, warnings %q", src, n.Body, n.Warnings)
		}
	}
	// Without a closing line it is not front matter.
	if n := parse(t, "a.md", "---\ntitle: x\nbody", time.Time{}); n.Title != "a" || len(n.Warnings) != 0 {
		t.Errorf("unclosed: %+v", n)
	}
}

func TestTimes(t *testing.T) {
	// Without times in the front matter: the file's time, else now.
	n := parse(t, "a.md", "x", mtime)
	if !n.CreatedAt.Equal(mtime) || !n.UpdatedAt.Equal(mtime) {
		t.Errorf("from the file: %v, %v", n.CreatedAt, n.UpdatedAt)
	}
	n = parse(t, "a.md", "x", time.Time{})
	if !n.CreatedAt.Equal(now) || !n.UpdatedAt.Equal(now) {
		t.Errorf("now: %v, %v", n.CreatedAt, n.UpdatedAt)
	}
	// A creation time only: the file's time is the update time when later.
	n = parse(t, "a.md", "---\ncreated: 2026-01-02T03:04:05Z\n---\nx", mtime)
	if !n.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || !n.UpdatedAt.Equal(mtime) {
		t.Errorf("created only: %v, %v", n.CreatedAt, n.UpdatedAt)
	}
	// An update time before the creation time is moved up to it.
	n = parse(t, "a.md", "---\ncreated: 2026-05-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nx", time.Time{})
	if !n.UpdatedAt.Equal(n.CreatedAt) {
		t.Errorf("updated before created: %v, %v", n.CreatedAt, n.UpdatedAt)
	}
	n = parse(t, "a.md", "---\ndate: yesterday\n---\nx", mtime)
	if !n.CreatedAt.Equal(mtime) || len(n.Warnings) != 1 {
		t.Errorf("unreadable time: %v, %q", n.CreatedAt, n.Warnings)
	}
}

func TestTags(t *testing.T) {
	for src, want := range map[string][]string{
		"tags: a, b ,, a":                {"a", "b"},
		"tags: [x, '#y']":                {"x", "y"},
		"tags:\n  - 日本語\n  - [nested]\n": {"日本語"},
	} {
		n := parse(t, "a.md", "---\n"+src+"\n---\n", time.Time{})
		if !slices.Equal(n.Tags, want) {
			t.Errorf("%q: Tags = %q, want %q", src, n.Tags, want)
		}
	}
}

func TestImages(t *testing.T) {
	body := "![a](img/a.png)\n" +
		"![b](<my images/b.png> \"title\")\n" +
		"![remote](https://example.com/c.png)\n" +
		"![abs](/tmp/d.png)\n" +
		"![data](data:image/png;base64,xxx)\n" +
		"<img alt=\"e\" src=\"./e.jpg\">\n" +
		"![a again](img/a.png)\n" +
		"[a link](doc.pdf)\n"
	n := parse(t, "a.md", body, time.Time{})
	if want := []string{"img/a.png", "my images/b.png", "./e.jpg"}; !slices.Equal(n.Images, want) {
		t.Errorf("Images = %q, want %q", n.Images, want)
	}
	got := RewriteImages(n.Body, map[string]string{
		"img/a.png":       "n1.assets/a.png",
		"my images/b.png": "n1.assets/b.png",
		"./e.jpg":         "n1.assets/e.jpg",
	})
	want := "![a](n1.assets/a.png)\n" +
		"![b](n1.assets/b.png \"title\")\n" +
		"![remote](https://example.com/c.png)\n" +
		"![abs](/tmp/d.png)\n" +
		"![data](data:image/png;base64,xxx)\n" +
		"<img alt=\"e\" src=\"n1.assets/e.jpg\">\n" +
		"![a again](n1.assets/a.png)\n" +
		"[a link](doc.pdf)\n"
	if got != want {
		t.Errorf("RewriteImages:\n%s\nwant:\n%s", got, want)
	}
}

func TestTooLarge(t *testing.T) {
	if _, err := Parse(Input{Name: "a.md", Content: strings.Repeat("x", MaxSize+1)}); err != ErrTooLarge {
		t.Errorf("err = %v", err)
	}
}

// The kept keys can be written back as YAML.
func TestExtraNodes(t *testing.T) {
	n := parse(t, "a.md", "---\nauthor: me # comment\nlist: [1, 2]\n---\nx", time.Time{})
	m := &yaml.Node{Kind: yaml.MappingNode, Content: n.Extra}
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "author: me # comment\nlist: [1, 2]\n" {
		t.Errorf("got %q", got)
	}
}

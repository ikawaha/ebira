// Package guide adds the guide of ebira to a notes directory: a notebook of
// notes that tell how to use ebira, each showing the feature it tells about,
// such as marks, separators and a sub notebook. The server adds it to a new
// notes directory, and people can add it from the web UI at any time.
//
// The notes are Markdown files embedded for each language (content/<lang>),
// converted as imports are (see internal/importer), and guide.yaml there
// tells their order, the separators between them, their marks and the sub
// notebooks. Notes link to each other by their file names, such as
// [search](search.md), which become links to the new notes.
package guide

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"

	"github.com/ikawaha/ebira/internal/importer"
	"github.com/ikawaha/ebira/internal/store"
)

// Version is the version of the guide, in the name of its notebook, so that
// guides added at different times tell apart: the date the content changed,
// as YYYY-MM-DD (YYYY-MM before 2026-11-07). Raise it when the content
// changes, not with each version of ebira.
const Version = "2026-11-12"

// DefaultLang is the language of the guide for a language without one.
const DefaultLang = "en"

//go:embed content
var content embed.FS

// manifest is guide.yaml of a language.
type manifest struct {
	Name  string `yaml:"name"`
	Items []item `yaml:"items"`
}

// item is a note, a separator or a sub notebook of the guide, in order.
type item struct {
	Note      string `yaml:"note"`      // file name of the note
	Mark      string `yaml:"mark"`      // mark of the note
	Separator string `yaml:"separator"` // heading of the separator
	Color     string `yaml:"color"`     // color of the separator
	Sub       string `yaml:"sub"`       // ID of the sub notebook in the guide
	Name      string `yaml:"name"`      // display name of the sub notebook
	Items     []item `yaml:"items"`     // notes and separators of the sub notebook
}

// Langs returns the languages of the guide.
func Langs() []string {
	entries, _ := content.ReadDir("content")
	var langs []string
	for _, e := range entries {
		langs = append(langs, e.Name())
	}
	return langs
}

// Names returns the name of the guide in each language: the names of its
// notebooks are these, followed by the version.
func Names() []string {
	var names []string
	for _, lang := range Langs() {
		b, err := content.ReadFile(path.Join("content", lang, "guide.yaml"))
		if err != nil {
			continue
		}
		var m manifest
		if yaml.Unmarshal(b, &m) == nil && m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names
}

// IsNew reports whether dir is a notes directory to be made: missing, or
// with no files other than the settings shared with the notes (which the
// setup writes first) and .DS_Store. The guide goes only to such a
// directory, so that a guide deleted does not come back.
func IsNew(dir string) bool {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() != store.ConfigFile && e.Name() != ".DS_Store" {
			return false
		}
	}
	return true
}

// Result tells the notebook the guide was added as.
type Result struct {
	Notebook string // ID
	Name     string // display name
}

// orderStep is the gap between the keys of the notes and separators in the
// manual order, as when a notebook's order is renumbered.
const orderStep = 1000

// Install adds the guide in language lang (DefaultLang for one without a
// guide) to st as a new notebook after the others, made at time now. It
// never touches what is there: a guide added before stays, and the new one
// takes another ID and name. The caller indexes the new notes.
func Install(st *store.Store, lang string, now time.Time) (Result, error) {
	if !slices.Contains(Langs(), lang) {
		lang = DefaultLang
	}
	dir := path.Join("content", lang)
	b, err := content.ReadFile(path.Join(dir, "guide.yaml"))
	if err != nil {
		return Result{}, err
	}
	var m manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Result{}, fmt.Errorf("guide %s: %w", lang, err)
	}

	nb, name, order, err := newNotebook(st, m.Name+" "+Version)
	if err != nil {
		return Result{}, err
	}
	// Each note takes a new ID, as IDs are unique among all notebooks; the
	// links between them follow.
	ids := map[string]string{}
	var collect func([]item)
	collect = func(items []item) {
		for _, it := range items {
			if it.Note != "" {
				ids[it.Note] = store.NewID()
			}
			collect(it.Items)
		}
	}
	collect(m.Items)

	if err := st.SaveNotebookMeta(nb, store.NotebookMeta{Name: name, Order: order}); err != nil {
		return Result{}, err
	}
	if err := fill(st, nb, dir, m.Items, ids, now); err != nil {
		return Result{}, err
	}
	for _, it := range m.Items {
		if it.Sub == "" {
			continue
		}
		sub := nb + "/" + it.Sub
		if err := st.SaveNotebookMeta(sub, store.NotebookMeta{Name: it.Name}); err != nil {
			return Result{}, err
		}
		if err := fill(st, sub, dir, it.Items, ids, now); err != nil {
			return Result{}, err
		}
	}
	return Result{Notebook: nb, Name: name}, nil
}

// newNotebook returns the ID, display name and order of a new top notebook
// named name: with " (2)" and so on when the name is taken, with an ID not
// taken in or out of the trash, and after the other notebooks.
func newNotebook(st *store.Store, name string) (id, display string, order int, err error) {
	nbs, err := st.Notebooks()
	if err != nil {
		return "", "", 0, err
	}
	trashed, err := st.TrashedNotebooks()
	if err != nil {
		return "", "", 0, err
	}
	taken := map[string]bool{store.Inbox: true, store.Trash: true}
	names := map[string]bool{}
	for _, nb := range nbs {
		taken[nb] = true
		if _, sub := store.SplitNotebook(nb); sub != "" || nb == store.Inbox || nb == store.Trash {
			continue
		}
		meta, err := st.NotebookMeta(nb)
		if err != nil {
			continue // a broken settings file blocks no name
		}
		names[strings.ToLower(norm.NFC.String(cmp.Or(meta.Name, store.NotebookName(nb))))] = true
		order = max(order, meta.Order)
	}
	for _, nb := range trashed {
		taken[nb] = true
	}
	display = name
	for i := 2; names[strings.ToLower(norm.NFC.String(display))]; i++ {
		display = fmt.Sprintf("%s (%d)", name, i)
	}
	id = store.UniqueNotebookID(store.NotebookID(display), func(id string) bool { return taken[id] })
	return id, display, order + 10, nil
}

// fill writes the notes and separators of items to notebook nb, in their
// order, and leaves out the sub notebooks.
func fill(st *store.Store, nb, dir string, items []item, ids map[string]string, now time.Time) error {
	var seps []store.Separator
	key := int64(len(items)) * orderStep
	for _, it := range items {
		switch {
		case it.Separator != "":
			seps = append(seps, store.Separator{ID: store.NewSeparatorID(), Title: it.Separator, Color: it.Color, Order: key})
		case it.Note != "":
			b, err := content.ReadFile(path.Join(dir, it.Note))
			if err != nil {
				return err
			}
			p, err := importer.Parse(importer.Input{Name: it.Note, Content: string(b), ModTime: now, Now: now})
			if err != nil {
				return fmt.Errorf("guide %s: %w", it.Note, err)
			}
			k := key
			n := &store.Note{
				ID:        ids[it.Note],
				Notebook:  nb,
				Title:     p.Title,
				Tags:      p.Tags,
				Marks:     store.CleanMarks([]string{it.Mark}),
				CreatedAt: now,
				UpdatedAt: now,
				Order:     &k,
				Body:      links(p.Body, ids),
			}
			if err := st.Create(n); err != nil {
				return err
			}
		default:
			continue
		}
		key -= orderStep
	}
	if len(seps) == 0 {
		return nil
	}
	return st.SaveSeparators(nb, seps)
}

// linkRe matches the target of a Markdown link to a file of the guide.
var linkRe = regexp.MustCompile(`\]\(([a-z0-9-]+\.md)\)`)

// links makes the links to the files of the guide in body links to their
// notes.
func links(body string, ids map[string]string) string {
	return linkRe.ReplaceAllStringFunc(body, func(s string) string {
		if id, ok := ids[linkRe.FindStringSubmatch(s)[1]]; ok {
			return "](/#note/" + id + ")"
		}
		return s
	})
}

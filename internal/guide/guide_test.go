package guide

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ikawaha/ebira/internal/store"
)

var now = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// notesOf returns the notes of notebook nb in st, in the manual order.
func notesOf(t *testing.T, st *store.Store, nb string) []*store.Note {
	t.Helper()
	files, err := st.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var notes []*store.Note
	for _, f := range files {
		if f.Notebook != nb {
			continue
		}
		n, err := st.LoadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		notes = append(notes, n)
	}
	slices.SortFunc(notes, func(a, b *store.Note) int { return int(b.SortKey() - a.SortKey()) })
	return notes
}

func TestInstall(t *testing.T) {
	for _, lang := range Langs() {
		t.Run(lang, func(t *testing.T) {
			st, err := store.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r, err := Install(st, lang, now)
			if err != nil {
				t.Fatal(err)
			}
			var m manifest
			b, _ := content.ReadFile(path.Join("content", lang, "guide.yaml"))
			if err := yaml.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			if want := m.Name + " " + Version; r.Name != want {
				t.Errorf("name = %q, want %q", r.Name, want)
			}
			meta, err := st.NotebookMeta(r.Notebook)
			if err != nil || meta.Name != r.Name || meta.Kind != "" {
				t.Errorf("meta = %+v, %v", meta, err)
			}

			var wantNotes, wantSeps []string
			for _, it := range m.Items {
				if it.Note != "" {
					wantNotes = append(wantNotes, it.Note)
				}
				if it.Separator != "" {
					wantSeps = append(wantSeps, it.Separator)
				}
			}
			notes := notesOf(t, st, r.Notebook)
			if len(notes) != len(wantNotes) {
				t.Fatalf("%d notes, want %d", len(notes), len(wantNotes))
			}
			seps, err := st.Separators(r.Notebook)
			if err != nil {
				t.Fatal(err)
			}
			var titles []string
			for _, s := range seps {
				titles = append(titles, s.Title)
			}
			if !slices.Equal(titles, wantSeps) {
				t.Errorf("separators = %q, want %q", titles, wantSeps)
			}
			// The first separator comes before the first note.
			if len(seps) > 0 && seps[0].Order <= notes[0].SortKey() {
				t.Errorf("the first separator (%d) is not above the first note (%d)", seps[0].Order, notes[0].SortKey())
			}
			if notes[0].Title == "" || !slices.Equal(notes[0].Marks, []string{"star"}) {
				t.Errorf("first note = %q %v", notes[0].Title, notes[0].Marks)
			}

			ids := map[string]bool{}
			var all []*store.Note
			all = append(all, notes...)
			for _, it := range m.Items {
				if it.Sub == "" {
					continue
				}
				sub := r.Notebook + "/" + it.Sub
				sm, err := st.NotebookMeta(sub)
				if err != nil || sm.Name != it.Name {
					t.Errorf("sub %s = %+v, %v", sub, sm, err)
				}
				if got := notesOf(t, st, sub); len(got) != len(it.Items) {
					t.Errorf("sub %s: %d notes, want %d", sub, len(got), len(it.Items))
				} else {
					all = append(all, got...)
				}
			}
			for _, n := range all {
				ids[n.ID] = true
				if n.Title == "" || !slices.Contains(n.Tags, "guide") {
					t.Errorf("note %s: title %q, tags %v", n.ID, n.Title, n.Tags)
				}
			}
			// Every link to a file of the guide became a link to its note.
			linked := regexp.MustCompile(`\]\(/#note/([a-z0-9]+)\)`)
			for _, n := range all {
				if strings.Contains(n.Body, ".md)") {
					t.Errorf("note %q has a link to a file left", n.Title)
				}
				for _, l := range linked.FindAllStringSubmatch(n.Body, -1) {
					if !ids[l[1]] {
						t.Errorf("note %q links to %s, not a note of the guide", n.Title, l[1])
					}
				}
			}
		})
	}
}

// TestFiles checks that each language has the same notes, and that every
// file is in its guide.
func TestFiles(t *testing.T) {
	var first []string
	for _, lang := range Langs() {
		entries, err := content.ReadDir(path.Join("content", lang))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if first == nil {
			first = names
		} else if !slices.Equal(names, first) {
			t.Errorf("%s has %v, unlike %v", lang, names, first)
		}
		var m manifest
		b, _ := content.ReadFile(path.Join("content", lang, "guide.yaml"))
		if err := yaml.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		used := []string{"guide.yaml"}
		var walk func([]item)
		walk = func(items []item) {
			for _, it := range items {
				if it.Note != "" {
					used = append(used, it.Note)
				}
				if it.Mark != "" && !store.ValidMark(it.Mark) {
					t.Errorf("%s: unknown mark %s", lang, it.Mark)
				}
				if it.Color != "" && !store.ValidSeparatorColor(it.Color) {
					t.Errorf("%s: unknown color %s", lang, it.Color)
				}
				walk(it.Items)
			}
		}
		walk(m.Items)
		for _, n := range names {
			if !slices.Contains(used, n) {
				t.Errorf("%s: %s is not in the guide", lang, n)
			}
		}
	}
	if !slices.Contains(Langs(), DefaultLang) {
		t.Errorf("no guide in %s", DefaultLang)
	}
}

func TestInstallAgain(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r1, err := Install(st, "en", now)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Install(st, "en", now)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Notebook == r1.Notebook || r2.Name != r1.Name+" (2)" {
		t.Errorf("second guide = %+v, first %+v", r2, r1)
	}
	m1, _ := st.NotebookMeta(r1.Notebook)
	m2, _ := st.NotebookMeta(r2.Notebook)
	if m2.Order <= m1.Order {
		t.Errorf("order %d is not after %d", m2.Order, m1.Order)
	}
	// The first guide is left as it was.
	if n := len(notesOf(t, st, r1.Notebook)); n != len(notesOf(t, st, r2.Notebook)) {
		t.Errorf("the first guide has %d notes", n)
	}

	// An unknown language falls back to the default.
	r3, err := Install(st, "xx", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r3.Name, r1.Name) {
		t.Errorf("guide in xx = %q", r3.Name)
	}
}

func TestIsNew(t *testing.T) {
	dir := t.TempDir()
	if !IsNew(filepath.Join(dir, "missing")) {
		t.Error("a missing directory is not new")
	}
	if !IsNew(dir) {
		t.Error("an empty directory is not new")
	}
	for _, name := range []string{store.ConfigFile, ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !IsNew(dir) {
		t.Error("a directory with only the settings is not new")
	}
	if err := os.Mkdir(filepath.Join(dir, store.Inbox), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsNew(dir) {
		t.Error("a directory with the inbox is new")
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) != len(Langs()) {
		t.Fatalf("Names() = %q, want one for each of %q", names, Langs())
	}
	for _, n := range names {
		if n == "" {
			t.Errorf("an empty name in %q", names)
		}
	}
}

// TestVersion checks the form of Version, which the web UI reads in the names
// of the notebooks of the guide to open the newest (openGuide).
func TestVersion(t *testing.T) {
	if _, err := time.Parse(time.DateOnly, Version); err != nil {
		t.Errorf("Version %q is not a date as YYYY-MM-DD: %v", Version, err)
	}
}

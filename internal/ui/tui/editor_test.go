package tui

import (
	"os"
	"slices"
	"testing"
)

func TestDraftRoundTrip(t *testing.T) {
	for _, id := range []string{"", "my-note"} {
		b, err := formatDraft(id, "work", "タイトル: colon", []string{"a", "b c"}, "# body\n\ntext\n")
		if err != nil {
			t.Fatal(err)
		}
		fm, body, err := parseDraft(b)
		if err != nil {
			t.Fatal(err)
		}
		if fm.ID != id || fm.Notebook != "work" || fm.Title != "タイトル: colon" || !slices.Equal(fm.Tags, []string{"a", "b c"}) || body != "# body\n\ntext\n" {
			t.Errorf("got %+v %q from:\n%s", fm, body, b)
		}
	}
}

func TestParseDraft(t *testing.T) {
	tests := []struct {
		in      string
		title   string
		body    string
		wantErr bool
	}{
		{in: "---\ntitle: T\ntags: []\n---\nbody", title: "T", body: "body"},
		{in: "---\nid: \" x \" # comment\ntitle: T\n---\nbody", title: "T", body: "body"},
		{in: "---\r\ntitle: T\r\n---\r\nbody\r\n", title: "T", body: "body\n"},
		{in: "---\ntitle: T\n---", title: "T", body: ""},
		{in: "---\ntitle: T\n---\n", title: "T", body: ""},
		{in: "title: T\nbody", wantErr: true},
		{in: "---\ntitle: T\nbody", wantErr: true},
	}
	for _, tt := range tests {
		fm, body, err := parseDraft([]byte(tt.in))
		title := fm.Title
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDraft(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (title != tt.title || body != tt.body) {
			t.Errorf("parseDraft(%q) = %q, %q; want %q, %q", tt.in, title, body, tt.title, tt.body)
		}
	}
}

func TestDraftLoad(t *testing.T) {
	tests := []struct {
		name    string
		id      string // ID of the note being edited; empty for a new note
		content string
		wantID  string
		wantErr bool
	}{
		{name: "new with id", content: "---\nid: my-note\ntitle: T\n---\n", wantID: "my-note"},
		{name: "new without id", content: "---\nid: \"\"\ntitle: T\n---\n", wantID: ""},
		{name: "invalid id", content: "---\nid: Bad ID\ntitle: T\n---\n", wantErr: true},
		{name: "no title", content: "---\nid: x\ntitle: \"\"\n---\n", wantErr: true},
		{name: "existing keeps id", id: "x", content: "---\nid: x\ntitle: T2\n---\n", wantID: "x"},
		{name: "existing with id removed", id: "x", content: "---\ntitle: T2\n---\n", wantID: ""},
		{name: "existing with id changed", id: "x", content: "---\nid: y\ntitle: T2\n---\n", wantErr: true},
		{name: "invalid notebook", content: "---\nnotebook: Bad Name\ntitle: T\n---\n", wantErr: true},
		{name: "move to trash", id: "x", content: "---\nnotebook: trash\ntitle: T\n---\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := newDraft(tt.id, "inbox", "", "T", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			defer d.remove()
			if err := os.WriteFile(d.path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := d.load()
			if !changed {
				t.Fatal("changed = false")
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("load error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && d.newID != tt.wantID {
				t.Errorf("newID = %q, want %q", d.newID, tt.wantID)
			}
		})
	}
}

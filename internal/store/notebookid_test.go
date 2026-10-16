package store

import (
	"strings"
	"testing"
)

func TestPunycode(t *testing.T) {
	// Expected values from Python's "punycode" codec, an RFC 3492 implementation.
	for in, want := range map[string]string{
		"開発メモ":          "4dkc0654c57za",
		"アイデア帳":         "cckaf4lq93u",
		"読書メモ":          "4dkc0562bx33a",
		"goa-設計":        "goa--3y7l9g",
		"2026年の目標と振り返り": "2026-ec4c9a3oa6986hc5jiqsjk0bvw8d",
		"café":          "caf-dma",
		"ü":             "tda",
		"日本語ノート":        "fdkl2l208szlbtz6h",
		"mañana-2":      "maana-2-5za",
	} {
		if got := punycode(in); got != want {
			t.Errorf("punycode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPunydecode(t *testing.T) {
	for _, s := range []string{"開発メモ", "アイデア帳", "goa-設計", "2026年の目標と振り返り", "café", "ü", "mañana-2", "a😀b"} {
		got, err := punydecode(punycode(s))
		if err != nil || got != s {
			t.Errorf("punydecode(punycode(%q)) = %q, %v", s, got, err)
		}
	}
	for _, bad := range []string{"4dkc0654c57z!", "zzzzzzzzzzzzzzzzzzzzzzzzz9", "café-dma", "4dkc0654c57z"} {
		if got, err := punydecode(bad); err == nil {
			t.Errorf("punydecode(%q) = %q, want an error", bad, got)
		}
	}
}

func TestNotebookName(t *testing.T) {
	long := strings.Repeat("とても長いノートブックの名前", 5)
	tests := []struct{ id, want string }{
		{"xn--4dkc0654c57za", "開発メモ"},
		{"xn--goa--3y7l9g", "goa-設計"}, // made from "Goa 設計"
		{"xn----0hud5732eeq6a", "開発-メモ"},
		{"xn--4dkc0654c57za-2", "開発メモ (2)"},
		{"xn--4dkc0654c57za-12", "開発メモ (12)"},
		// Not restorable: kept as is.
		{"books", "books"},
		{"my-notes-2", "my-notes-2"},
		{"xn--4dkc0654c57za-02", "xn--4dkc0654c57za-02"},
		// Decodes, but to a name NotebookID makes a different ID from.
		{"xn--" + punycode("開発 メモ"), "xn--" + punycode("開発 メモ")},
		{"xn--abc", "xn--abc"},
		{"xn--", "xn--"},
		{NotebookID(long), NotebookID(long)}, // cut for its length
	}
	for _, tt := range tests {
		if got := NotebookName(tt.id); got != tt.want {
			t.Errorf("NotebookName(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
	// Every ID made from a name that needs no lower-casing or separators gives the name back.
	for _, name := range []string{"開発メモ", "アイデア帳", "料理のレシピ", "読書メモ-2026", "日本語ノート"} {
		if got := NotebookName(NotebookID(name)); got != name {
			t.Errorf("NotebookName(NotebookID(%q)) = %q", name, got)
		}
	}
}

func TestNotebookID(t *testing.T) {
	tests := []struct{ name, want string }{
		{"books", "books"},
		{"My Notes", "my-notes"},
		{"  Goa / gRPC: memo!  ", "goa-grpc-memo"},
		{"a_b-c", "a_b-c"},
		{"_-hidden", "hidden"},
		{"開発メモ", "xn--4dkc0654c57za"},
		{"Goa 設計", "xn--goa--3y7l9g"},
		// Japanese punctuation and spaces separate words like ASCII ones.
		{"開発・メモ", "xn----0hud5732eeq6a"},
		{"開発　メモ", "xn----0hud5732eeq6a"},
		// NFC: a decomposed kana gives the same ID as the composed one.
		{"ガイド", NotebookID("ガイド")},
		{"!!!", "notebook"},
		{"", "notebook"},
	}
	for _, tt := range tests {
		got := NotebookID(tt.name)
		if got != tt.want {
			t.Errorf("NotebookID(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if !ValidNotebook(got) {
			t.Errorf("NotebookID(%q) = %q is not a valid notebook ID", tt.name, got)
		}
	}
}

func TestNotebookIDLong(t *testing.T) {
	long := strings.Repeat("とても長いノートブックの名前", 5)
	id := NotebookID(long)
	if len(id) != maxIDLen || !ValidNotebook(id) || !strings.HasPrefix(id, "xn--") {
		t.Errorf("NotebookID(long) = %q (%d bytes)", id, len(id))
	}
	if other := NotebookID(long + "違う"); other == id {
		t.Errorf("long names that differ at the end gave the same ID %q", id)
	}
}

func TestUniqueNotebookID(t *testing.T) {
	taken := map[string]bool{"books": true, "books-2": true, Inbox: true}
	has := func(id string) bool { return taken[id] }
	for in, want := range map[string]string{"books": "books-3", "inbox": "inbox-2", "new": "new"} {
		if got := UniqueNotebookID(in, has); got != want {
			t.Errorf("UniqueNotebookID(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", maxIDLen)
	taken[long] = true
	if got := UniqueNotebookID(long, has); len(got) != maxIDLen || !strings.HasSuffix(got, "-2") || !ValidNotebook(got) {
		t.Errorf("UniqueNotebookID(long) = %q", got)
	}
}

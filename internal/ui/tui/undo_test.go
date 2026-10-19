package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestEditorUndo(t *testing.T) {
	e := newEditor(darkTheme, newCatalog("en"), &gennotes.Note{ID: "a", Notebook: "inbox", Title: "A", Body: "one\ntwo", Version: `"v"`}, "inbox")
	e.resize(60, 20)
	e.focus(fieldBody)
	typeText := func(s string) {
		for _, r := range s {
			if r == ' ' {
				e.update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
				continue
			}
			e.update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	ctrl := func(r rune) { e.update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}) }

	e.update(tea.KeyPressMsg{Code: tea.KeyDown})
	e.update(tea.KeyPressMsg{Code: tea.KeyEnd})
	typeText(" three four")
	if got := e.body.Value(); got != "one\ntwo three four" {
		t.Fatalf("typed: %q", got)
	}
	// A word typed in a row is undone at once, and the space before it apart.
	ctrl('z')
	if got := e.body.Value(); got != "one\ntwo three " {
		t.Fatalf("after one undo: %q", got)
	}
	ctrl('z')
	ctrl('z')
	if got := e.body.Value(); got != "one\ntwo " {
		t.Fatalf("after three undos: %q", got)
	}
	for range 10 {
		ctrl('z')
	}
	if got := e.body.Value(); got != "one\ntwo" {
		t.Fatalf("after undoing all: %q", got)
	}
	if e.body.Line() != 1 || e.body.Column() != 3 {
		t.Errorf("cursor after undo at %d:%d, want 1:3", e.body.Line(), e.body.Column())
	}
	ctrl('y')
	if got := e.body.Value(); got != "one\ntwo " {
		t.Fatalf("after redo: %q", got)
	}
	// A new change drops what was undone.
	typeText("x")
	ctrl('y')
	if got := e.body.Value(); got != "one\ntwo x" {
		t.Fatalf("redo after a new change: %q", got)
	}
	// A paste is undone on its own.
	e.updateOther(tea.PasteMsg{Content: "pasted"})
	ctrl('z')
	if got := e.body.Value(); got != "one\ntwo x" {
		t.Fatalf("after undoing a paste: %q", got)
	}
	// The cursor blinks between letters do not split a word.
	typeText("y")
	e.updateOther(struct{}{})
	typeText("z")
	ctrl('z')
	if got := e.body.Value(); got != "one\ntwo x" {
		t.Errorf("after undoing a word typed across a blink: %q", got)
	}
}

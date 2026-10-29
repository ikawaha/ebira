package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestCopyNote(t *testing.T) {
	n := &gennotes.Note{ID: "a", Title: "週末", Body: "- 自転車を直す\n![](a.assets/x.png)\n"}
	want := "# 週末\n\n- 自転車を直す\n![](a.assets/x.png)\n"
	if got := noteMarkdown(n); got != want {
		t.Errorf("noteMarkdown = %q, want %q", got, want)
	}
	if got := noteMarkdown(&gennotes.Note{Body: "本文"}); got != "本文" {
		t.Errorf("noteMarkdown without a title = %q", got)
	}

	saved := clipboardCommands
	t.Cleanup(func() { clipboardCommands = saved })

	// A clipboard command takes the Markdown on its standard input.
	out := filepath.Join(t.TempDir(), "clipboard")
	clipboardCommands = func() [][]string {
		return [][]string{{"no-such-clipboard-command"}, {"sh", "-c", "cat > " + out}}
	}
	if msg := copyNote(n)(); msg != (copiedMsg{status: "status.copied"}) {
		t.Errorf("copy with a command = %+v, want done", msg)
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != want {
		t.Errorf("clipboard = %q, %v", b, err)
	}

	// Without a command that works, the text goes to the terminal (OSC 52).
	clipboardCommands = func() [][]string { return [][]string{{"false"}} }
	if msg := copyNote(n)(); msg != (copiedMsg{text: want, status: "status.copied"}) {
		t.Errorf("copy without a command = %+v, want the text for OSC 52", msg)
	}

	// y in the list copies the note in the preview.
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	m.note = n
	m.focus = focusList
	if _, cmd := press(t, m, text("y")); cmd == nil {
		t.Error("y did not copy")
	}
	tm, cmd := m.Update(copiedMsg{text: want, status: "status.copied"})
	if cmd == nil || tm.(Model).status == "" {
		t.Errorf("OSC 52: cmd %v, status %q", cmd, tm.(Model).status)
	}
}

func TestCopyLink(t *testing.T) {
	const web = "http://localhost:13579/"
	n := &gennotes.Note{ID: "be4c1193735ee1", Title: `メモ [下書き] a\b`}
	if got, want := noteURL(web, n.ID), "http://localhost:13579/#note/be4c1193735ee1"; got != want {
		t.Errorf("noteURL = %q, want %q", got, want)
	}
	if got, want := noteLinkMarkdown(web, n), `[メモ \[下書き\] a\\b](http://localhost:13579/#note/be4c1193735ee1)`; got != want {
		t.Errorf("noteLinkMarkdown = %q, want %q", got, want)
	}
	if got, want := noteLinkMarkdown(web, &gennotes.Note{ID: "x1"}), "[x1](http://localhost:13579/#note/x1)"; got != want {
		t.Errorf("noteLinkMarkdown without a title = %q, want %q", got, want)
	}

	saved := clipboardCommands
	t.Cleanup(func() { clipboardCommands = saved })
	out := filepath.Join(t.TempDir(), "clipboard")
	clipboardCommands = func() [][]string { return [][]string{{"sh", "-c", "cat > " + out}} }

	// Y asks which link; m copies the Markdown link, and other keys nothing.
	m := New(context.Background(), &Clients{Web: web}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	m.note = n
	m.focus = focusList
	m, _ = press(t, m, text("Y"))
	if m.prompt != promptLink {
		t.Fatalf("Y: prompt = %v, want promptLink", m.prompt)
	}
	if _, cmd := press(t, m, text("x")); cmd != nil {
		t.Error("x at promptLink copied something")
	}
	m, cmd := press(t, m, text("m"))
	if m.prompt != promptNone || cmd == nil {
		t.Fatalf("m at promptLink: prompt %v, cmd %v", m.prompt, cmd)
	}
	if msg := cmd(); msg != (copiedMsg{status: "status.copiedLink"}) {
		t.Errorf("m copied %+v", msg)
	}
	if b, _ := os.ReadFile(out); string(b) != noteLinkMarkdown(web, n) {
		t.Errorf("clipboard = %q", b)
	}
}

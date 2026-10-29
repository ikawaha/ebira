package tui

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// Copying a note to the clipboard: with the clipboard command of the system
// when there is one, which works in every terminal, else with OSC 52, which
// asks the terminal to write its clipboard and works over ssh too, but only
// in the terminals that support it.

// copiedMsg reports copying a note, or a link to it. status is the key of the
// status to show (with "Terminal" after it when copied through the terminal);
// text is set when no command could copy it, for OSC 52.
type copiedMsg struct{ text, status string }

// clipboardCommands returns the commands that may write the clipboard here,
// in the order they are tried.
var clipboardCommands = func() [][]string {
	var cmds [][]string
	if runtime.GOOS == "darwin" {
		cmds = append(cmds, []string{"pbcopy"})
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmds = append(cmds, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		cmds = append(cmds, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return cmds
}

// noteMarkdown returns note n as Markdown to copy: its title as a heading,
// then its body. The paths of its images are left as they are.
func noteMarkdown(n *gennotes.Note) string {
	if n.Title == "" {
		return n.Body
	}
	return "# " + n.Title + "\n\n" + n.Body
}

// noteURL returns the URL of note id in the web UI at web, which opens the
// note there and which AI clients take to name the note.
func noteURL(web, id string) string {
	return web + "#note/" + url.PathEscape(id)
}

// noteLinkMarkdown returns a Markdown link to note n in the web UI at web,
// with its title, or its ID without one.
func noteLinkMarkdown(web string, n *gennotes.Note) string {
	title := n.Title
	if title == "" {
		title = n.ID
	}
	title = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(title)
	return "[" + title + "](" + noteURL(web, n.ID) + ")"
}

// copyNote copies note n to the clipboard as Markdown.
func copyNote(n *gennotes.Note) tea.Cmd {
	return copyText(noteMarkdown(n), "status.copied")
}

// copyLink copies a link to note n in the web UI at web: its URL, or with
// markdown, a Markdown link.
func copyLink(web string, n *gennotes.Note, markdown bool) tea.Cmd {
	if markdown {
		return copyText(noteLinkMarkdown(web, n), "status.copiedLink")
	}
	return copyText(noteURL(web, n.ID), "status.copiedURL")
}

// copyText copies text to the clipboard, and reports it with status.
func copyText(text, status string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range clipboardCommands() {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			cmd := exec.CommandContext(ctx, c[0], c[1:]...)
			cmd.Stdin = strings.NewReader(text)
			err := cmd.Run()
			cancel()
			if err == nil {
				return copiedMsg{status: status}
			}
		}
		return copiedMsg{text: text, status: status}
	}
}

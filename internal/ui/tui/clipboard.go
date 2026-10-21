package tui

import (
	"context"
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

// copiedMsg reports copying a note. text is set when no command could copy
// it, for OSC 52.
type copiedMsg struct{ text string }

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

// copyNote copies note n to the clipboard as Markdown.
func copyNote(n *gennotes.Note) tea.Cmd {
	text := noteMarkdown(n)
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
				return copiedMsg{}
			}
		}
		return copiedMsg{text: text}
	}
}

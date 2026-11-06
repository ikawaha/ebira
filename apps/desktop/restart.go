package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// markID is the ID of the mark the app puts in the page of an outdated server.
const markID = "ebira-app-restart"

// leaveRestart is leave, for starting the server again: the page reloads.
const leaveRestart = toApp + `.postMessage(` + leaves + ` ? "ebira:restart" : "ebira:stay");`

// outdated reports whether the server lacks something this app relies on.
// A newer one is fine: the page comes from it.
func (h *health) outdated() bool {
	return h.API < api || h.API == api && h.APIRev < apiRev
}

// markOutdated puts a mark beside the connection status of the page when its
// server is outdated. Pages of such servers cannot show it themselves, so the
// app adds it; a page without the status is left alone.
func (s *shell) markOutdated() {
	s.mu.Lock()
	addr, win := s.current.Addr, s.win
	s.mu.Unlock()
	h, err := readHealth(addr)
	if win == nil || err != nil || !h.outdated() {
		return
	}
	title, err := json.Marshal(s.text("The server is older than this app. Click to start it again with the version in the app."))
	if err != nil {
		panic(err) // a string always marshals
	}
	win.ExecJS(`(() => {
  const status = document.getElementById("status");
  if (!status || document.getElementById("` + markID + `")) return;
  const b = document.createElement("button");
  b.id = "` + markID + `";
  b.type = "button";
  b.textContent = "↻";
  b.title = ` + string(title) + `;
  b.setAttribute("aria-label", b.title);
  b.style.cssText = "border: none; background: none; padding: 0 2px; margin-left: 6px; color: var(--fg-muted); font-size: 15px; line-height: 1; cursor: pointer;";
  b.addEventListener("click", () => ` + toApp + `.postMessage('ebira:{"type":"restart"}'));
  status.after(b);
})();`)
}

// askRestart asks before starting the server again: it is shared with the
// command line, the TUI and the MCP clients, and its environment is lost.
func (s *shell) askRestart() {
	win := s.main()
	if win == nil {
		return
	}
	s.mu.Lock()
	addr := s.current.Addr
	s.mu.Unlock()
	dialog := s.app.Dialog.Question().
		SetTitle(s.text("Start the server again?")).
		SetMessage(fmt.Sprintf(s.text("The server at %s is older than this app, and some menus do nothing. It is started again with the version in the app. Its environment variables, such as the API keys of the assistant, are not carried over: a server started with them is better started again the same way."), addr))
	restart := dialog.AddButton(s.text("Start Again"))
	restart.OnClick(func() { win.ExecJS(leaveRestart) })
	cancel := dialog.AddButton(s.text("Cancel"))
	dialog.SetDefaultButton(restart).SetCancelButton(cancel).AttachToWindow(win).Show()
}

// restartServer starts the server again with the ebira of the bundle, which
// keeps the flags it was started with, and reloads the page.
func (s *shell) restartServer() {
	s.mu.Lock()
	t, win := s.current, s.win
	s.mu.Unlock()
	bin, err := helper()
	if err == nil {
		var out []byte
		if out, err = helperCommand(bin, "server", "restart", "-notes", t.Notes).CombinedOutput(); err != nil {
			err = fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
		}
	}
	if err != nil {
		log.Print("start the server again: ", err)
		s.app.Dialog.Error().
			SetTitle(s.text("Could not start the server again")).
			SetMessage(err.Error()).
			Show()
		return
	}
	if win != nil {
		win.SetURL(t.url())
	}
}

package main

import "fmt"

// showAbout shows the about panel: the version of the app from Info.plist, and
// the version of the server it shows, which may be of another build.
func (s *shell) showAbout() {
	s.mu.Lock()
	addr := s.current.Addr
	s.mu.Unlock()
	server := fmt.Sprintf(s.text("Server: not connected (%s)"), addr)
	if h, err := readHealth(addr); err == nil {
		version := h.VersionLabel
		if version == "" {
			version = "build " + h.Version // a server built before it told its version
		}
		server = fmt.Sprintf(s.text("Server: %s, API %d"), version, h.API) + "\n" + h.Addr + "\n" + h.NotesDir
	}
	s.showAboutPanel(fmt.Sprintf(s.text("App: %s"), appVersion()) + "\n" + server)
}

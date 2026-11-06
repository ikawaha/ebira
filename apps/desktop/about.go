package main

/*
#include <stdlib.h>
char *bundleString(const char *key);
void showAbout(const char *credits);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// appVersion is the version of ebira the app was built with, which build.sh
// writes to Info.plist: the version of the server in the bundle.
func appVersion() string {
	k := C.CString("EbiraVersion")
	defer C.free(unsafe.Pointer(k))
	v := C.bundleString(k)
	defer C.free(unsafe.Pointer(v))
	if s := C.GoString(v); s != "" {
		return s
	}
	return "devel"
}

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
	credits := C.CString(fmt.Sprintf(s.text("App: %s"), appVersion()) + "\n" + server)
	defer C.free(unsafe.Pointer(credits))
	C.showAbout(credits)
}

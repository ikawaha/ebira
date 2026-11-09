package main

import (
	"time"

	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/daemon"
)

// healthState is what GET /health tells of the server of st, which anyone
// reaching it may ask, without a session. With passkeys (mode), the flags it
// started with and when are left out: the desktop app, the commands and the
// web UI need only its process, address, notes directory, versions and
// instance, and the commands read the rest from server.json.
func healthState(st daemon.State, mode string) daemon.State {
	if mode == auth.ModePasskey {
		st.Flags, st.StartedAt = nil, time.Time{}
	}
	return st
}

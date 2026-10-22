package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ikawaha/ebira/internal/daemon"
)

// runVersion runs ebira version: the version of this ebira, without a server.
func runVersion(w io.Writer) error {
	bi, _ := debug.ReadBuildInfo()
	_, err := io.WriteString(w, versionText(bi, daemon.Version()))
	return err
}

// versionText tells the version of the module, such as v0.1.0 when installed
// with go install, or (devel) when built from a checkout; the commit it was
// built from, and whether the files had changes; the build, as ebira server
// status shows it for the server; and the Go toolchain.
func versionText(bi *debug.BuildInfo, build string) string {
	version, commit := "unknown", "unknown"
	goVersion := runtime.Version()
	if bi != nil {
		version, goVersion = bi.Main.Version, bi.GoVersion
		settings := map[string]string{}
		for _, s := range bi.Settings {
			settings[s.Key] = s.Value
		}
		if rev := settings["vcs.revision"]; rev != "" {
			commit = rev[:min(12, len(rev))]
			var notes []string
			if t, err := time.Parse(time.RFC3339, settings["vcs.time"]); err == nil {
				notes = append(notes, t.Local().Format(time.DateTime))
			}
			if settings["vcs.modified"] == "true" {
				notes = append(notes, "with changes not committed")
			}
			if len(notes) > 0 {
				commit += " (" + strings.Join(notes, ", ") + ")"
			}
		}
	}
	return fmt.Sprintf("version:   %s\ncommit:    %s\nbuild:     %s\ngo:        %s %s/%s\n",
		version, commit, build, goVersion, runtime.GOOS, runtime.GOARCH)
}

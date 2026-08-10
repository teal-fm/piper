package main

import (
	"os"
	"time"
)

// buildTime can be stamped at link time with
// -ldflags "-X main.buildTime=2006-01-02T15:04:05Z".
var buildTime string

// resolveBuildTime falls back to the binary's own mtime, which is what go
// build, air and Docker leave behind. Returns the zero time if neither is
// available, which the template renders as "N/A".
func resolveBuildTime() time.Time {
	if t, err := time.Parse(time.RFC3339, buildTime); err == nil {
		return t.UTC()
	}

	exe, err := os.Executable()
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(exe)
	if err != nil {
		return time.Time{}
	}

	return info.ModTime().UTC()
}

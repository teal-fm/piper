package main

import (
	"os"
	"time"
)

// buildTime is stamped at link time with -ldflags "-X main.buildTime=...".
var buildTime string

// resolveBuildTime falls back to the binary's mtime, or the zero time if
// neither is available.
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

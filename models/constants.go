package models

import "runtime/debug"

const releaseSubmissionAgent = "piper/v0.1.0"

// Stamped by Docker builds, which do not include the Git checkout.
// Release builds set buildChannel to "release" to use the version above.
var buildChannel string
var buildRevision string

var SubmissionAgent = submissionAgent(buildChannel, buildRevision, debug.ReadBuildInfo)

func submissionAgent(channel, revision string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if channel == "release" {
		return releaseSubmissionAgent
	}
	if revision == "" {
		if info, ok := readBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					revision = setting.Value
					break
				}
			}
		}
	}
	if revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		return "piper/main (" + revision + ")"
	}
	if channel == "main" {
		return "piper/main"
	}
	return releaseSubmissionAgent
}

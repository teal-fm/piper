package models

import (
	"runtime/debug"
	"testing"
)

func TestSubmissionAgent(t *testing.T) {
	for _, tt := range []struct {
		name, channel, revision, vcsRevision, want string
	}{
		{"source checkout", "", "", "abcdef0123456789", "piper/main (abcdef0)"},
		{"main image", "main", "123456789abcdef", "", "piper/main (1234567)"},
		{"explicit revision wins", "main", "123456789abcdef", "abcdef0123456789", "piper/main (1234567)"},
		{"release ignores revision", "release", "123456789abcdef", "abcdef0123456789", releaseSubmissionAgent},
		{"source archive", "", "", "", releaseSubmissionAgent},
		{"main without metadata", "main", "", "", "piper/main"},
		{"short revision", "main", "abc123", "", "piper/main (abc123)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			read := func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{Settings: []debug.BuildSetting{
					{Key: "vcs", Value: "git"},
					{Key: "vcs.revision", Value: tt.vcsRevision},
				}}, true
			}
			if got := submissionAgent(tt.channel, tt.revision, read); got != tt.want {
				t.Fatalf("submissionAgent() = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("build info unavailable", func(t *testing.T) {
		if got := submissionAgent("", "", func() (*debug.BuildInfo, bool) { return nil, false }); got != releaseSubmissionAgent {
			t.Fatalf("submissionAgent() = %q, want %q", got, releaseSubmissionAgent)
		}
	})
}

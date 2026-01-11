package applemusic

import (
	"strings"
	"testing"
)

// Helper to create AppleRecentTrack for testing
func makeTestTrack(name, album, artist string) *AppleRecentTrack {
	track := &AppleRecentTrack{}
	track.Attributes.Name = name
	track.Attributes.AlbumName = album
	track.Attributes.ArtistName = artist
	return track
}

func TestGenerateUploadHash(t *testing.T) {
	tests := []struct {
		name     string
		track    *AppleRecentTrack
		wantHash string
	}{
		{
			name:     "basic track",
			track:    makeTestTrack("Test Song", "Test Album", "Test Artist"),
			wantHash: "am_uploaded_ec50bb20ebeddc6f04cb65bcff156fed3c59d7e311e3d3efbbea506c3f8ad5ae",
		},
		{
			name:     "track with different data",
			track:    makeTestTrack("Collaboration", "Best Hits", "Artist One"),
			wantHash: "am_uploaded_c387e87af520fd4dae9b9cb872bd9cfbc8f7a88ca1b49d874dc1045183ba43c3",
		},
		{
			name:     "track with empty album",
			track:    makeTestTrack("Single Track", "", "Solo Artist"),
			wantHash: "am_uploaded_f73402f47a46c5ff050b64a86d5a1fcbaa20a734a8963f458dca81be290731f7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateUploadHash(tt.track)

			// The hash should be prefixed with "am_uploaded_" so that it's clear it's an uploaded Apple Music track
			if !strings.HasPrefix(got, "am_uploaded_") {
				t.Errorf("generateUploadHash() = %v, want prefix 'am_uploaded_'", got)
			}

			if got != tt.wantHash {
				t.Errorf("generateUploadHash() = %v, want %v", got, tt.wantHash)
			}

			// Hash is deterministic -- same track will return same hash
			got2 := generateUploadHash(tt.track)
			if got != got2 {
				t.Errorf("generateUploadHash() is not deterministic: first=%v, second=%v", got, got2)
			}
		})
	}
}

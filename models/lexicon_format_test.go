package models

import "testing"

func TestFormatMBIDURI(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want *string
	}{
		{name: "nil"},
		{name: "empty", in: strPtr(" "), want: nil},
		{name: "plain", in: strPtr("98255a8c-017a-4bc7-8dd6-1fa36124572b"), want: strPtr("mbid:98255a8c-017a-4bc7-8dd6-1fa36124572b")},
		{name: "already uri", in: strPtr("mbid:98255a8c-017a-4bc7-8dd6-1fa36124572b"), want: strPtr("mbid:98255a8c-017a-4bc7-8dd6-1fa36124572b")},
		{name: "trim", in: strPtr(" 98255a8c-017a-4bc7-8dd6-1fa36124572b "), want: strPtr("mbid:98255a8c-017a-4bc7-8dd6-1fa36124572b")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMBIDURI(tt.in)
			assertStringPtr(t, got, tt.want)
		})
	}
}

func TestFormatMusicServiceURI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want *string
	}{
		{name: "empty"},
		{name: "alias", in: "spotify", want: strPtr("https://spotify.com")},
		{name: "lastfm alias", in: "lastfm", want: strPtr("https://last.fm")},
		{name: "url", in: "https://open.spotify.com/track/test", want: strPtr("https://open.spotify.com")},
		{name: "domain with path", in: "music.apple.com/us/album/test", want: strPtr("https://music.apple.com")},
		{name: "domain", in: "ListenBrainz.org", want: strPtr("https://listenbrainz.org")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMusicServiceURI(tt.in)
			assertStringPtr(t, got, tt.want)
		})
	}
}

func assertStringPtr(t *testing.T, got, want *string) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("got %q, want %q", *got, *want)
	}
}

func strPtr(s string) *string {
	return &s
}

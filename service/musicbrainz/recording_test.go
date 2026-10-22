package musicbrainz

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teal-fm/piper/models"
)

type recordingTransport func(*http.Request) (*http.Response, error)

func (f recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRecordingMetadataLookupAndCache(t *testing.T) {
	s := NewMusicBrainzService(nil)
	calls := 0
	s.httpClient.Transport = recordingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/ws/2/recording/recording-id" || r.URL.Query().Get("inc") != "isrcs" {
			t.Fatalf("unexpected lookup: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"recording-id","length":234567,"isrcs":["USABC1234567"]}`)), Header: make(http.Header)}, nil
	})
	for range 2 {
		r, err := s.RecordingMetadata(context.Background(), "recording-id")
		if err != nil {
			t.Fatal(err)
		}
		if r.Length != 234567 || len(r.ISRCs) != 1 || r.ISRCs[0] != "USABC1234567" {
			t.Fatalf("unexpected metadata: %+v", r)
		}
	}
	if calls != 1 {
		t.Fatalf("made %d calls, want cached result", calls)
	}
}

func TestHydrateTrackRejectsUnusableArtistCredits(t *testing.T) {
	for _, credits := range []string{`[]`, `[{"name":"   ","artist":{"id":"artist"}}]`} {
		t.Run(credits, func(t *testing.T) {
			s := NewMusicBrainzService(nil)
			s.httpClient.Transport = recordingTransport(func(*http.Request) (*http.Response, error) {
				body := `{"recordings":[{"id":"recording","title":"Song","artist-credit":` + credits + `}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			source := models.Track{Name: "Song", Artist: []models.Artist{{Name: "Apple Artist"}}}
			track, err := HydrateTrack(s, source)
			if err == nil || track != nil {
				t.Fatalf("hydration = %+v, %v; want an error so callers retain source metadata", track, err)
			}
		})
	}
}

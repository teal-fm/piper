package musicbrainz

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
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

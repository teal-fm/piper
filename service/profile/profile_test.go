package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCachedReturnsExpiredAccountWithoutWaiting(t *testing.T) {
	resolver := &Resolver{
		cache: map[string]cacheEntry{
			"did:plc:test": {
				account:   Account{Handle: "listener.example"},
				expiresAt: time.Now().Add(-time.Minute),
			},
		},
		refreshing: map[string]bool{"did:plc:test": true},
	}

	start := time.Now()
	account, ok := resolver.Cached("did:plc:test")
	if !ok || account.Handle != "listener.example" {
		t.Fatalf("got %#v, %v", account, ok)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("cached lookup took %s", elapsed)
	}
}

func TestLatestRecordsReturnsNewestRecordPerService(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/com.atproto.repo.listRecords" || r.URL.Query().Has("reverse") || r.URL.Query().Get("limit") != "100" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"records":[
			{"uri":"at://did:plc:listener/fm.teal.feed.play/3wrong","value":{"musicServiceUri":"https://last.fm","trackName":"Older","playedTime":"2026-08-26T22:36:19Z"}},
			{"uri":"at://did:plc:listener/fm.teal.feed.play/3lastfm","value":{"musicServiceUri":"https://last.fm","trackName":"Latest Last.fm","playedTime":"2026-08-27T22:36:19Z"}},
			{"uri":"at://did:plc:listener/fm.teal.feed.play/3spotify","value":{"musicServiceUri":"https://open.spotify.com","trackName":"Latest Spotify","playedTime":"2026-08-27T21:00:00Z"}},
			{"uri":"at://did:plc:listener/fm.teal.feed.play/3apple","value":{"musicServiceUri":"https://music.apple.com","trackName":"Latest Apple","playedTime":"2026-08-27T20:00:00Z"}}
		]}`))
	}))
	defer server.Close()
	resolver := &Resolver{
		client: server.Client(),
		cache: map[string]cacheEntry{
			"did:plc:listener": {
				account:   Account{PDS: strings.TrimPrefix(server.URL, "https://")},
				expiresAt: time.Now().Add(time.Minute),
			},
		},
		refreshing: make(map[string]bool),
	}
	records, err := resolver.LatestRecords(context.Background(), "did:plc:listener", map[string]RecordTarget{
		"lastfm":     {TrackName: "Latest Last.fm", PlayedAt: time.Date(2026, 8, 27, 22, 36, 19, 0, time.UTC)},
		"spotify":    {TrackName: "Latest Spotify", PlayedAt: time.Date(2026, 8, 27, 21, 0, 0, 0, time.UTC)},
		"applemusic": {TrackName: "Latest Apple", PlayedAt: time.Date(2026, 8, 27, 20, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("LatestRecords: %v", err)
	}
	if records["lastfm"] != "at://did:plc:listener/fm.teal.feed.play/3lastfm" ||
		records["spotify"] != "at://did:plc:listener/fm.teal.feed.play/3spotify" ||
		records["applemusic"] != "at://did:plc:listener/fm.teal.feed.play/3apple" {
		t.Fatalf("got %#v", records)
	}
}

func TestValidateHTTPSURL(t *testing.T) {
	for _, rawURL := range []string{"http://pds.example", "https://user@pds.example", "https://"} {
		target, _ := url.Parse(rawURL)
		if err := validateHTTPSURL(target); err == nil {
			t.Errorf("validateHTTPSURL(%q) succeeded", rawURL)
		}
	}
	target, _ := url.Parse("https://pds.example")
	if err := validateHTTPSURL(target); err != nil {
		t.Fatalf("valid HTTPS URL rejected: %v", err)
	}
}

func TestSafePublicAddress(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"169.254.169.254":      false,
		"192.0.2.10":           false,
		"::1":                  false,
		"fc00::1":              false,
		"2001:db8::1":          false,
		"1.1.1.1":              true,
		"2606:4700:4700::1111": true,
	}
	for rawAddress, want := range tests {
		if got := safePublicAddress(netip.MustParseAddr(rawAddress)); got != want {
			t.Errorf("safePublicAddress(%s) = %v, want %v", rawAddress, got, want)
		}
	}
}

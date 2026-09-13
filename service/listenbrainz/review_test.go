package listenbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func TestRejectsPlaintextAndRedirects(t *testing.T) {
	hits := 0
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer plain.Close()
	s := NewService(nil, plain.URL, "test", nil, nil, nil)
	if _, err := s.ValidateToken(context.Background(), "secret"); err == nil {
		t.Fatal("accepted plaintext API")
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plain.URL, http.StatusFound) }))
	defer tls.Close()
	s = testService(nil, tls, nil)
	if _, err := s.ValidateToken(context.Background(), "secret"); err == nil {
		t.Fatal("accepted redirect")
	}
	if hits != 0 {
		t.Fatalf("sent %d plaintext requests", hits)
	}
}

func TestBacklogPaginationAndDistinctSameSecondListens(t *testing.T) {
	for _, failPage := range []bool{false, true} {
		t.Run(strconv.FormatBool(failPage), func(t *testing.T) {
			database := testDatabase(t)
			user := linkedUser(t, database, "listener", "secret")
			watermark := time.Unix(100, 0).UTC()
			user.ListenBrainzSyncedAt = &watermark
			if err := database.SaveListenBrainzSyncTimestamp(user.ID, watermark); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var response listensResponse
				add := func(ts int64, artist string) {
					response.Payload.Listens = append(response.Payload.Listens, models.ListenBrainzPayload{ListenedAt: &ts, TrackMetadata: models.ListenBrainzTrackMetadata{TrackName: "Same title", ArtistName: artist}})
				}
				if r.URL.Query().Get("max_ts") == "" {
					for ts := int64(1100); ts > 100; ts-- {
						add(ts, "Artist A")
					}
				} else {
					if r.URL.Query().Get("max_ts") != "102" || r.URL.Query().Get("min_ts") != "" {
						t.Errorf("wrong pagination query: %s", r.URL)
					}
					if failPage {
						http.Error(w, "failed page", 500)
						return
					}
					add(101, "Artist A")
					add(101, "Artist B")
					add(100, "Artist A")
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			s := testService(database, server, nil)
			err := s.syncListens(context.Background(), user)
			stored, readErr := database.GetUserByID(user.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failPage {
				if err == nil || !stored.ListenBrainzSyncedAt.Equal(watermark) {
					t.Fatalf("failed page advanced watermark: %v, %v", err, stored.ListenBrainzSyncedAt)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tracks, err := database.GetRecentTracks(user.ID, 2000)
			if err != nil {
				t.Fatal(err)
			}
			if len(tracks) != 1002 {
				t.Fatalf("stored %d listens, want 1002", len(tracks))
			}
			if stored.ListenBrainzSyncedAt.Unix() != 1100 {
				t.Fatalf("wrong watermark: %v", stored.ListenBrainzSyncedAt)
			}
		})
	}
}

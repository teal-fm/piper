package playingnow

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

type testStatusClient struct {
	client *client.APIClient
}

func (p testStatusClient) GetATProtoClient(_ string, _ string, _ context.Context) (*client.APIClient, error) {
	return p.client, nil
}

func TestClearPlayingNowOnlyWritesOriginalStatus(t *testing.T) {
	for _, tc := range []struct {
		name, storedCID string
		rejectSwap      bool
		wantWrites      int32
	}{
		{"original status", "original", false, 1},
		{"newer status", "newer", false, 0},
		{"status changes during clear", "original", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			did := syntax.DID("did:plc:status-test")
			const uri = "at://did:plc:status-test/fm.teal.actor.status/self"
			database, err := db.New(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.Initialize(); err != nil {
				t.Fatal(err)
			}
			user, err := database.FindOrCreateUserByDID(did.String())
			if err != nil {
				t.Fatal(err)
			}
			if err := database.SetLatestATProtoSessionId(did.String(), "fixture"); err != nil {
				t.Fatal(err)
			}

			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/xrpc/com.atproto.repo.getRecord":
					cid := tc.storedCID
					if tc.rejectSwap && writes.Load() > 0 {
						cid = "newer"
					}
					json.NewEncoder(w).Encode(map[string]string{"uri": uri, "cid": cid})
				case "/xrpc/com.atproto.repo.putRecord":
					writes.Add(1)
					var body struct {
						SwapRecord string `json:"swapRecord"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SwapRecord != "original" {
						t.Errorf("putRecord swapped %q, want the original CID: %v", body.SwapRecord, err)
					}
					if tc.rejectSwap {
						w.WriteHeader(http.StatusBadRequest)
						json.NewEncoder(w).Encode(map[string]string{"error": "InvalidSwap"})
						return
					}
					json.NewEncoder(w).Encode(map[string]string{"uri": uri, "cid": "expired"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			pds := client.NewAPIClient(server.URL)
			pds.AccountDID = &did
			service := NewPlayingNowService(database, nil, nil)
			service.atprotoService = testStatusClient{pds}
			err = service.ClearPlayingNowIfCurrent(t.Context(), user.ID, "original")
			if tc.rejectSwap {
				if err == nil || !strings.Contains(err.Error(), "InvalidSwap") {
					t.Fatalf("first clear = %v, want InvalidSwap", err)
				}
				err = service.ClearPlayingNowIfCurrent(t.Context(), user.ID, "original")
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := writes.Load(); got != tc.wantWrites {
				t.Fatalf("putRecord calls = %d, want %d", got, tc.wantWrites)
			}
		})
	}
}

func TestPlayingNowSkipsUserWithoutATProtoSession(t *testing.T) {
	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	defer database.Close()

	if err := database.Initialize(); err != nil {
		t.Fatalf("initialize database: %v", err)
	}

	did := "did:plc:test"
	userID, err := database.CreateUser(&models.User{ATProtoDID: &did})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &Service{
		db:            database,
		logger:        log.New(io.Discard, "", 0),
		clearedStatus: make(map[int64]bool),
	}

	track := &models.Track{Name: "Test Track", Artist: []models.Artist{{Name: "Test Artist"}}}
	if err := service.PublishPlayingNow(context.Background(), userID, track); err != nil {
		t.Fatalf("PublishPlayingNow returned an error: %v", err)
	}
	if err := service.ClearPlayingNow(context.Background(), userID); err != nil {
		t.Fatalf("ClearPlayingNow returned an error: %v", err)
	}
}

func TestTrackToPlayView(t *testing.T) {
	// Create a mock playing now service (we'll test the conversion logic)
	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer database.Close()

	if err := database.Initialize(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	// Mock ATProto service (we'll just test the conversion, not the actual submission)
	service := &Service{
		db:     database,
		logger: nil, // We'll skip logging in tests
	}

	// Create a test track
	track := &models.Track{
		Name: "Test Track",
		Artist: []models.Artist{
			{
				Name: "Test Artist",
				MBID: func() *string { s := "test-artist-mbid"; return &s }(),
			},
		},
		Album:          "Test Album",
		DurationMs:     240000, // 4 minutes
		Timestamp:      time.Now(),
		ServiceBaseUrl: "spotify",
		URL:            "https://open.spotify.com/track/test",
		RecordingMBID:  func() *string { s := "test-recording-mbid"; return &s }(),
		ReleaseMBID:    func() *string { s := "test-release-mbid"; return &s }(),
		ISRC:           "TEST1234567",
	}

	// Test the conversion
	playView, err := service.trackToPlayView(track)
	if err != nil {
		t.Fatalf("Failed to convert track to PlayView: %v", err)
	}

	// Verify the conversion
	if playView.TrackName != "Test Track" {
		t.Errorf("Expected track name 'Test Track', got %s", playView.TrackName)
	}

	if len(playView.Artists) != 1 {
		t.Errorf("Expected 1 artist, got %d", len(playView.Artists))
	} else {
		if playView.Artists[0].ArtistName != "Test Artist" {
			t.Errorf("Expected artist name 'Test Artist', got %s", playView.Artists[0].ArtistName)
		}
		if playView.Artists[0].ArtistMbId == nil || *playView.Artists[0].ArtistMbId != "mbid:test-artist-mbid" {
			t.Errorf("Artist MBID not set correctly")
		}
	}

	if playView.ReleaseName == nil || *playView.ReleaseName != "Test Album" {
		t.Errorf("Release name not set correctly")
	}

	if playView.Duration == nil || *playView.Duration != 240 {
		t.Errorf("Expected duration 240 seconds, got %v", playView.Duration)
	}

	if playView.RecordingMbId == nil || *playView.RecordingMbId != "mbid:test-recording-mbid" {
		t.Errorf("Recording MBID not set correctly")
	}

	if playView.ReleaseMbId == nil || *playView.ReleaseMbId != "mbid:test-release-mbid" {
		t.Errorf("Release MBID not set correctly")
	}

	if playView.Isrc == nil || *playView.Isrc != "TEST1234567" {
		t.Errorf("ISRC not set correctly")
	}

	if playView.OriginUri == nil || *playView.OriginUri != "https://open.spotify.com/track/test" {
		t.Errorf("Origin URL not set correctly")
	}

	if playView.MusicServiceUri == nil || *playView.MusicServiceUri != "https://spotify.com" {
		t.Errorf("Music service not set correctly")
	}
}

func TestTrackToPlayViewEmptyTrack(t *testing.T) {
	service := &Service{}

	// Test with empty track name (should fail)
	track := &models.Track{
		Name:   "", // Empty name should cause error
		Artist: []models.Artist{{Name: "Test Artist"}},
	}

	_, err := service.trackToPlayView(track)
	if err == nil {
		t.Error("Expected error for empty track name, got nil")
	}
}

func TestTrackToPlayViewMinimal(t *testing.T) {
	service := &Service{}

	// Test with minimal track data
	track := &models.Track{
		Name:   "Minimal Track",
		Artist: []models.Artist{{Name: "Minimal Artist"}},
	}

	playView, err := service.trackToPlayView(track)
	if err != nil {
		t.Fatalf("Failed to convert minimal track: %v", err)
	}

	if playView.TrackName != "Minimal Track" {
		t.Errorf("Expected track name 'Minimal Track', got %s", playView.TrackName)
	}

	if len(playView.Artists) != 1 || playView.Artists[0].ArtistName != "Minimal Artist" {
		t.Errorf("Artist not set correctly")
	}

	// Optional fields should be nil for minimal track
	if playView.Duration != nil {
		t.Errorf("Expected duration to be nil for minimal track")
	}

	if playView.ReleaseName != nil {
		t.Errorf("Expected release name to be nil for minimal track")
	}
}

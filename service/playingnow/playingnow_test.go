package playingnow

import (
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

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
		if playView.Artists[0].ArtistMbId == nil || *playView.Artists[0].ArtistMbId != "test-artist-mbid" {
			t.Errorf("Artist MBID not set correctly")
		}
	}

	if playView.ReleaseName == nil || *playView.ReleaseName != "Test Album" {
		t.Errorf("Release name not set correctly")
	}

	if playView.Duration == nil || *playView.Duration != 240 {
		t.Errorf("Expected duration 240 seconds, got %v", playView.Duration)
	}

	if playView.RecordingMbId == nil || *playView.RecordingMbId != "test-recording-mbid" {
		t.Errorf("Recording MBID not set correctly")
	}

	if playView.ReleaseMbId == nil || *playView.ReleaseMbId != "test-release-mbid" {
		t.Errorf("Release MBID not set correctly")
	}

	if playView.Isrc == nil || *playView.Isrc != "TEST1234567" {
		t.Errorf("ISRC not set correctly")
	}

	if playView.OriginUrl == nil || *playView.OriginUrl != "https://open.spotify.com/track/test" {
		t.Errorf("Origin URL not set correctly")
	}

	if playView.MusicServiceBaseDomain == nil || *playView.MusicServiceBaseDomain != "spotify" {
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

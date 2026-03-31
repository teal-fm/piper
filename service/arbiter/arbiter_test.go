package arbiter

import (
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

type stubProvider struct {
	liveTrack   *models.Track
	live        bool
	lastStamped *models.Track
}

func (s stubProvider) GetLiveTrack(userID int64) (*models.Track, bool) {
	return s.liveTrack, s.live
}

func (s stubProvider) GetLastStampedTrack(userID int64) *models.Track {
	return s.lastStamped
}

func setupArbiterDB(t *testing.T) (*db.DB, int64) {
	t.Helper()

	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatalf("failed to initialize db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return database, userID
}

func testTrack(serviceBaseURL, name, artist, album, isrc string, at time.Time) *models.Track {
	return &models.Track{
		Name:           name,
		Artist:         []models.Artist{{Name: artist}},
		Album:          album,
		URL:            serviceBaseURL + "/" + name,
		ServiceBaseUrl: serviceBaseURL,
		ISRC:           isrc,
		Timestamp:      at,
	}
}

func TestResolveCurrentTrackUsesUserPriority(t *testing.T) {
	database, userID := setupArbiterDB(t)
	if err := database.UpdateUserServicePriority(userID, "applemusic,spotify"); err != nil {
		t.Fatalf("failed to update priority: %v", err)
	}

	now := time.Now().UTC()
	spotifyTrack := testTrack("open.spotify.com", "Shared Song", "Artist", "Album", "ISRC1", now)
	appleTrack := testTrack("music.apple.com", "Shared Song", "Artist", "Album", "ISRC1", now)

	coordinator := New(database)
	coordinator.SetSpotify(stubProvider{liveTrack: spotifyTrack, live: true})
	coordinator.SetAppleMusic(stubProvider{liveTrack: appleTrack, live: true})

	track, service, err := coordinator.ResolveCurrentTrack(userID)
	if err != nil {
		t.Fatalf("ResolveCurrentTrack returned error: %v", err)
	}
	if service != models.ServiceAppleMusic {
		t.Fatalf("expected Apple Music to win, got %s", service)
	}
	if track == nil || track.ServiceBaseUrl != "music.apple.com" {
		t.Fatalf("expected Apple Music track, got %#v", track)
	}
}

func TestCanScrobbleBlocksLowerPriorityWhenHigherPriorityLive(t *testing.T) {
	database, userID := setupArbiterDB(t)
	now := time.Now().UTC()

	coordinator := New(database)
	coordinator.SetSpotify(stubProvider{liveTrack: testTrack("open.spotify.com", "Song", "Artist", "Album", "ISRC1", now), live: true})
	coordinator.SetAppleMusic(stubProvider{})

	allowed, err := coordinator.CanScrobble(userID, models.ServiceAppleMusic, testTrack("music.apple.com", "Other Song", "Artist", "Album", "ISRC2", now))
	if err != nil {
		t.Fatalf("CanScrobble returned error: %v", err)
	}
	if allowed {
		t.Fatal("expected Apple Music scrobble to be blocked while higher-priority Spotify is live")
	}
}

func TestCanScrobbleBlocksDuplicateAgainstHigherPriorityStamp(t *testing.T) {
	database, userID := setupArbiterDB(t)
	if err := database.UpdateUserServicePriority(userID, "spotify,applemusic"); err != nil {
		t.Fatalf("failed to update priority: %v", err)
	}

	now := time.Now().UTC()
	spotifyStamped := testTrack("open.spotify.com", "Song", "Artist", "Album", "ISRC1", now)
	appleCandidate := testTrack("music.apple.com", "Song", "Artist", "Album", "ISRC1", now.Add(30*time.Second))

	coordinator := New(database)
	coordinator.SetSpotify(stubProvider{lastStamped: spotifyStamped})
	coordinator.SetAppleMusic(stubProvider{})

	allowed, err := coordinator.CanScrobble(userID, models.ServiceAppleMusic, appleCandidate)
	if err != nil {
		t.Fatalf("CanScrobble returned error: %v", err)
	}
	if allowed {
		t.Fatal("expected duplicate Apple Music scrobble to be blocked")
	}
}

func TestCanPublishNowPlayingOnlyAllowsResolvedOwner(t *testing.T) {
	database, userID := setupArbiterDB(t)
	now := time.Now().UTC()
	spotifyTrack := testTrack("open.spotify.com", "Song", "Artist", "Album", "ISRC1", now)
	appleTrack := testTrack("music.apple.com", "Song", "Artist", "Album", "ISRC1", now)

	coordinator := New(database)
	coordinator.SetSpotify(stubProvider{liveTrack: spotifyTrack, live: true})
	coordinator.SetAppleMusic(stubProvider{liveTrack: appleTrack, live: true})

	allowed, err := coordinator.CanPublishNowPlaying(userID, models.ServiceAppleMusic, appleTrack)
	if err != nil {
		t.Fatalf("CanPublishNowPlaying returned error: %v", err)
	}
	if allowed {
		t.Fatal("expected lower-priority Apple Music publish to be blocked")
	}

	allowed, err = coordinator.CanPublishNowPlaying(userID, models.ServiceSpotify, spotifyTrack)
	if err != nil {
		t.Fatalf("CanPublishNowPlaying returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected Spotify publish to be allowed")
	}
}

func TestCanClearNowPlayingOnlyAllowsOwnerOrNoTrack(t *testing.T) {
	database, userID := setupArbiterDB(t)
	now := time.Now().UTC()

	coordinator := New(database)
	coordinator.SetSpotify(stubProvider{liveTrack: testTrack("open.spotify.com", "Song", "Artist", "Album", "ISRC1", now), live: true})
	coordinator.SetAppleMusic(stubProvider{})

	allowed, err := coordinator.CanClearNowPlaying(userID, models.ServiceAppleMusic)
	if err != nil {
		t.Fatalf("CanClearNowPlaying returned error: %v", err)
	}
	if allowed {
		t.Fatal("expected Apple Music clear to be blocked while Spotify owns live state")
	}

	allowed, err = coordinator.CanClearNowPlaying(userID, models.ServiceSpotify)
	if err != nil {
		t.Fatalf("CanClearNowPlaying returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected Spotify clear to be allowed for current owner")
	}
}

func TestTracksEquivalentFallsBackToMetadataAndWindow(t *testing.T) {
	now := time.Now().UTC()
	left := testTrack("music.apple.com", " Song ", " Artist ", " Album ", "", now)
	right := testTrack("open.spotify.com", "song", "artist", "album", "", now.Add(90*time.Second))

	if !TracksEquivalent(left, right, 2*time.Minute) {
		t.Fatal("expected tracks to be treated as equivalent by normalized metadata")
	}

	if TracksEquivalent(left, right, 30*time.Second) {
		t.Fatal("expected tracks outside the dedupe window to be different")
	}
}

func TestResolveCurrentTrackSupportsLastFMInPriorityOrder(t *testing.T) {
	database, userID := setupArbiterDB(t)
	if err := database.UpdateUserServicePriority(userID, "lastfm,spotify,applemusic"); err != nil {
		t.Fatalf("failed to update priority: %v", err)
	}

	now := time.Now().UTC()
	lastFMTrack := testTrack("lastfm", "Live Song", "Artist", "Album", "", now)
	spotifyTrack := testTrack("open.spotify.com", "Live Song", "Artist", "Album", "", now)

	coordinator := New(database)
	coordinator.SetLastFM(stubProvider{liveTrack: lastFMTrack, live: true})
	coordinator.SetSpotify(stubProvider{liveTrack: spotifyTrack, live: true})

	track, service, err := coordinator.ResolveCurrentTrack(userID)
	if err != nil {
		t.Fatalf("ResolveCurrentTrack returned error: %v", err)
	}
	if service != models.ServiceLastFM {
		t.Fatalf("expected Last.fm to win, got %s", service)
	}
	if track == nil || track.ServiceBaseUrl != "lastfm" {
		t.Fatalf("expected Last.fm track, got %#v", track)
	}
}

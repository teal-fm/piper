package lastfm

import (
	"context"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/arbiter"
)

type recordingPlayingNowService struct {
	publishCount int
	clearCount   int
}

func (r *recordingPlayingNowService) PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error {
	r.publishCount++
	return nil
}

func (r *recordingPlayingNowService) ClearPlayingNow(ctx context.Context, userID int64) error {
	r.clearCount++
	return nil
}

type staticProvider struct {
	liveTrack   *models.Track
	live        bool
	lastStamped *models.Track
}

func (s staticProvider) GetLiveTrack(userID int64) (*models.Track, bool) {
	return s.liveTrack, s.live
}

func (s staticProvider) GetLastStampedTrack(userID int64) *models.Track {
	return s.lastStamped
}

func setupLastFMTestDB(t *testing.T) (*db.DB, *models.User) {
	t.Helper()

	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatalf("failed to initialize db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	username := "lfm-user"
	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := database.AddLastFMUsername(userID, username); err != nil {
		t.Fatalf("failed to add lastfm username: %v", err)
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	return database, user
}

func newLastFMTestService(database *db.DB, playingNow *recordingPlayingNowService) *Service {
	return &Service{
		db:                 database,
		httpClient:         nil,
		limiter:            nil,
		apiKey:             "test-key",
		Usernames:          nil,
		musicBrainzService: nil,
		atprotoService:     nil,
		playingNowService:  playingNow,
		lastSeenNowPlaying: make(map[string]Track),
		liveTracks:         make(map[int64]*models.Track),
		lastStamped:        make(map[int64]*models.Track),
		mu:                 sync.Mutex{},
		logger:             log.New(io.Discard, "", 0),
	}
}

func TestProcessTracksBlocksLastFMPublishWhenHigherPriorityServiceOwnsLiveState(t *testing.T) {
	database, user := setupLastFMTestDB(t)
	playingNow := &recordingPlayingNowService{}
	service := newLastFMTestService(database, playingNow)

	coordinator := arbiter.New(database)
	coordinator.SetSpotify(staticProvider{
		liveTrack: &models.Track{
			Name:           "Spotify Live",
			Artist:         []models.Artist{{Name: "Artist"}},
			Album:          "Album",
			ServiceBaseUrl: "open.spotify.com",
			Timestamp:      time.Now().UTC(),
		},
		live: true,
	})
	service.SetCoordinator(coordinator)

	tracks := []Track{{
		Name:  "LastFM Live",
		Album: Album{Text: "Album"},
		Artist: Artist{
			Text: "Artist",
		},
		Attr: &struct {
			NowPlaying string `json:"nowplaying"`
		}{NowPlaying: "true"},
	}}

	if err := service.processTracks(context.Background(), *user.LastFMUsername, tracks); err != nil {
		t.Fatalf("processTracks returned error: %v", err)
	}
	if playingNow.publishCount != 0 {
		t.Fatalf("expected publish to be blocked, got %d publishes", playingNow.publishCount)
	}
	if track, ok := service.GetLiveTrack(user.ID); !ok || track == nil {
		t.Fatal("expected Last.fm live track to be tracked locally")
	}
}

func TestProcessTracksBlocksLastFMClearWhenHigherPriorityServiceOwnsLiveState(t *testing.T) {
	database, user := setupLastFMTestDB(t)
	playingNow := &recordingPlayingNowService{}
	service := newLastFMTestService(database, playingNow)
	service.liveTracks[user.ID] = &models.Track{Name: "Old Live", Artist: []models.Artist{{Name: "Artist"}}}

	coordinator := arbiter.New(database)
	coordinator.SetSpotify(staticProvider{
		liveTrack: &models.Track{
			Name:           "Spotify Live",
			Artist:         []models.Artist{{Name: "Artist"}},
			Album:          "Album",
			ServiceBaseUrl: "open.spotify.com",
			Timestamp:      time.Now().UTC(),
		},
		live: true,
	})
	service.SetCoordinator(coordinator)

	if err := service.processTracks(context.Background(), *user.LastFMUsername, nil); err != nil {
		t.Fatalf("processTracks returned error: %v", err)
	}
	if playingNow.clearCount != 0 {
		t.Fatalf("expected clear to be blocked, got %d clears", playingNow.clearCount)
	}
}

func TestProcessTracksBlocksLastFMScrobbleWhenHigherPriorityServiceOwnsSession(t *testing.T) {
	database, user := setupLastFMTestDB(t)
	playingNow := &recordingPlayingNowService{}
	service := newLastFMTestService(database, playingNow)

	coordinator := arbiter.New(database)
	coordinator.SetSpotify(staticProvider{
		liveTrack: &models.Track{
			Name:           "Spotify Live",
			Artist:         []models.Artist{{Name: "Artist"}},
			Album:          "Album",
			ServiceBaseUrl: "open.spotify.com",
			Timestamp:      time.Now().UTC(),
		},
		live: true,
	})
	service.SetCoordinator(coordinator)

	tracks := []Track{{
		Name:  "LastFM Track",
		URL:   "https://last.fm/track/1",
		Album: Album{Text: "Album"},
		Artist: Artist{
			Text: "Artist",
		},
		Date: &TrackDate{Time: time.Now().UTC()},
	}}

	if err := service.processTracks(context.Background(), *user.LastFMUsername, tracks); err != nil {
		t.Fatalf("processTracks returned error: %v", err)
	}

	savedTracks, err := database.GetRecentTracks(user.ID, 10)
	if err != nil {
		t.Fatalf("failed to query saved tracks: %v", err)
	}
	if len(savedTracks) != 0 {
		t.Fatalf("expected scrobble save to be blocked, got %d tracks", len(savedTracks))
	}
}

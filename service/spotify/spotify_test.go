package spotify

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/session"
)

// ===== Mock Implementations =====

// publishCall records a call to PublishPlayingNow
type publishCall struct {
	userID int64
	track  *models.Track
}

// mockPlayingNowService implements the playingNowService interface for testing
type mockPlayingNowService struct {
	publishCalls []publishCall
	clearCalls   []int64
	publishErr   error
	clearErr     error
}

func (m *mockPlayingNowService) PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error {
	m.publishCalls = append(m.publishCalls, publishCall{userID: userID, track: track})
	return m.publishErr
}

func (m *mockPlayingNowService) ClearPlayingNow(ctx context.Context, userID int64) error {
	m.clearCalls = append(m.clearCalls, userID)
	return m.clearErr
}

// ===== Test Helpers =====

func setupTestDB(t *testing.T) *db.DB {
	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	if err := database.Initialize(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	return database
}

func createTestUser(t *testing.T, database *db.DB) int64 {
	user := &models.User{
		Email: func() *string { s := "test@example.com"; return &s }(),
	}
	userID, err := database.CreateUser(user)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	return userID
}

func createTestTrack(name, artistName, url string, durationMs, progressMs int64) *models.Track {
	return &models.Track{
		Name:           name,
		Artist:         []models.Artist{{Name: artistName, ID: "artist123"}},
		Album:          "Test Album",
		URL:            url,
		DurationMs:     durationMs,
		ProgressMs:     progressMs,
		ServiceBaseUrl: "open.spotify.com",
		ISRC:           "TEST1234567",
		Timestamp:      time.Now().UTC(),
	}
}

func newTestService(database *db.DB, playingNow *mockPlayingNowService) *Service {
	return &Service{
		DB:                 database,
		atprotoAuthService: nil,
		mb:                 nil,
		playingNowService:  playingNow,
		userPlayStates:     make(map[int64]*userPlayState),
		userTokens:         make(map[int64]string),
		logger:             log.New(io.Discard, "", 0),
	}
}

func withUserContext(ctx context.Context, userID int64) context.Context {
	return session.WithUserID(ctx, userID)
}

// ===== getFirstArtist Tests =====

func TestGetFirstArtist(t *testing.T) {
	testCases := []struct {
		name     string
		track    *models.Track
		expected string
	}{
		{
			name:     "nil track",
			track:    nil,
			expected: "Unknown Artist",
		},
		{
			name: "empty artists",
			track: &models.Track{
				Name:   "Test Track",
				Artist: []models.Artist{},
			},
			expected: "Unknown Artist",
		},
		{
			name: "one artist",
			track: &models.Track{
				Name:   "Test Track",
				Artist: []models.Artist{{Name: "Daft Punk", ID: "123"}},
			},
			expected: "Daft Punk",
		},
		{
			name: "multiple artists",
			track: &models.Track{
				Name: "Test Track",
				Artist: []models.Artist{
					{Name: "Artist A", ID: "1"},
					{Name: "Artist B", ID: "2"},
				},
			},
			expected: "Artist A",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getFirstArtist(tc.track)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

// ===== computeStateUpdate Tests =====

func TestComputeStateUpdate_NoPriorState(t *testing.T) {
	t.Run("track playing, no prior state", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)
		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}

		action := svc.computeStateUpdate(userID, resp)

		// Should publish now playing
		if !action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be true")
		}
		if action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be false")
		}

		// State should be created
		state := svc.userPlayStates[userID]
		if state == nil {
			t.Fatal("Expected state to be created")
		}
		if state.isPaused {
			t.Error("Expected isPaused to be false")
		}
		// accumulatedMs should be min(progressMs, maxSkipDeltaMs)
		if state.accumulatedMs != 5000 {
			t.Errorf("Expected accumulatedMs to be 5000, got %d", state.accumulatedMs)
		}
	})

	t.Run("track playing with high progress, capped at maxSkipDeltaMs", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		// Progress is 60s, should be capped at 30s
		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 60000)
		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}

		action := svc.computeStateUpdate(userID, resp)

		if !action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be true")
		}

		state := svc.userPlayStates[userID]
		if state.accumulatedMs != maxSkipDeltaMs {
			t.Errorf("Expected accumulatedMs to be capped at %d, got %d", maxSkipDeltaMs, state.accumulatedMs)
		}
	})

	t.Run("track paused, no prior state", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)
		resp := &SpotifyTrackResponse{Track: track, IsPlaying: false}

		action := svc.computeStateUpdate(userID, resp)

		if !action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be true")
		}
		if action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be false")
		}

		state := svc.userPlayStates[userID]
		if state == nil {
			t.Fatal("Expected state to be created")
		}
		if !state.isPaused {
			t.Error("Expected isPaused to be true")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		action := svc.computeStateUpdate(userID, nil)

		// Should be a no-op
		if action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be false for nil response with no prior state")
		}
		if action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be false")
		}
		if action.stampTrack {
			t.Error("Expected stampTrack to be false")
		}
	})

	t.Run("nil track in response", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		resp := &SpotifyTrackResponse{Track: nil, IsPlaying: true}
		action := svc.computeStateUpdate(userID, resp)

		// Should be a no-op
		if action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be false for nil track with no prior state")
		}
		if action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be false")
		}
		if action.stampTrack {
			t.Error("Expected stampTrack to be false")
		}
	})
}

func TestComputeStateUpdate_SameTrackContinues(t *testing.T) {
	t.Run("same track still playing, accumulates time", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)

		// Set up existing state
		pastTime := time.Now().Add(-10 * time.Second) // 10 seconds ago
		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 5000,
			lastPollTime:  pastTime,
			hasStamped:    false,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		action := svc.computeStateUpdate(userID, resp)

		// Should not publish (same track continuing)
		if action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be false for same track continuing")
		}

		state := svc.userPlayStates[userID]
		// Should have added ~10s to accumulated (within tolerance)
		if state.accumulatedMs != 15000 {
			t.Errorf("Expected accumulatedMs to be %d, got %d", 15000, state.accumulatedMs)
		}
	})

	t.Run("same track now paused", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)

		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 60000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: false}
		action := svc.computeStateUpdate(userID, resp)

		if !action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be true")
		}

		state := svc.userPlayStates[userID]
		if !state.isPaused {
			t.Error("Expected isPaused to be true")
		}
	})

	t.Run("same track resumed from pause", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)

		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 60000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      true, // Was paused
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		action := svc.computeStateUpdate(userID, resp)

		if !action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be true when resuming")
		}

		state := svc.userPlayStates[userID]
		if state.isPaused {
			t.Error("Expected isPaused to be false after resume")
		}
	})

	t.Run("delta time capped at maxDeltaMs", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)

		// Set up state with lastPollTime 60 seconds ago
		pastTime := time.Now().Add(-60 * time.Second)
		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 10000,
			lastPollTime:  pastTime,
			hasStamped:    false,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		svc.computeStateUpdate(userID, resp)

		state := svc.userPlayStates[userID]
		// Should be capped: 10000 + 30000 = 40000 (not 10000 + 60000)
		if state.accumulatedMs != 10000+maxDeltaMs { // small tolerance
			t.Errorf("Expected delta to be capped at maxDeltaMs, got accumulatedMs=%d", state.accumulatedMs)
		}
	})
}

func TestComputeStateUpdate_NewTrackDetected(t *testing.T) {
	t.Run("different track URL", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		oldTrack := createTestTrack("Old Song", "Old Artist", "http://spotify/track1", 240000, 120000)
		newTrack := createTestTrack("New Song", "New Artist", "http://spotify/track2", 180000, 5000)

		svc.userPlayStates[userID] = &userPlayState{
			track:         oldTrack,
			accumulatedMs: 120000,
			lastPollTime:  time.Now(),
			hasStamped:    true,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: newTrack, IsPlaying: true}
		action := svc.computeStateUpdate(userID, resp)

		if !action.publishNowPlaying {
			t.Error("Expected publishNowPlaying to be true for new track")
		}

		state := svc.userPlayStates[userID]
		if state.track.URL != newTrack.URL {
			t.Error("Expected state to have new track")
		}
		if state.hasStamped {
			t.Error("Expected hasStamped to be reset to false")
		}
		if state.accumulatedMs != 5000 {
			t.Errorf("Expected accumulatedMs to be reset to progressMs (5000), got %d", state.accumulatedMs)
		}
	})
}

func TestComputeStateUpdate_SongRepeat(t *testing.T) {
	t.Run("loop detected when accumulated >= duration", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 180000, 5000)

		// Set accumulated to just under duration
		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 175000,
			lastPollTime:  time.Now().Add(-10 * time.Second),
			hasStamped:    true,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		svc.computeStateUpdate(userID, resp)

		state := svc.userPlayStates[userID]
		// After adding ~10s, accumulated should be ~185000, exceeding duration of 180000
		// So it should subtract duration: 185000 - 180000 = 5000
		if state.accumulatedMs != 5000 {
			t.Errorf("Expected accumulatedMs to be reset below duration, got %d", state.accumulatedMs)
		}
		if state.hasStamped {
			t.Error("Expected hasStamped to be reset to false after loop")
		}
	})

	t.Run("overflow preserved after loop", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 100000, 5000)

		// Set accumulated to duration + 5000
		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 105000,
			lastPollTime:  time.Now(), // recent, so delta is small
			hasStamped:    true,
			isPaused:      false,
		}

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		svc.computeStateUpdate(userID, resp)

		state := svc.userPlayStates[userID]
		// Should have subtracted duration: 105000 - 100000 = 5000
		if state.accumulatedMs != 5000 {
			t.Errorf("Expected accumulatedMs to be 5000 after loop, got %d", state.accumulatedMs)
		}
	})
}

func TestComputeStateUpdate_StampThreshold(t *testing.T) {
	testCases := []struct {
		name          string
		durationMs    int64
		accumulatedMs int64
		hasStamped    bool
		expectStamp   bool
	}{
		{
			name:          "half duration on long track",
			durationMs:    240000, // 4 min
			accumulatedMs: 121000, // just over 2 min
			hasStamped:    false,
			expectStamp:   true,
		},
		{
			name:          "30s threshold on medium track",
			durationMs:    50000, // 50 sec track, threshold = max(25s, 30s) = 30s
			accumulatedMs: 31000, // over 30s
			hasStamped:    false,
			expectStamp:   true,
		},
		{
			name:          "below threshold",
			durationMs:    240000,
			accumulatedMs: 50000, // threshold is 120000
			hasStamped:    false,
			expectStamp:   false,
		},
		{
			name:          "already stamped",
			durationMs:    240000,
			accumulatedMs: 150000,
			hasStamped:    true,
			expectStamp:   false,
		},
		{
			name:          "exactly at threshold should not stamp",
			durationMs:    240000,
			accumulatedMs: 120000, // exactly at threshold, needs to be > threshold
			hasStamped:    false,
			expectStamp:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			database := setupTestDB(t)
			defer database.Close()

			svc := newTestService(database, nil)
			userID := int64(1)

			track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", tc.durationMs, 5000)

			svc.userPlayStates[userID] = &userPlayState{
				track:         track,
				accumulatedMs: tc.accumulatedMs,
				lastPollTime:  time.Now(), // recent, so minimal delta added
				hasStamped:    tc.hasStamped,
				isPaused:      false,
			}

			resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
			action := svc.computeStateUpdate(userID, resp)

			if action.stampTrack != tc.expectStamp {
				t.Errorf("Expected stampTrack=%v, got %v", tc.expectStamp, action.stampTrack)
			}

			if tc.expectStamp {
				state := svc.userPlayStates[userID]
				if !state.hasStamped {
					t.Error("Expected hasStamped to be true after stamping")
				}
			}
		})
	}
}

func TestComputeStateUpdate_EdgeCases(t *testing.T) {
	t.Run("zero duration track", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 0, 0)

		resp := &SpotifyTrackResponse{Track: track, IsPlaying: true}
		action := svc.computeStateUpdate(userID, resp)

		// Should not panic, threshold should be max(0, 30000) = 30000
		if action.stampTrack {
			t.Error("Should not stamp with 0 accumulated time")
		}
	})

	t.Run("nil response with existing state clears now playing", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := int64(1)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 5000)
		svc.userPlayStates[userID] = &userPlayState{
			track:         track,
			accumulatedMs: 60000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		action := svc.computeStateUpdate(userID, nil)

		if !action.clearNowPlaying {
			t.Error("Expected clearNowPlaying to be true when response is nil with existing state")
		}

		state := svc.userPlayStates[userID]
		if !state.isPaused {
			t.Error("Expected isPaused to be true")
		}
	})
}

// ===== HTTP Handler Tests =====

func TestHandleCurrentTrack(t *testing.T) {
	t.Run("no auth returns unauthorized", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)

		req := httptest.NewRequest(http.MethodGet, "/current", nil)
		rr := httptest.NewRecorder()

		svc.HandleCurrentTrack(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("no state returns no track playing", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := createTestUser(t, database)

		req := httptest.NewRequest(http.MethodGet, "/current", nil)
		ctx := withUserContext(req.Context(), userID)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		svc.HandleCurrentTrack(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}
		if rr.Body.String() != "No track currently playing" {
			t.Errorf("Expected 'No track currently playing', got '%s'", rr.Body.String())
		}
	})

	t.Run("nil track in state returns no track playing", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := createTestUser(t, database)

		svc.userPlayStates[userID] = &userPlayState{
			track: nil,
		}

		req := httptest.NewRequest(http.MethodGet, "/current", nil)
		ctx := withUserContext(req.Context(), userID)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		svc.HandleCurrentTrack(rr, req)

		if rr.Body.String() != "No track currently playing" {
			t.Errorf("Expected 'No track currently playing', got '%s'", rr.Body.String())
		}
	})

	t.Run("success returns track JSON", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := createTestUser(t, database)

		track := createTestTrack("Test Song", "Test Artist", "http://spotify/track1", 240000, 60000)
		svc.userPlayStates[userID] = &userPlayState{
			track: track,
		}

		req := httptest.NewRequest(http.MethodGet, "/current", nil)
		ctx := withUserContext(req.Context(), userID)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		svc.HandleCurrentTrack(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}

		contentType := rr.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}

		var returnedTrack models.Track
		if err := json.Unmarshal(rr.Body.Bytes(), &returnedTrack); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if returnedTrack.Name != "Test Song" {
			t.Errorf("Expected track name 'Test Song', got '%s'", returnedTrack.Name)
		}
	})
}

func TestHandleTrackHistory(t *testing.T) {
	t.Run("no auth returns unauthorized", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)

		req := httptest.NewRequest(http.MethodGet, "/history", nil)
		rr := httptest.NewRecorder()

		svc.HandleTrackHistory(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("empty history returns empty array", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := createTestUser(t, database)

		req := httptest.NewRequest(http.MethodGet, "/history", nil)
		ctx := withUserContext(req.Context(), userID)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		svc.HandleTrackHistory(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}

		var tracks []*models.Track
		if err := json.Unmarshal(rr.Body.Bytes(), &tracks); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if len(tracks) != 0 {
			t.Errorf("Expected empty array, got %d tracks", len(tracks))
		}
	})

	t.Run("success returns tracks", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userID := createTestUser(t, database)

		// Save some tracks to the database
		track1 := createTestTrack("Track 1", "Artist 1", "http://spotify/track1", 180000, 0)
		track2 := createTestTrack("Track 2", "Artist 2", "http://spotify/track2", 200000, 0)

		if _, err := database.SaveTrack(userID, track1); err != nil {
			t.Fatalf("Failed to save track1: %v", err)
		}
		if _, err := database.SaveTrack(userID, track2); err != nil {
			t.Fatalf("Failed to save track2: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/history", nil)
		ctx := withUserContext(req.Context(), userID)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		svc.HandleTrackHistory(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}

		contentType := rr.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}

		var tracks []*models.Track
		if err := json.Unmarshal(rr.Body.Bytes(), &tracks); err != nil {
			t.Fatalf("Failed to parse response JSON: %v", err)
		}

		if len(tracks) != 2 {
			t.Errorf("Expected 2 tracks, got %d", len(tracks))
		}
	})
}

// ===== stampTrack Tests =====

func TestStampTrack(t *testing.T) {
	t.Run("saves track to database with HasStamped true", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		// createTestUser does not assign a DID to the user.
		// This prevents a PDS submission from occurring.
		userID := createTestUser(t, database)

		track := createTestTrack("Stamp Test", "Test Artist", "http://spotify/track1", 240000, 0)

		svc.stampTrack(context.Background(), userID, track)

		// Verify track was saved
		tracks, err := database.GetRecentTracks(userID, 10)
		if err != nil {
			t.Fatalf("Failed to get recent tracks: %v", err)
		}

		if len(tracks) != 1 {
			t.Fatalf("Expected 1 track, got %d", len(tracks))
		}

		if tracks[0].Name != "Stamp Test" {
			t.Errorf("Expected track name 'Stamp Test', got '%s'", tracks[0].Name)
		}

		if !tracks[0].HasStamped {
			t.Error("Expected HasStamped to be true")
		}
	})

	t.Run("without MusicBrainz service saves original track", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		svc.mb = nil // Explicitly nil, already should be but just in case
		userID := createTestUser(t, database)

		track := createTestTrack("No MB Test", "Test Artist", "http://spotify/track1", 240000, 0)

		svc.stampTrack(context.Background(), userID, track)

		tracks, err := database.GetRecentTracks(userID, 10)
		if err != nil {
			t.Fatalf("Failed to get recent tracks: %v", err)
		}

		if len(tracks) != 1 {
			t.Fatalf("Expected 1 track, got %d", len(tracks))
		}

		// Track should be saved even without MB service
		if tracks[0].Name != "No MB Test" {
			t.Errorf("Expected track name 'No MB Test', got '%s'", tracks[0].Name)
		}
	})
}

// ===== Multi-User Tests =====

func TestComputeStateUpdate_MultipleUsersIsolation(t *testing.T) {
	t.Run("two users with different tracks playing simultaneously", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 5000)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 10000)

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		// Both users start playing
		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// Both should publish now playing
		if !actionA.publishNowPlaying {
			t.Error("Expected User A to publishNowPlaying")
		}
		if !actionB.publishNowPlaying {
			t.Error("Expected User B to publishNowPlaying")
		}

		// Verify states are independent
		stateA := svc.userPlayStates[userA]
		stateB := svc.userPlayStates[userB]

		if stateA.track.URL != trackA.URL {
			t.Errorf("User A has wrong track: expected %s, got %s", trackA.URL, stateA.track.URL)
		}
		if stateB.track.URL != trackB.URL {
			t.Errorf("User B has wrong track: expected %s, got %s", trackB.URL, stateB.track.URL)
		}
		if stateA.accumulatedMs != 5000 {
			t.Errorf("User A accumulatedMs: expected 5000, got %d", stateA.accumulatedMs)
		}
		if stateB.accumulatedMs != 10000 {
			t.Errorf("User B accumulatedMs: expected 10000, got %d", stateB.accumulatedMs)
		}
	})

	t.Run("one user's track change doesn't reset another user's accumulated time", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 0)

		// Set up existing states
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 100000, // 100 seconds accumulated
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 50000, // 50 seconds accumulated
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		// User A changes track
		newTrackA := createTestTrack("New Song A", "Artist A", "http://spotify/trackA2", 200000, 5000)
		respA := &SpotifyTrackResponse{Track: newTrackA, IsPlaying: true}
		svc.computeStateUpdate(userA, respA)

		// User A should have reset state
		stateA := svc.userPlayStates[userA]
		if stateA.accumulatedMs != 5000 {
			t.Errorf("User A should have reset accumulatedMs to 5000, got %d", stateA.accumulatedMs)
		}

		// User B should be unchanged
		stateB := svc.userPlayStates[userB]
		if stateB.accumulatedMs != 50000 {
			t.Errorf("User B accumulatedMs should remain 50000, got %d", stateB.accumulatedMs)
		}
		if stateB.track.URL != trackB.URL {
			t.Errorf("User B track should be unchanged")
		}
	})

	t.Run("one user's stamp doesn't affect another user's stamp status", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 0)

		// User A is above stamp threshold, User B is below
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 125000, // Above threshold (120000 for 4 min track)
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 50000, // Below threshold (90000 for 3 min track)
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// User A should stamp
		if !actionA.stampTrack {
			t.Error("Expected User A to stamp")
		}
		if !svc.userPlayStates[userA].hasStamped {
			t.Error("User A hasStamped should be true")
		}

		// User B should NOT stamp
		if actionB.stampTrack {
			t.Error("User B should NOT stamp")
		}
		if svc.userPlayStates[userB].hasStamped {
			t.Error("User B hasStamped should remain false")
		}
	})
}

func TestComputeStateUpdate_MultipleUsersDifferentStates(t *testing.T) {
	t.Run("user A playing, user B paused", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 5000)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 30000)

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: false} // paused

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// User A should publish now playing
		if !actionA.publishNowPlaying {
			t.Error("User A should publishNowPlaying")
		}
		if actionA.clearNowPlaying {
			t.Error("User A should NOT clearNowPlaying")
		}

		// User B should clear now playing (paused)
		if !actionB.clearNowPlaying {
			t.Error("User B should clearNowPlaying")
		}
		if actionB.publishNowPlaying {
			t.Error("User B should NOT publishNowPlaying")
		}

		// Verify states
		stateA := svc.userPlayStates[userA]
		stateB := svc.userPlayStates[userB]

		if stateA.isPaused {
			t.Error("User A should NOT be paused")
		}
		if !stateB.isPaused {
			t.Error("User B should be paused")
		}
	})

	t.Run("user A pauses while user B continues playing", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 0)

		// Both users are playing
		pastTime := time.Now().Add(-10 * time.Second)
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 60000,
			lastPollTime:  pastTime,
			hasStamped:    false,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 40000,
			lastPollTime:  pastTime,
			hasStamped:    false,
			isPaused:      false,
		}

		// User A pauses, User B continues
		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: false}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// User A should clear
		if !actionA.clearNowPlaying {
			t.Error("User A should clearNowPlaying")
		}

		// User B should NOT clear and should NOT publish (same track continuing)
		if actionB.clearNowPlaying {
			t.Error("User B should NOT clearNowPlaying")
		}
		if actionB.publishNowPlaying {
			t.Error("User B should NOT publishNowPlaying (same track continuing)")
		}

		// User A should be paused, User B should not
		if !svc.userPlayStates[userA].isPaused {
			t.Error("User A should be paused")
		}
		if svc.userPlayStates[userB].isPaused {
			t.Error("User B should NOT be paused")
		}

		// User B should have accumulated more time (~10s)
		stateB := svc.userPlayStates[userB]
		if stateB.accumulatedMs != 50000 {
			t.Errorf("User B accumulatedMs should be ~50000, got %d", stateB.accumulatedMs)
		}
	})

	t.Run("user A resumes while user B is already playing", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 180000, 0)

		// User A is paused, User B is playing
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 60000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      true,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 40000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		// User A resumes, User B continues
		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// User A should publish (resuming from pause)
		if !actionA.publishNowPlaying {
			t.Error("User A should publishNowPlaying on resume")
		}

		// User B should NOT publish (same track continuing)
		if actionB.publishNowPlaying {
			t.Error("User B should NOT publishNowPlaying (same track continuing)")
		}

		// Both should not be paused
		if svc.userPlayStates[userA].isPaused {
			t.Error("User A should NOT be paused after resume")
		}
		if svc.userPlayStates[userB].isPaused {
			t.Error("User B should NOT be paused")
		}
	})
}

func TestComputeStateUpdate_MultipleUsersStampThreshold(t *testing.T) {
	t.Run("user A reaches stamp threshold, user B doesn't", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		// Both have same duration track (threshold = 120000)
		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 240000, 0)

		// User A is past threshold, User B is not
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 125000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 60000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		if !actionA.stampTrack {
			t.Error("User A should stamp")
		}
		if actionB.stampTrack {
			t.Error("User B should NOT stamp")
		}
	})

	t.Run("both users reach threshold at different accumulated times", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		// Different duration tracks, different thresholds
		trackA := createTestTrack("Song A", "Artist A", "http://spotify/trackA", 240000, 0) // threshold = 120000
		trackB := createTestTrack("Song B", "Artist B", "http://spotify/trackB", 50000, 0)  // threshold = 30000 (max(25000, 30000))

		// Both are past their respective thresholds
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 125000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 35000,
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		actionA := svc.computeStateUpdate(userA, respA)
		actionB := svc.computeStateUpdate(userB, respB)

		// Both should stamp
		if !actionA.stampTrack {
			t.Error("User A should stamp")
		}
		if !actionB.stampTrack {
			t.Error("User B should stamp")
		}

		// Both should have hasStamped = true
		if !svc.userPlayStates[userA].hasStamped {
			t.Error("User A hasStamped should be true")
		}
		if !svc.userPlayStates[userB].hasStamped {
			t.Error("User B hasStamped should be true")
		}
	})

	t.Run("one user loops track while another continues - independent loop detection", func(t *testing.T) {
		database := setupTestDB(t)
		defer database.Close()

		svc := newTestService(database, nil)
		userA := int64(1)
		userB := int64(2)

		// User A has a short track that will loop
		trackA := createTestTrack("Short Song", "Artist A", "http://spotify/trackA", 100000, 0)
		trackB := createTestTrack("Long Song", "Artist B", "http://spotify/trackB", 300000, 0)

		// User A is past their track duration (will trigger loop)
		// User B is still in the middle of their track
		svc.userPlayStates[userA] = &userPlayState{
			track:         trackA,
			accumulatedMs: 105000, // Past 100000 duration
			lastPollTime:  time.Now(),
			hasStamped:    true,
			isPaused:      false,
		}
		svc.userPlayStates[userB] = &userPlayState{
			track:         trackB,
			accumulatedMs: 100000, // Still less than 300000 duration
			lastPollTime:  time.Now(),
			hasStamped:    false,
			isPaused:      false,
		}

		respA := &SpotifyTrackResponse{Track: trackA, IsPlaying: true}
		respB := &SpotifyTrackResponse{Track: trackB, IsPlaying: true}

		svc.computeStateUpdate(userA, respA)
		svc.computeStateUpdate(userB, respB)

		stateA := svc.userPlayStates[userA]
		stateB := svc.userPlayStates[userB]

		// User A should have looped: accumulatedMs reduced, hasStamped reset
		if stateA.accumulatedMs >= trackA.DurationMs {
			t.Errorf("User A should have looped, accumulatedMs=%d should be < %d", stateA.accumulatedMs, trackA.DurationMs)
		}
		if stateA.hasStamped {
			t.Error("User A hasStamped should be reset to false after loop")
		}

		// User B should NOT have looped
		if stateB.accumulatedMs < 100000 {
			t.Errorf("User B should NOT have looped, accumulatedMs=%d", stateB.accumulatedMs)
		}
		// User B should still not be stamped (threshold is 150000 for 300000ms track)
		if stateB.hasStamped {
			t.Error("User B hasStamped should still be false (not reached threshold yet)")
		}
	})
}

// ===== generateLocalHash Tests =====

func TestGenerateLocalHash(t *testing.T) {
	t.Run("deterministic output", func(t *testing.T) {
		track := createTestTrack("My Song", "My Artist", "", 240000, 0)
		hash1 := generateLocalHash(track)
		hash2 := generateLocalHash(track)
		if hash1 != hash2 {
			t.Errorf("Expected deterministic output, got %s and %s", hash1, hash2)
		}
	})

	t.Run("has correct prefix", func(t *testing.T) {
		track := createTestTrack("My Song", "My Artist", "", 240000, 0)
		hash := generateLocalHash(track)
		if !strings.HasPrefix(hash, "sp_local_") {
			t.Errorf("Expected hash to start with 'sp_local_', got '%s'", hash)
		}
	})

	t.Run("different tracks produce different hashes", func(t *testing.T) {
		trackA := createTestTrack("Song A", "Artist A", "", 240000, 0)
		trackB := createTestTrack("Song B", "Artist B", "", 240000, 0)
		hashA := generateLocalHash(trackA)
		hashB := generateLocalHash(trackB)
		if hashA == hashB {
			t.Errorf("Expected different hashes for different tracks, both got %s", hashA)
		}
	})

	t.Run("uses Unknown Artist for empty artist list", func(t *testing.T) {
		trackWithArtist := &models.Track{
			Name:  "Local File",
			Album: "My Album",
			// Hardcoding the other song to Unknown Artist to show that they result in the same hash.
			// This would (probably) never happen, but just for testing sake
			Artist: []models.Artist{{Name: "Unknown Artist", ID: ""}},
		}
		trackNoArtist := &models.Track{
			Name:   "Local File",
			Album:  "My Album",
			Artist: []models.Artist{},
		}
		hashWith := generateLocalHash(trackWithArtist)
		hashWithout := generateLocalHash(trackNoArtist)
		if hashWith != hashWithout {
			t.Errorf("Expected same hash when artist is 'Unknown Artist' vs empty list, got %s and %s", hashWith, hashWithout)
		}
	})

	t.Run("album name affects hash", func(t *testing.T) {
		trackA := createTestTrack("Same Song", "Same Artist", "", 240000, 0)
		trackB := createTestTrack("Same Song", "Same Artist", "", 240000, 0)
		trackB.Album = "Different Album"
		hashA := generateLocalHash(trackA)
		hashB := generateLocalHash(trackB)
		if hashA == hashB {
			t.Errorf("Expected different hashes for different albums, both got %s", hashA)
		}
	})
}

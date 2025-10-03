package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/session"
)

func setupTestDB(t *testing.T) *db.DB {
	// Use in-memory SQLite database for testing
	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	if err := database.Initialize(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	return database
}

func createTestUser(t *testing.T, database *db.DB) (int64, string) {
	// Create a test user
	user := &models.User{
		Email:      func() *string { s := "test@example.com"; return &s }(),
		ATProtoDID: func() *string { s := "did:test:user"; return &s }(),
	}

	userID, err := database.CreateUser(user)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create API key for the user
	sessionManager := session.NewSessionManager(database)
	apiKeyObj, err := sessionManager.CreateAPIKey(userID, "test-key", 30) // 30 days validity
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	return userID, apiKeyObj.ID
}

// Helper to create context with user ID (simulating auth middleware)
func withUserContext(ctx context.Context, userID int64) context.Context {
	return session.WithUserID(ctx, userID)
}

func TestListenBrainzSubmission_Success(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	// Create test submission
	submission := models.ListenBrainzSubmission{
		ListenType: "single",
		Payload: []models.ListenBrainzPayload{
			{
				ListenedAt: func() *int64 { i := int64(1704067200); return &i }(),
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName:  "Daft Punk",
					TrackName:   "One More Time",
					ReleaseName: func() *string { s := "Discovery"; return &s }(),
					AdditionalInfo: &models.ListenBrainzAdditionalInfo{
						RecordingMBID: func() *string { s := "98255a8c-017a-4bc7-8dd6-1fa36124572b"; return &s }(),
						ArtistMBIDs:   []string{"db92a151-1ac2-438b-bc43-b82e149ddd50"},
						ReleaseMBID:   func() *string { s := "bf9e91ea-8029-4a04-a26a-224e00a83266"; return &s }(),
						DurationMs:    func() *int64 { i := int64(320000); return &i }(),
						SpotifyID:     func() *string { s := "4PTG3Z6ehGkBFwjybzWkR8"; return &s }(),
						ISRC:          func() *string { s := "GBARL0600925"; return &s }(),
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	// Add user context (simulating authentication middleware)
	ctx := withUserContext(req.Context(), userID)
	req = req.WithContext(ctx)

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call handler
	handler := apiSubmitListensHandler(database, nil, nil, nil)
	handler(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response
	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}

	processed, ok := response["processed"].(float64)
	if !ok || processed != 1 {
		t.Errorf("Expected processed count 1, got %v", response["processed"])
	}

	// Verify data was saved to database
	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("Failed to get tracks from database: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("Expected 1 track in database, got %d", len(tracks))
	}

	track := tracks[0]
	if track.Name != "One More Time" {
		t.Errorf("Expected track name 'One More Time', got %s", track.Name)
	}
	if len(track.Artist) == 0 || track.Artist[0].Name != "Daft Punk" {
		t.Errorf("Expected artist 'Daft Punk', got %v", track.Artist)
	}
	if track.Album != "Discovery" {
		t.Errorf("Expected album 'Discovery', got %s", track.Album)
	}
	if track.RecordingMBID == nil || *track.RecordingMBID != "98255a8c-017a-4bc7-8dd6-1fa36124572b" {
		t.Errorf("Expected recording MBID to be set correctly")
	}
	if track.DurationMs != 320000 {
		t.Errorf("Expected duration 320000ms, got %d", track.DurationMs)
	}
}

func TestListenBrainzSubmission_MinimalPayload(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	// Create minimal submission (only required fields)
	submission := models.ListenBrainzSubmission{
		ListenType: "single",
		Payload: []models.ListenBrainzPayload{
			{
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "The Beatles",
					TrackName:  "Hey Jude",
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	ctx := withUserContext(req.Context(), userID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler := apiSubmitListensHandler(database, nil, nil, nil)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Verify track was saved
	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("Failed to get tracks from database: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("Expected 1 track in database, got %d", len(tracks))
	}

	track := tracks[0]
	if track.Name != "Hey Jude" {
		t.Errorf("Expected track name 'Hey Jude', got %s", track.Name)
	}
	if len(track.Artist) == 0 || track.Artist[0].Name != "The Beatles" {
		t.Errorf("Expected artist 'The Beatles', got %v", track.Artist)
	}
	// Timestamp should be set to current time if not provided
	if track.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}
}

func TestListenBrainzSubmission_BulkImport(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	// Create bulk submission
	submission := models.ListenBrainzSubmission{
		ListenType: "import",
		Payload: []models.ListenBrainzPayload{
			{
				ListenedAt: func() *int64 { i := int64(1704067200); return &i }(),
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Track One Artist",
					TrackName:  "Track One",
				},
			},
			{
				ListenedAt: func() *int64 { i := int64(1704067300); return &i }(),
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Track Two Artist",
					TrackName:  "Track Two",
				},
			},
			{
				ListenedAt: func() *int64 { i := int64(1704067400); return &i }(),
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Track Three Artist",
					TrackName:  "Track Three",
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	ctx := withUserContext(req.Context(), userID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler := apiSubmitListensHandler(database, nil, nil, nil)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	processed, ok := response["processed"].(float64)
	if !ok || processed != 3 {
		t.Errorf("Expected processed count 3, got %v", response["processed"])
	}

	// Verify all tracks were saved
	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("Failed to get tracks from database: %v", err)
	}

	if len(tracks) != 3 {
		t.Fatalf("Expected 3 tracks in database, got %d", len(tracks))
	}
}

func TestListenBrainzSubmission_PlayingNow(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	// Create playing_now submission
	submission := models.ListenBrainzSubmission{
		ListenType: "playing_now",
		Payload: []models.ListenBrainzPayload{
			{
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Current Artist",
					TrackName:  "Currently Playing",
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	ctx := withUserContext(req.Context(), userID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler := apiSubmitListensHandler(database, nil, nil, nil)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// playing_now tracks should not be permanently stored
	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("Failed to get tracks from database: %v", err)
	}

	if len(tracks) != 0 {
		t.Errorf("Expected 0 tracks in database for playing_now, got %d", len(tracks))
	}
}

func TestListenBrainzSubmission_ValidationErrors(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	testCases := []struct {
		name           string
		submission     models.ListenBrainzSubmission
		expectedStatus int
		expectedError  string
	}{
		{
			name: "invalid_listen_type",
			submission: models.ListenBrainzSubmission{
				ListenType: "invalid",
				Payload:    []models.ListenBrainzPayload{},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid listen_type",
		},
		{
			name: "empty_payload",
			submission: models.ListenBrainzSubmission{
				ListenType: "single",
				Payload:    []models.ListenBrainzPayload{},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Payload cannot be empty",
		},
		{
			name: "missing_artist_name",
			submission: models.ListenBrainzSubmission{
				ListenType: "single",
				Payload: []models.ListenBrainzPayload{
					{
						TrackMetadata: models.ListenBrainzTrackMetadata{
							TrackName: "Track Without Artist",
						},
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "artist_name is required",
		},
		{
			name: "missing_track_name",
			submission: models.ListenBrainzSubmission{
				ListenType: "single",
				Payload: []models.ListenBrainzPayload{
					{
						TrackMetadata: models.ListenBrainzTrackMetadata{
							ArtistName: "Artist Without Track",
						},
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "track_name is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tc.submission)
			if err != nil {
				t.Fatalf("Failed to marshal submission: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Token "+apiKey)

			ctx := withUserContext(req.Context(), userID)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler := apiSubmitListensHandler(database, nil, nil, nil)
			handler(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tc.expectedStatus, rr.Code, rr.Body.String())
			}

			if tc.expectedError != "" {
				body := rr.Body.String()
				if !bytes.Contains([]byte(body), []byte(tc.expectedError)) {
					t.Errorf("Expected error containing '%s', got: %s", tc.expectedError, body)
				}
			}
		})
	}
}

func TestListenBrainzSubmission_Unauthorized(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	submission := models.ListenBrainzSubmission{
		ListenType: "single",
		Payload: []models.ListenBrainzPayload{
			{
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Test Artist",
					TrackName:  "Test Track",
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header

	rr := httptest.NewRecorder()
	handler := apiSubmitListensHandler(database, nil, nil, nil)
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestListenBrainzDataConversion(t *testing.T) {
	// Test the conversion logic directly
	payload := models.ListenBrainzPayload{
		ListenedAt: func() *int64 { i := int64(1704067200); return &i }(),
		TrackMetadata: models.ListenBrainzTrackMetadata{
			ArtistName:  "Test Artist",
			TrackName:   "Test Track",
			ReleaseName: func() *string { s := "Test Album"; return &s }(),
			AdditionalInfo: &models.ListenBrainzAdditionalInfo{
				RecordingMBID: func() *string { s := "test-recording-mbid"; return &s }(),
				ArtistMBIDs:   []string{"test-artist-mbid-1", "test-artist-mbid-2"},
				ReleaseMBID:   func() *string { s := "test-release-mbid"; return &s }(),
				DurationMs:    func() *int64 { i := int64(240000); return &i }(),
				SpotifyID:     func() *string { s := "test-spotify-id"; return &s }(),
				ISRC:          func() *string { s := "TEST1234567"; return &s }(),
			},
		},
	}

	track := payload.ConvertToTrack(123)

	// Verify conversion
	if track.Name != "Test Track" {
		t.Errorf("Expected track name 'Test Track', got %s", track.Name)
	}
	if track.Album != "Test Album" {
		t.Errorf("Expected album 'Test Album', got %s", track.Album)
	}
	if track.RecordingMBID == nil || *track.RecordingMBID != "test-recording-mbid" {
		t.Errorf("Recording MBID not set correctly")
	}
	if track.ReleaseMBID == nil || *track.ReleaseMBID != "test-release-mbid" {
		t.Errorf("Release MBID not set correctly")
	}
	if track.DurationMs != 240000 {
		t.Errorf("Expected duration 240000ms, got %d", track.DurationMs)
	}
	if track.ISRC != "TEST1234567" {
		t.Errorf("Expected ISRC 'TEST1234567', got %s", track.ISRC)
	}
	if track.URL != "https://open.spotify.com/track/test-spotify-id" {
		t.Errorf("Expected Spotify URL to be constructed correctly, got %s", track.URL)
	}
	if track.ServiceBaseUrl != "spotify" {
		t.Errorf("Expected service 'spotify', got %s", track.ServiceBaseUrl)
	}

	expectedTime := time.Unix(1704067200, 0)
	if !track.Timestamp.Equal(expectedTime) {
		t.Errorf("Expected timestamp %v, got %v", expectedTime, track.Timestamp)
	}

	if !track.HasStamped {
		t.Error("Expected HasStamped to be true for external submissions")
	}

	// Check artists
	if len(track.Artist) != 2 {
		t.Errorf("Expected 2 artists, got %d", len(track.Artist))
	}
	if track.Artist[0].MBID == nil || *track.Artist[0].MBID != "test-artist-mbid-1" {
		t.Errorf("First artist MBID not set correctly")
	}
	if track.Artist[1].MBID == nil || *track.Artist[1].MBID != "test-artist-mbid-2" {
		t.Errorf("Second artist MBID not set correctly")
	}
}

func TestListenBrainzSubmission_WithMusicBrainzHydration(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)

	// Create a MusicBrainz service for hydration
	mbService := musicbrainz.NewMusicBrainzService(database)

	// Create minimal submission (artist and track name only)
	submission := models.ListenBrainzSubmission{
		ListenType: "single",
		Payload: []models.ListenBrainzPayload{
			{
				ListenedAt: func() *int64 { i := int64(1704067200); return &i }(),
				TrackMetadata: models.ListenBrainzTrackMetadata{
					ArtistName: "Daft Punk",
					TrackName:  "One More Time",
					// No MBIDs provided - should be hydrated
				},
			},
		},
	}

	jsonData, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("Failed to marshal submission: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	ctx := withUserContext(req.Context(), userID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	// Call handler with MusicBrainz service
	handler := apiSubmitListensHandler(database, nil, nil, mbService)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Verify track was saved
	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("Failed to get tracks from database: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("Expected 1 track in database, got %d", len(tracks))
	}

	track := tracks[0]

	// The track should have been hydrated with MusicBrainz data
	// Note: This test requires network access to MusicBrainz API
	// In a real test environment, you might want to mock the HTTP client
	if track.RecordingMBID != nil {
		t.Logf("Track was hydrated with recording MBID: %s", *track.RecordingMBID)
	}

	if track.ReleaseMBID != nil {
		t.Logf("Track was hydrated with release MBID: %s", *track.ReleaseMBID)
	}

	// Even if hydration fails, the track should still be saved with original data
	if track.Name != "One More Time" {
		t.Errorf("Expected track name 'One More Time', got %s", track.Name)
	}
	if len(track.Artist) == 0 || track.Artist[0].Name != "Daft Punk" {
		t.Errorf("Expected artist 'Daft Punk', got %v", track.Artist)
	}
}

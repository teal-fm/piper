package applemusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

// createTestJWT creates a minimal JWT for testing that will pass structural validation
func createTestJWT(teamID string, expiry time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))

	claims := map[string]any{
		"iss": teamID,
		"iat": time.Now().Unix(),
		"exp": expiry.Unix(),
	}
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	// Signature doesn't need to be valid for structural validation
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))

	return header + "." + payload + "." + signature
}

// Helper to create AppleRecentTrack for testing
func makeTestTrack(name, album, artist string) *AppleRecentTrack {
	track := &AppleRecentTrack{}
	track.Attributes.Name = name
	track.Attributes.AlbumName = album
	track.Attributes.ArtistName = artist
	return track
}

// trackResponseTransport returns a fixed JSON response simulating the Apple Music API.
type trackResponseTransport struct {
	response string
}

func (t *trackResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.response)),
		Header:     make(http.Header),
	}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newTestDB creates an in-memory SQLite database for testing.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	testDB, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := testDB.Initialize(); err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	t.Cleanup(func() { testDB.Close() })
	return testDB
}

// createTestUser creates a user in the DB with an Apple Music token set.
func createTestUser(t *testing.T, testDB *db.DB) *models.User {
	t.Helper()
	userID, err := testDB.CreateUser(&models.User{})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := testDB.UpdateAppleMusicUserToken(userID, "fake-token"); err != nil {
		t.Fatalf("failed to set apple music token: %v", err)
	}
	user, err := testDB.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	return user
}

// newTestService creates a Service backed by an in-memory DB and the given transport.
func newTestService(t *testing.T, testDB *db.DB, transport http.RoundTripper) *Service {
	t.Helper()
	tokenExpiry := time.Now().Add(1 * time.Hour)
	return &Service{
		DB:           testDB,
		httpClient:   &http.Client{Transport: transport},
		logger:       log.New(io.Discard, "", 0),
		teamID:       "test-team",
		keyID:        "test-key",
		cachedToken:  createTestJWT("test-team", tokenExpiry),
		cachedExpiry: tokenExpiry,
	}
}

// uploadedTrackJSON builds an Apple Music API response for an uploaded track (no URL).
func uploadedTrackJSON(name, artist, album string) string {
	track := map[string]any{
		"id": generateUploadHash(makeTestTrack(name, album, artist)),
		"attributes": map[string]string{
			"name":       name,
			"artistName": artist,
			"albumName":  album,
		},
	}
	data, _ := json.Marshal(map[string]any{"data": []any{track}})
	return string(data)
}

// processUserTestEnv sets up a DB, user, and service wired to the given API response.
type processUserTestEnv struct {
	testDB *db.DB
	user   *models.User
	svc    *Service
}

func newProcessUserTestEnv(t *testing.T, apiResponse string) *processUserTestEnv {
	t.Helper()
	testDB := newTestDB(t)
	user := createTestUser(t, testDB)
	transport := &trackResponseTransport{response: apiResponse}
	svc := newTestService(t, testDB, transport)
	return &processUserTestEnv{testDB: testDB, user: user, svc: svc}
}

// seedUploadedTrack saves an uploaded track to the DB, using its upload hash as the URL.
func (env *processUserTestEnv) seedUploadedTrack(t *testing.T, name, artist, album string) {
	t.Helper()
	hash := generateUploadHash(makeTestTrack(name, album, artist))
	_, err := env.testDB.SaveTrack(env.user.ID, db.SourceAppleMusic, &models.Track{
		Name:           name,
		Artist:         []models.Artist{{Name: artist}},
		Album:          album,
		URL:            hash,
		ServiceBaseUrl: "music.apple.com",
	})
	if err != nil {
		t.Fatalf("failed to seed track: %v", err)
	}
}

// trackCount returns the number of tracks stored for the test user.
func (env *processUserTestEnv) trackCount(t *testing.T) int {
	t.Helper()
	tracks, err := env.testDB.GetRecentTracks(env.user.ID, 100)
	if err != nil {
		t.Fatalf("failed to get recent tracks: %v", err)
	}
	return len(tracks)
}

func TestProcessUserSkipsDuplicateUploadedTrack(t *testing.T) {
	env := newProcessUserTestEnv(t, uploadedTrackJSON("My Upload", "Local Artist", "Local Album"))
	env.seedUploadedTrack(t, "My Upload", "Local Artist", "Local Album")

	if err := env.svc.ProcessUser(context.Background(), env.user); err != nil {
		t.Fatalf("ProcessUser returned error: %v", err)
	}

	if got := env.trackCount(t); got != 1 {
		t.Errorf("expected 1 track (no duplicate save), got %d", got)
	}
}

func TestProcessUserSavesDifferentUploadedTrack(t *testing.T) {
	env := newProcessUserTestEnv(t, uploadedTrackJSON("Old Upload", "Old Artist", "Old Album"))
	env.seedUploadedTrack(t, "Old Upload", "Old Artist", "Old Album")
	if err := env.svc.ProcessUser(context.Background(), env.user); err != nil {
		t.Fatal(err)
	}
	env.svc.httpClient.Transport = &trackResponseTransport{response: uploadedTrackJSON("New Upload", "New Artist", "New Album")}

	if err := env.svc.ProcessUser(context.Background(), env.user); err != nil {
		t.Fatalf("ProcessUser returned error: %v", err)
	}

	if got := env.trackCount(t); got != 2 {
		t.Errorf("expected 2 tracks (new upload saved), got %d", got)
	}
}

func TestGetCurrentAppleMusicTrackResolvesCatalogURL(t *testing.T) {
	testDB := newTestDB(t)
	var paths []string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/v1/me/recent/played/tracks":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"i.library-song","attributes":{"name":"Catalog Song","artistName":"Catalog Artist","albumName":"Catalog Album","playParams":{"id":"i.library-song","kind":"song","catalogId":"123456789"}}}]}`)),
				Header:     make(http.Header),
			}, nil
		case "/v1/me/storefront":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"us"}]}`)),
				Header:     make(http.Header),
			}, nil
		case "/v1/catalog/us/songs":
			if got := req.URL.Query().Get("ids"); got != "123456789" {
				t.Errorf("catalog ids = %q, want %q", got, "123456789")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"attributes":{"url":"https://music.apple.com/us/song/catalog-song/123456789"}}]}`)),
				Header:     make(http.Header),
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	})
	svc := newTestService(t, testDB, transport)
	user := createTestUser(t, testDB)

	track, err := svc.GetCurrentAppleMusicTrack(context.Background(), user)
	if err != nil {
		t.Fatalf("GetCurrentAppleMusicTrack returned error: %v", err)
	}
	if track.Attributes.URL != "https://music.apple.com/us/song/catalog-song/123456789" {
		t.Fatalf("track URL = %q, want catalog URL", track.Attributes.URL)
	}
	if got, want := strings.Join(paths, ","), "/v1/me/recent/played/tracks,/v1/me/storefront,/v1/catalog/us/songs"; got != want {
		t.Fatalf("request paths = %q, want %q", got, want)
	}
}

func TestFetchRecentPlayedTracksRequestsLibrarySongs(t *testing.T) {
	testDB := newTestDB(t)
	var gotTypes, gotLimit string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotTypes = req.URL.Query().Get("types")
		gotLimit = req.URL.Query().Get("limit")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Header:     make(http.Header),
		}, nil
	})
	svc := newTestService(t, testDB, transport)

	if _, err := svc.FetchRecentPlayedTracks(context.Background(), "user-token", 1); err != nil {
		t.Fatalf("FetchRecentPlayedTracks returned error: %v", err)
	}

	if gotTypes != "songs,library-songs" {
		t.Errorf("types query = %q, want %q", gotTypes, "songs,library-songs")
	}
	if gotLimit != "1" {
		t.Errorf("limit query = %q, want %q", gotLimit, "1")
	}
}

func TestFetchRecentPlayedTracksIncludesAppleErrorDetails(t *testing.T) {
	testDB := newTestDB(t)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body: io.NopCloser(strings.NewReader(`{
				"errors":[{
					"status":"403",
					"code":"AUTHORIZATION_ERROR",
					"title":"Forbidden",
					"detail":"The music user token is invalid or expired."
				}]
			}`)),
			Header: make(http.Header),
		}, nil
	})
	svc := newTestService(t, testDB, transport)

	_, err := svc.FetchRecentPlayedTracks(context.Background(), "user-token", 1)
	if err == nil {
		t.Fatal("FetchRecentPlayedTracks returned nil error")
	}
	for _, want := range []string{"403 Forbidden", "Forbidden", "invalid or expired", "AUTHORIZATION_ERROR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestGenerateUploadHash(t *testing.T) {
	tests := []struct {
		name     string
		track    *AppleRecentTrack
		wantHash string
	}{
		{
			name:     "basic track",
			track:    makeTestTrack("Test Song", "Test Album", "Test Artist"),
			wantHash: "am_uploaded_ec50bb20ebeddc6f04cb65bcff156fed3c59d7e311e3d3efbbea506c3f8ad5ae",
		},
		{
			name:     "track with different data",
			track:    makeTestTrack("Collaboration", "Best Hits", "Artist One"),
			wantHash: "am_uploaded_c387e87af520fd4dae9b9cb872bd9cfbc8f7a88ca1b49d874dc1045183ba43c3",
		},
		{
			name:     "track with empty album",
			track:    makeTestTrack("Single Track", "", "Solo Artist"),
			wantHash: "am_uploaded_f73402f47a46c5ff050b64a86d5a1fcbaa20a734a8963f458dca81be290731f7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateUploadHash(tt.track)

			// The hash should be prefixed with "am_uploaded_" so that it's clear it's an uploaded Apple Music track
			if !strings.HasPrefix(got, "am_uploaded_") {
				t.Errorf("generateUploadHash() = %v, want prefix 'am_uploaded_'", got)
			}

			if got != tt.wantHash {
				t.Errorf("generateUploadHash() = %v, want %v", got, tt.wantHash)
			}

			// Hash is deterministic -- same track will return same hash
			got2 := generateUploadHash(tt.track)
			if got != got2 {
				t.Errorf("generateUploadHash() is not deterministic: first=%v, second=%v", got, got2)
			}
		})
	}
}

func TestProcessUserHistoryWindow(t *testing.T) {
	database := newTestDB(t)
	user := createTestUser(t, database)
	transport := &trackResponseTransport{}
	poll := func(ids ...string) {
		t.Helper()
		items := []AppleRecentTrack{}
		for _, id := range ids {
			item := makeTestTrack(id, "Album", "Artist")
			item.ID = id
			items = append(items, *item)
		}
		body, _ := json.Marshal(recentPlayedResponse{Data: items})
		transport.response = string(body)
		// Recreate the service each poll to exercise persisted state.
		if err := newTestService(t, database, transport).ProcessUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	poll("A", "old") // First response is a baseline, not new listening activity.
	poll("B", "A", "old")
	poll("A") // A stale, truncated response must not replace the window.
	if _, err := database.SaveTrack(user.ID, db.SourceSpotify, &models.Track{Name: "Spotify", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	poll("D", "C", "B", "A")
	poll("B", "D", "A", "C")
	rows, err := database.Query(`SELECT name FROM tracks WHERE source = 'applemusic' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "B,C,D" {
		t.Fatalf("saved tracks = %s, want B,C,D", got)
	}
}

func TestProcessUserStableIdentityAndEmptyHistory(t *testing.T) {
	database := newTestDB(t)
	user := createTestUser(t, database)
	transport := &trackResponseTransport{response: `{"data":[]}`}
	svc := newTestService(t, database, transport)
	poll := func(body string) {
		t.Helper()
		transport.response = body
		if err := svc.ProcessUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	poll(`{"data":[]}`)
	poll(`{"data":[{"id":"bad"}]}`)
	poll(`{"data":[{"id":"A","attributes":{"name":"A","artistName":"Artist","url":"old-url"}}]}`)
	poll(`{"data":[{"id":"A","attributes":{"name":"Renamed","artistName":"Artist","url":"new-url"}}]}`)
	poll(`{"data":[]}`)
	// A library upload identified only by playParams.id remains stable after metadata changes.
	poll(`{"data":[{"attributes":{"name":"Upload","artistName":"Artist","playParams":{"id":"i.B"}}}]}`)
	poll(`{"data":[{"attributes":{"name":"Renamed upload","artistName":"Artist","playParams":{"id":"i.B"}}}]}`)
	tracks, err := database.GetRecentTracks(user.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "Upload" {
		t.Fatalf("tracks = %+v, want one Upload", tracks)
	}
}

func TestProcessUserRequestsFullHistoryPage(t *testing.T) {
	database := newTestDB(t)
	svc := newTestService(t, database, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("limit"); got != "30" {
			t.Errorf("limit = %s, want 30", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: make(http.Header)}, nil
	}))
	if err := svc.ProcessUser(context.Background(), createTestUser(t, database)); err != nil {
		t.Fatal(err)
	}
}

func TestProcessUserFailedResponseDoesNotResetHistory(t *testing.T) {
	database := newTestDB(t)
	user := createTestUser(t, database)
	transport := &trackResponseTransport{response: uploadedTrackJSON("A", "Artist", "Album")}
	svc := newTestService(t, database, transport)
	if err := svc.ProcessUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`broken json`, `{"data":null}`} {
		transport.response = body
		err := svc.ProcessUser(context.Background(), user)
		if body == "broken json" && err == nil {
			t.Fatal("expected decoding error")
		}
		if body != "broken json" && err != nil {
			t.Fatal(err)
		}
	}
	svc.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("test network failure") })
	if err := svc.ProcessUser(context.Background(), user); err == nil {
		t.Fatal("expected network error")
	}
	svc.httpClient.Transport = transport
	transport.response = uploadedTrackJSON("A", "Artist", "Album")
	if err := svc.ProcessUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	tracks, err := database.GetRecentTracks(user.ID, 100)
	if err != nil || len(tracks) != 0 {
		t.Fatalf("tracks after retry = %v, %v", tracks, err)
	}
}

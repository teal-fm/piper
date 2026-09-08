package listenbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"golang.org/x/time/rate"
)

func testDatabase(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(filepath.Join(t.TempDir(), "piper.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func linkedUser(t *testing.T, database *db.DB, username, token string) *models.User {
	t.Helper()
	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.LinkListenBrainz(userID, username, token); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func testService(database *db.DB, server *httptest.Server, playingNow playingNowPublisher) *Service {
	service := NewService(database, server.URL, "piper/test (test@example.com)", nil, nil, playingNow)
	service.httpClient.Transport = server.Client().Transport
	service.limiter = rate.NewLimiter(rate.Inf, 1)
	return service
}

func TestValidateToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/validate-token" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Token secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "piper/test (test@example.com)" {
			t.Errorf("User-Agent = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "valid": true, "user_name": "musicbrainz-user",
		})
	}))
	defer server.Close()

	username, err := testService(testDatabase(t), server, nil).ValidateToken(context.Background(), " secret ")
	if err != nil {
		t.Fatal(err)
	}
	if username != "musicbrainz-user" {
		t.Fatalf("username = %q", username)
	}
}

func TestSyncListensUsesResolvedMetadataAndDeduplicates(t *testing.T) {
	database := testDatabase(t)
	user := linkedUser(t, database, "rob", "secret")
	var calls int

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.EscapedPath() != "/1/user/rob/listens" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		wantCount := strconv.Itoa(initialSyncLimit)
		wantMinTS := ""
		if calls == 2 {
			wantCount = strconv.Itoa(updateSyncLimit)
			wantMinTS = "199"
		}
		if got := r.URL.Query().Get("count"); got != wantCount {
			t.Errorf("count = %q, want %q", got, wantCount)
		}
		if got := r.URL.Query().Get("min_ts"); got != wantMinTS {
			t.Errorf("min_ts = %q, want %q", got, wantMinTS)
		}
		if got := r.Header.Get("Authorization"); got != "Token secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"payload": {"count": 2, "user_id": "rob", "listens": [
				{"listened_at": 200, "track_metadata": {
					"artist_name": "Submitted Artist", "track_name": "Newer", "release_name": "Album",
					"additional_info": {"recording_mbid": "unverified", "tracknumber": "2", "spotify_id": "catalog-id", "origin_url": "https://www.youtube.com/watch?v=catalog-id", "music_service": "spotify"},
					"mbid_mapping": {
						"recording_mbid": "recording-2", "release_mbid": "release-2",
						"artists": [{"artist_mbid": "artist-2", "artist_credit_name": "Mapped Artist", "join_phrase": ""}],
						"url_rels": [{"type": "streaming", "url": "https://bandcamp.com/track/newer"}]
					}
				}},
				{"listened_at": 100, "track_metadata": {
					"artist_name": "First Artist", "track_name": "Older",
					"additional_info": {"duration": 123, "isrc": "ISRC1"}
				}}
			]}
		}`))
	}))
	defer server.Close()

	service := testService(database, server, nil)
	if err := service.syncListens(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := service.syncListens(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}

	tracks, err := database.GetRecentTracks(user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("stored %d tracks, want 2", len(tracks))
	}
	newer := tracks[0]
	if newer.Name != "Newer" || newer.RecordingMBID == nil || *newer.RecordingMBID != "recording-2" {
		t.Errorf("resolved recording was not stored: %+v", newer)
	}
	if newer.ReleaseMBID == nil || *newer.ReleaseMBID != "release-2" {
		t.Errorf("resolved release was not stored: %+v", newer.ReleaseMBID)
	}
	if len(newer.Artist) != 1 || newer.Artist[0].Name != "Mapped Artist" || newer.Artist[0].MBID == nil || *newer.Artist[0].MBID != "artist-2" {
		t.Errorf("resolved artist was not stored: %+v", newer.Artist)
	}
	if newer.URL != "https://listenbrainz.org/track/recording-2/" || newer.ServiceBaseUrl != "listenbrainz.org" {
		t.Errorf("incorrect listen provenance: url=%q service=%q", newer.URL, newer.ServiceBaseUrl)
	}
	older := tracks[1]
	if older.DurationMs != 123000 || older.ISRC != "ISRC1" {
		t.Errorf("additional_info was not stored: %+v", older)
	}
}

func TestSyncListensIncludesLatestSecond(t *testing.T) {
	database := testDatabase(t)
	user := linkedUser(t, database, "same-second", "secret")
	timestamp := time.Unix(100, 0).UTC()
	if _, err := database.SaveTrack(user.ID, db.SourceListenBrainz, &models.Track{
		Name: "Already stored", Artist: []models.Artist{{Name: "Artist"}}, Timestamp: timestamp,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveListenBrainzSyncTimestamp(user.ID, timestamp); err != nil {
		t.Fatal(err)
	}
	user.ListenBrainzSyncedAt = &timestamp

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("min_ts"); got != "99" {
			t.Errorf("min_ts = %q, want 99", got)
		}
		if got := r.URL.Query().Get("count"); got != strconv.Itoa(updateSyncLimit) {
			t.Errorf("count = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"payload":{"listens":[
			{"listened_at":100,"track_metadata":{"artist_name":"Artist","track_name":"Already stored"}},
			{"listened_at":100,"track_metadata":{"artist_name":"Artist","track_name":"Also at 100"}},
			{"listened_at":101,"track_metadata":{"artist_name":"Artist","track_name":"At 101"}}
		]}}`))
	}))
	defer server.Close()

	if err := testService(database, server, nil).syncListens(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	tracks, err := database.GetRecentTracks(user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 {
		t.Fatalf("stored %d tracks, want 3", len(tracks))
	}
}

type fakePlayingNow struct {
	mu        sync.Mutex
	published []*models.Track
	cleared   []int64
}

func (f *fakePlayingNow) PublishPlayingNow(_ context.Context, _ int64, track *models.Track) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *track
	f.published = append(f.published, &copy)
	return nil
}

func (f *fakePlayingNow) ClearPlayingNow(_ context.Context, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, userID)
	return nil
}

func TestSyncPlayingNowPublishesChangesAndClears(t *testing.T) {
	database := testDatabase(t)
	user := linkedUser(t, database, "listener", "secret")
	var requestCount int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount <= 2 {
			w.Write([]byte(`{"payload":{"playing_now":true,"listens":[{"track_metadata":{"artist_name":"Artist","track_name":"Song","mbid_mapping":{"recording_mbid":"recording"}}}]}}`))
			return
		}
		w.Write([]byte(`{"payload":{"playing_now":false,"listens":[]}}`))
	}))
	defer server.Close()

	fake := &fakePlayingNow{}
	service := testService(database, server, fake)
	for range 3 {
		if err := service.syncPlayingNow(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.published) != 1 {
		t.Fatalf("published %d times, want 1", len(fake.published))
	}
	if fake.published[0].HasStamped {
		t.Error("playing-now track must not be marked as a completed listen")
	}
	if track := fake.published[0]; track.URL != "https://listenbrainz.org/track/recording/" || track.ServiceBaseUrl != "listenbrainz.org" {
		t.Errorf("incorrect playing-now provenance: url=%q service=%q", track.URL, track.ServiceBaseUrl)
	}
	if fake.published[0].RecordingMBID == nil || *fake.published[0].RecordingMBID != "recording" {
		t.Errorf("resolved recording missing: %+v", fake.published[0])
	}
	if len(fake.cleared) != 1 || fake.cleared[0] != user.ID {
		t.Errorf("cleared = %v", fake.cleared)
	}
}

func TestGetJSONReportsAPIErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := testService(testDatabase(t), server, nil)
	err := service.getJSON(context.Background(), "/1/validate-token", "secret", url.Values{}, &struct{}{})
	if err == nil || err.Error() != "ListenBrainz returned HTTP 401: bad token" {
		t.Fatalf("error = %v", err)
	}
}

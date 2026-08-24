package db

import (
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := db.Initialize(); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createTestUser(t *testing.T, db *DB) int64 {
	t.Helper()
	id, err := db.CreateUser(&models.User{})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func saveTrack(t *testing.T, db *DB, userID int64, url string, source TrackSource, presentation string, at time.Time) {
	t.Helper()
	_, err := db.SaveTrack(userID, source, &models.Track{
		Name:           "Test Track",
		Artist:         []models.Artist{{Name: "Test Artist"}},
		URL:            url,
		ServiceBaseUrl: presentation,
		Timestamp:      at,
	})
	if err != nil {
		t.Fatalf("save track: %v", err)
	}
}

func TestBackfill(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	cases := []struct {
		name       string
		present    string
		wantSource string
	}{
		{"apple", "music.apple.com", string(SourceAppleMusic)},
		{"lastfm dot", "last.fm", string(SourceLastfm)},
		{"lastfm bare", "lastfm", string(SourceLastfm)},
		{"spotify", "open.spotify.com", string(SourceSpotify)},
		{"listenbrainz default", "listenbrainz", string(SourceListenBrainz)},
		{"listenbrainz spotify id", "spotify", string(SourceListenBrainz)},
		{"arbitrary", "Tidal Music App", string(externalSource)},
		{"empty", "", string(externalSource)},
	}
	for _, tc := range cases {
		_, err := db.Exec(`INSERT INTO tracks (user_id, name, artist, album, url, timestamp, service_base_url) VALUES (?, 'Seed','[]','',?, ?, ?)`, uid, "seeded/"+tc.present, time.Now().UTC(), tc.present)
		if err != nil {
			t.Fatalf("seed %s: %v", tc.name, err)
		}
	}
	if err := db.backfillTrackSources(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if err := db.QueryRow(`SELECT source FROM tracks WHERE user_id=? AND url=?`, uid, "seeded/"+tc.present).Scan(&got); err != nil {
				t.Fatalf("load: %v", err)
			}
			if got != tc.wantSource {
				t.Errorf("got %q want %q", got, tc.wantSource)
			}
		})
	}
}

func TestSaveTrack(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	cases := []struct {
		name    string
		source  TrackSource
		present string
	}{
		{"apple", SourceAppleMusic, "music.apple.com"},
		{"lastfm", SourceLastfm, "last.fm"},
		{"listenbrainz spoof apple", SourceListenBrainz, "music.apple.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "save/" + tc.name
			_, err := db.SaveTrack(uid, tc.source, &models.Track{
				Name:           "Persist",
				Artist:         []models.Artist{{Name: "Artist"}},
				URL:            url,
				ServiceBaseUrl: tc.present,
				Timestamp:      time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("want ok got %v", err)
			}
			var gotSource, gotPresent string
			if err := db.QueryRow(`SELECT source, service_base_url FROM tracks WHERE user_id=? AND url=?`, uid, url).Scan(&gotSource, &gotPresent); err != nil {
				t.Fatalf("load: %v", err)
			}
			if gotSource != string(tc.source) || gotPresent != tc.present {
				t.Errorf("got %q/%q want %q/%q", gotSource, gotPresent, tc.source, tc.present)
			}
		})
	}
}

func TestSaveTrackRejectsInvalidSource(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	cases := []struct {
		name   string
		source TrackSource
	}{
		{"empty", TrackSource("")},
		{"external", externalSource},
		{"unknown", TrackSource("banana")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.SaveTrack(uid, tc.source, &models.Track{
				Name:      "Persist",
				Artist:    []models.Artist{{Name: "Artist"}},
				URL:       "save/" + tc.name,
				Timestamp: time.Now().UTC(),
			})
			if err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestUpdateTrack(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	url := "upd/owner" + time.Now().Format(time.RFC3339Nano)
	id, err := db.SaveTrack(uid, SourceLastfm, &models.Track{
		Name:      "Original",
		Artist:    []models.Artist{{Name: "Artist"}},
		URL:       url,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.UpdateTrack(id, SourceLastfm, &models.Track{
		Name:      "Clobbered",
		Artist:    []models.Artist{{Name: "Artist"}},
		URL:       url,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("want ok got %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT name FROM tracks WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "Clobbered" {
		t.Errorf("got %q want %q", got, "Clobbered")
	}
}

func TestUpdateTrackRejectsForeignOrInvalidSource(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	cases := []struct {
		name     string
		updateAs TrackSource
	}{
		{"foreign", SourceAppleMusic},
		{"invalid", TrackSource("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "upd/" + tc.name + time.Now().Format(time.RFC3339Nano)
			id, err := db.SaveTrack(uid, SourceLastfm, &models.Track{
				Name:      "Original",
				Artist:    []models.Artist{{Name: "Artist"}},
				URL:       url,
				Timestamp: time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := db.UpdateTrack(id, tc.updateAs, &models.Track{
				Name:      "Clobbered",
				Artist:    []models.Artist{{Name: "Artist"}},
				URL:       url,
				Timestamp: time.Now().UTC(),
			}); err == nil {
				t.Fatal("want error")
			}
			var got string
			if err := db.QueryRow(`SELECT name FROM tracks WHERE id=?`, id).Scan(&got); err != nil {
				t.Fatalf("load: %v", err)
			}
			if got != "Original" {
				t.Errorf("got %q want %q", got, "Original")
			}
		})
	}
	t.Run("unknown id", func(t *testing.T) {
		if err := db.UpdateTrack(9999, SourceLastfm, &models.Track{Name: "Ghost", Artist: []models.Artist{{Name: "A"}}, Timestamp: time.Now().UTC()}); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestCursors(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	base := time.Now().UTC()

	saveTrack(t, db, uid, "https://music.apple.com/us/song/old/1", SourceAppleMusic, "music.apple.com", base.Add(-10*time.Minute))
	saveTrack(t, db, uid, "https://www.last.fm/music/_/New", SourceLastfm, "last.fm", base.Add(-5*time.Minute))
	saveTrack(t, db, uid, "https://music.apple.com/us/song/new/2", SourceAppleMusic, "music.apple.com", base.Add(-1*time.Minute))
	saveTrack(t, db, uid, "https://music.apple.com/us/song/spoofed/2", SourceListenBrainz, "music.apple.com", base.Add(-30*time.Second))

	cases := []struct {
		name    string
		source  TrackSource
		wantURL string
	}{
		{"apple newest", SourceAppleMusic, "https://music.apple.com/us/song/new/2"},
		{"lastfm only", SourceLastfm, "https://www.last.fm/music/_/New"},
		{"spoof does not leak into apple cursor", SourceAppleMusic, "https://music.apple.com/us/song/new/2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.GetLatestTrackForService(uid, tc.source)
			if err != nil {
				t.Fatalf("GetLatest: %v", err)
			}
			if got == nil || got.URL != tc.wantURL {
				t.Fatalf("got %v want %q", got, tc.wantURL)
			}
		})
	}
}

func TestCursorsEmpty(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	got, err := db.GetLatestTrackForService(uid, SourceAppleMusic)
	if err != nil {
		t.Fatalf("want ok got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil got %v", got)
	}
	if ts, err := db.GetLastKnownTimestamp(uid, SourceAppleMusic); err != nil || ts != nil {
		t.Fatalf("want nil timestamp got %v err %v", ts, err)
	}
}

func TestCursorsRejectsInvalidSource(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	base := time.Now().UTC()
	saveTrack(t, db, uid, "https://music.apple.com/us/song/new/2", SourceAppleMusic, "music.apple.com", base)

	cases := []struct {
		name   string
		source TrackSource
	}{
		{"empty", TrackSource("")},
		{"external", externalSource},
		{"unknown", TrackSource("banana")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.GetLatestTrackForService(uid, tc.source); err == nil {
				t.Fatal("want error")
			}
			if _, err := db.GetLastKnownTimestamp(uid, tc.source); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestCursorsTimestampScopesToSource(t *testing.T) {
	db := newTestDB(t)
	uid := createTestUser(t, db)
	base := time.Now().UTC()
	saveTrack(t, db, uid, "https://music.apple.com/us/song/new/2", SourceAppleMusic, "music.apple.com", base.Add(-1*time.Minute))
	saveTrack(t, db, uid, "https://www.last.fm/music/_/New", SourceLastfm, "last.fm", base.Add(-5*time.Minute))

	at := base.Add(-20 * time.Minute)
	saveTrack(t, db, uid, "https://www.last.fm/music/_/Old", SourceLastfm, "last.fm", at)
	got, err := db.GetLastKnownTimestamp(uid, SourceLastfm)
	if err != nil {
		t.Fatalf("timestamp: %v", err)
	}
	want := base.Add(-5 * time.Minute)
	if got == nil || !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

package db

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func TestAppleMusicHistoryPersistsAndBoundsWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piper.db")
	database, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if _, initialized, err := database.AppleMusicHistory(userID, []string{"A"}); err != nil || !initialized {
		t.Fatalf("baseline = %v, %v", initialized, err)
	}
	// An unchanged response must not expire with elapsed time or database reopen.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	seen, initialized, err := database.AppleMusicHistory(userID, []string{"B"})
	if err != nil || initialized || !seen["A"] || seen["B"] {
		t.Fatalf("reopened history = %v, %v, %v", seen, initialized, err)
	}
	for i := 0; i < appleMusicHistorySize; i++ {
		saved, err := database.SaveAppleMusicTrack(userID, fmt.Sprint(i), &models.Track{Name: "Track", Timestamp: time.Now()})
		if err != nil || !saved {
			t.Fatalf("save %d = %v, %v", i, saved, err)
		}
	}
	seen, _, err = database.AppleMusicHistory(userID, []string{"ignored"})
	if err != nil || len(seen) != appleMusicHistorySize || seen["A"] {
		t.Fatalf("bounded history = %v, %v", seen, err)
	}
	// Once evicted, a resource can be accepted as a replay.
	if saved, err := database.SaveAppleMusicTrack(userID, "A", &models.Track{Name: "A"}); err != nil || !saved {
		t.Fatalf("replay = %v, %v", saved, err)
	}
}

func TestAppleMusicSaveRollsBackAndDeduplicatesConcurrentPolls(t *testing.T) {
	database, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.AppleMusicHistory(userID, []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_submission BEFORE INSERT ON play_submissions BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{Name: "B", HasStamped: true, Timestamp: time.Now()}
	if saved, err := database.SaveAppleMusicTrack(userID, "B", track); err == nil || saved {
		t.Fatalf("failed save = %v, %v", saved, err)
	}
	seen, _, err := database.AppleMusicHistory(userID, []string{"B"})
	if err != nil || seen["B"] {
		t.Fatalf("failed save consumed B: %v, %v", seen, err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("tracks after rollback = %d, %v", count, err)
	}
	if _, err := database.Exec(`DROP TRIGGER fail_submission`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := *track
			if _, err := database.SaveAppleMusicTrack(userID, "B", &local); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, table := range []string{"tracks", "play_submissions"} {
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count = %d, %v", table, count, err)
		}
	}
	otherUser, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.AppleMusicHistory(otherUser, []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if saved, err := database.SaveAppleMusicTrack(otherUser, "B", track); err != nil || !saved {
		t.Fatalf("other user save = %v, %v", saved, err)
	}
}

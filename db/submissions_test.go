package db

import (
	"errors"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func TestSubmissionEligibilityAndLegacyMigration(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database)
	// Historical has_stamped=true must not be mistaken for a known failure.
	if _, err := database.Exec(`INSERT INTO tracks(user_id,name,artist,album,url,has_stamped) VALUES (?,'legacy','[]','','',1)`, uid); err != nil {
		t.Fatal(err)
	}
	for _, eligible := range []bool{false, true} {
		track := &models.Track{Name: "new", Timestamp: time.Now().UTC(), HasStamped: eligible}
		id, err := database.SaveTrack(uid, SourceAppleMusic, track)
		if err != nil {
			t.Fatal(err)
		}
		if track.PlayID != id {
			t.Fatal("saved track missing its play ID")
		}
	}
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	plays, err := database.ListUnpublishedPlays(uid, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 1 || plays[0].Status != "pending" {
		t.Fatalf("unexpected submissions: %+v", plays)
	}
}

func TestSubmissionClaimsOwnershipRetryAndExpiry(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database)
	other := createTestUser(t, database)
	id, err := database.SaveTrack(uid, SourceAppleMusic, &models.Track{Name: "play", HasStamped: true, Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimSubmission(other, id); !errors.Is(err, ErrSubmissionUnavailable) {
		t.Fatalf("other user claimed play: %v", err)
	}
	first, err := database.ClaimSubmission(uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimSubmission(uid, id); !errors.Is(err, ErrSubmissionUnavailable) {
		t.Fatalf("duplicate claim: %v", err)
	}
	if _, err := database.Exec(`UPDATE play_submissions SET last_attempt_at=? WHERE track_id=?`, time.Now().UTC().Add(-3*time.Minute), id); err != nil {
		t.Fatal(err)
	}
	second, err := database.ClaimSubmission(uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if second.RKey != first.RKey {
		t.Fatal("retry changed record key")
	}
	if err := database.FinishSubmission(first, ""); err == nil {
		t.Fatal("stale worker changed current claim")
	}
	if err := database.FinishSubmission(second, "HTTP 400: invalid_grant"); err != nil {
		t.Fatal(err)
	}
	retry, err := database.ClaimSubmission(uid, id)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Attempts != 3 {
		t.Fatalf("attempts = %d", retry.Attempts)
	}
	if err := database.FinishSubmission(retry, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimSubmission(uid, id); !errors.Is(err, ErrSubmissionUnavailable) {
		t.Fatalf("published play reclaimed: %v", err)
	}
	plays, err := database.ListUnpublishedPlays(uid, 20, 0)
	if err != nil || len(plays) != 0 {
		t.Fatalf("published play still listed: %+v %v", plays, err)
	}
}

func TestSaveTrackRollsBackWhenSubmissionCannotBeSaved(t *testing.T) {
	database := newTestDB(t)
	uid := createTestUser(t, database)
	if _, err := database.Exec(`CREATE TRIGGER reject_submission BEFORE INSERT ON play_submissions BEGIN SELECT RAISE(ABORT, 'storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{Name: "Must not be stranded", HasStamped: true, Timestamp: time.Now().UTC()}
	if _, err := database.SaveTrack(uid, SourceAppleMusic, track); err == nil {
		t.Fatal("expected storage failure")
	}
	tracks, err := database.GetRecentTracks(uid, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 || track.PlayID != 0 {
		t.Fatal("track survived without its pending submission")
	}
}

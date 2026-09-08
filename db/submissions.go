package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/teal-fm/piper/models"
)

// A missing submission row means the historical publishing outcome is unknown.
// HasStamped describes eligibility, not delivery to the PDS.
func (db *DB) initializeSubmissions() error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS play_submissions (
 track_id INTEGER PRIMARY KEY REFERENCES tracks(id),
 rkey TEXT NOT NULL UNIQUE,
 status TEXT NOT NULL DEFAULT 'pending',
 attempts INTEGER NOT NULL DEFAULT 0,
 last_attempt_at TIMESTAMP,
 last_error TEXT NOT NULL DEFAULT '',
 record_json TEXT,
 published_at TIMESTAMP
 )`)
	return err
}

type PlaySubmission struct {
	TrackID       int64
	Name          string
	Artists       []models.Artist
	Source        string
	PlayedAt      time.Time
	Status        string
	Attempts      int
	LastAttemptAt sql.NullTime
	LastError     string
	RKey          string
	RecordJSON    sql.NullString
}

func (db *DB) ListUnpublishedPlays(userID int64, limit, offset int) ([]PlaySubmission, error) {
	rows, err := db.Query(`SELECT t.id, t.name, t.artist, t.source, t.timestamp, s.status, s.attempts,
 s.last_attempt_at, s.last_error FROM play_submissions s JOIN tracks t ON t.id=s.track_id
 WHERE t.user_id=? AND s.status != 'published' ORDER BY t.timestamp, t.id LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plays []PlaySubmission
	for rows.Next() {
		var p PlaySubmission
		var artists string
		if err := rows.Scan(&p.TrackID, &p.Name, &artists, &p.Source, &p.PlayedAt, &p.Status, &p.Attempts, &p.LastAttemptAt, &p.LastError); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(artists), &p.Artists); err != nil && artists != "" {
			p.Artists = []models.Artist{{Name: artists}}
		}
		plays = append(plays, p)
	}
	return plays, rows.Err()
}

func (db *DB) GetTrackForUser(userID, trackID int64) (*models.Track, error) {
	return scanTrack(db.QueryRow(`SELECT id, name, recording_mbid, artist, album, release_mbid, url,
 timestamp, duration_ms, progress_ms, service_base_url, isrc, has_stamped FROM tracks WHERE id=? AND user_id=?`, trackID, userID))
}

var ErrSubmissionUnavailable = errors.New("play is already published, currently being submitted, or unavailable")

// ClaimSubmission uses an expiring claim so crashes do not strand plays. The
// attempt number fences late results from an older worker after lease expiry.
func (db *DB) ClaimSubmission(userID, trackID int64) (*PlaySubmission, error) {
	var p PlaySubmission
	err := db.QueryRow(`UPDATE play_submissions SET status='submitting', attempts=attempts+1, last_attempt_at=?
 WHERE track_id=? AND track_id IN (SELECT id FROM tracks WHERE user_id=?)
 AND (status IN ('pending','failed') OR (status='submitting' AND last_attempt_at < ?))
 RETURNING track_id,rkey,attempts,record_json`, time.Now().UTC(), trackID, userID, time.Now().UTC().Add(-2*time.Minute)).Scan(&p.TrackID, &p.RKey, &p.Attempts, &p.RecordJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSubmissionUnavailable
	}
	return &p, err
}

func (db *DB) SaveSubmissionRecord(p *PlaySubmission, record string) error {
	res, err := db.Exec(`UPDATE play_submissions SET record_json=? WHERE track_id=? AND attempts=? AND status='submitting'`, record, p.TrackID, p.Attempts)
	return submissionUpdated(res, err)
}

func (db *DB) FinishSubmission(p *PlaySubmission, message string) error {
	status := "published"
	var publishedAt any = time.Now().UTC()
	if message != "" {
		status = "failed"
		publishedAt = nil
	}
	res, err := db.Exec(`UPDATE play_submissions SET status=?, last_error=?, published_at=? WHERE track_id=? AND attempts=? AND status='submitting'`, status, message, publishedAt, p.TrackID, p.Attempts)
	return submissionUpdated(res, err)
}

func submissionUpdated(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("submission claim expired")
	}
	return nil
}

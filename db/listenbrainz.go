package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/teal-fm/piper/models"
)

// HasListenBrainzTrack distinguishes recordings sharing a title and timestamp.
func (db *DB) HasListenBrainzTrack(userID int64, track *models.Track) (bool, error) {
	artists, err := json.Marshal(track.Artist)
	if err != nil {
		return false, err
	}
	var exists bool
	err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM tracks WHERE user_id = ? AND source = ? AND name = ? AND timestamp = ? AND artist = ? AND album = ? AND recording_mbid IS ? AND release_mbid IS ?)`, userID, SourceListenBrainz, track.Name, track.Timestamp, string(artists), track.Album, track.RecordingMBID, track.ReleaseMBID).Scan(&exists)
	return exists, err
}

func (db *DB) LinkListenBrainz(userID int64, username, token string) error {
	if username == "" || token == "" {
		return errors.New("ListenBrainz username and token are required")
	}

	_, err := db.Exec(`
	UPDATE users
	SET listenbrainz_synced_at = CASE
	        WHEN listenbrainz_username = ? COLLATE NOCASE THEN listenbrainz_synced_at
	        ELSE NULL
	    END,
	    listenbrainz_username = ?,
	    listenbrainz_token = ?,
	    updated_at = ?
	WHERE id = ?`, username, username, token, time.Now().UTC(), userID)
	return err
}

func (db *DB) ClearListenBrainz(userID int64) error {
	_, err := db.Exec(`
	UPDATE users
	SET listenbrainz_username = NULL, listenbrainz_token = NULL,
	    listenbrainz_synced_at = NULL, updated_at = ?
	WHERE id = ?`, time.Now().UTC(), userID)
	return err
}

func (db *DB) GetAllUsersWithListenBrainz() ([]*models.User, error) {
	rows, err := db.Query(`
	SELECT id, atproto_did, most_recent_at_session_id, listenbrainz_username,
	       listenbrainz_token, listenbrainz_synced_at
	FROM users
	WHERE listenbrainz_username IS NOT NULL AND listenbrainz_token IS NOT NULL
	ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		if err := rows.Scan(
			&user.ID,
			&user.ATProtoDID,
			&user.MostRecentAtProtoSessionID,
			&user.ListenBrainzUsername,
			&user.ListenBrainzToken,
			&user.ListenBrainzSyncedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (db *DB) GetUserByListenBrainz(username string) (*models.User, error) {
	user := &models.User{}
	err := db.QueryRow(`
	SELECT id, atproto_did, most_recent_at_session_id, listenbrainz_username,
	       listenbrainz_token, listenbrainz_synced_at
	FROM users
	WHERE listenbrainz_username = ? COLLATE NOCASE`, username).Scan(
		&user.ID,
		&user.ATProtoDID,
		&user.MostRecentAtProtoSessionID,
		&user.ListenBrainzUsername,
		&user.ListenBrainzToken,
		&user.ListenBrainzSyncedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting ListenBrainz user %q: %w", username, err)
	}
	return user, nil
}

func (db *DB) SaveListenBrainzSyncTimestamp(userID int64, timestamp time.Time) error {
	timestamp = timestamp.UTC()
	_, err := db.Exec(`
	UPDATE users
	SET listenbrainz_synced_at = CASE
	        WHEN listenbrainz_synced_at IS NULL OR listenbrainz_synced_at < ? THEN ?
	        ELSE listenbrainz_synced_at
	    END,
	    updated_at = ?
	WHERE id = ?`, timestamp, timestamp, time.Now().UTC(), userID)
	return err
}

package db

import (
	"database/sql"
	"strings"

	"github.com/teal-fm/piper/models"
)

func (db *DB) AddLastFMUsername(userID int64, lastfmUsername string) error {
	lastfmUsername = strings.TrimSpace(lastfmUsername)
	if lastfmUsername == "" {
		return db.ClearLastFMUsername(userID)
	}
	_, err := db.Exec(`
    UPDATE users
    SET lastfm_username = ?
    WHERE id = ?`, lastfmUsername, userID)

	return err
}

func (db *DB) ClearLastFMUsername(userID int64) error {
	_, err := db.Exec(`
    UPDATE users
    SET lastfm_username = NULL
    WHERE id = ?`, userID)

	return err
}

func (db *DB) GetAllUsersWithLastFM() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, lastfm_username
    FROM users
    WHERE lastfm_username IS NOT NULL AND TRIM(lastfm_username) != ''
    ORDER BY id`)

	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			db.logger.Printf("Error closing rows: %s", err)
		}
	}(rows)

	var users []*models.User

	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.LastFMUsername)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func (db *DB) GetUserByLastFM(lastfmUsername string) (*models.User, error) {
	row := db.QueryRow(`
    SELECT id, username, email, atproto_did, most_recent_at_session_id, created_at, updated_at, lastfm_username
    FROM users
    WHERE lastfm_username = ?`, lastfmUsername)

	user := &models.User{}
	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.ATProtoDID, &user.MostRecentAtProtoSessionID,
		&user.CreatedAt, &user.UpdatedAt, &user.LastFMUsername)
	if err != nil {
		return nil, err
	}

	return user, nil
}

package db

import (
	"github.com/teal-fm/piper/models"
)

func (db *DB) AddLastFMUsername(userID int64, lastfmUsername string) error {
	_, err := db.Exec(`
    UPDATE users
    SET lastfm_username = ?
    WHERE id = ?`, lastfmUsername, userID)

	return err
}

func (db *DB) GetAllUsersWithLastFM() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, lastfm_username
    FROM users
    WHERE lastfm_username IS NOT NULL
    ORDER BY id`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
    SELECT id, username, email, atproto_did, created_at, updated_at, lastfm_username
    FROM users
    WHERE lastfm_username = ?`, lastfmUsername)

	user := &models.User{}
	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.ATProtoDID,
		&user.CreatedAt, &user.UpdatedAt, &user.LastFMUsername)
	if err != nil {
		return nil, err
	}

	return user, nil
}

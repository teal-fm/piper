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
    WHERE lastfm_username IS NOT NULL
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
		lastfmUsername := strings.TrimSpace(*user.LastFMUsername)
		if lastfmUsername == "" {
			continue
		}
		user.LastFMUsername = &lastfmUsername
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (db *DB) normalizeLastFMUsernames() error {
	rows, err := db.Query(`
    SELECT id, lastfm_username
    FROM users
    WHERE lastfm_username IS NOT NULL`)
	if err != nil {
		return err
	}

	type usernameUpdate struct {
		userID   int64
		username string
	}
	var updates []usernameUpdate
	for rows.Next() {
		var update usernameUpdate
		if err := rows.Scan(&update.userID, &update.username); err != nil {
			_ = rows.Close()
			return err
		}
		trimmed := strings.TrimSpace(update.username)
		if trimmed != update.username {
			update.username = trimmed
			updates = append(updates, update)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	transaction, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()

	for _, update := range updates {
		if update.username == "" {
			if _, err := transaction.Exec(`UPDATE users SET lastfm_username = NULL WHERE id = ?`, update.userID); err != nil {
				return err
			}
			continue
		}
		if _, err := transaction.Exec(`UPDATE users SET lastfm_username = ? WHERE id = ?`, update.username, update.userID); err != nil {
			return err
		}
	}

	return transaction.Commit()
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

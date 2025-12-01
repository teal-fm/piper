package db

import (
	"github.com/teal-fm/piper/models"
)

func (db *DB) AddPlyrFMHandle(userID int64, handle string) error {
	_, err := db.Exec(`
    UPDATE users
    SET plyrfm_handle = ?
    WHERE id = ?`, handle, userID)

	return err
}

func (db *DB) GetAllUsersWithPlyrFM() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, atproto_did, most_recent_at_session_id, plyrfm_handle
    FROM users
    WHERE plyrfm_handle IS NOT NULL
    ORDER BY id`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User

	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.ATProtoDID,
			&user.MostRecentAtProtoSessionID, &user.PlyrFMHandle)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func (db *DB) GetUserByPlyrFMHandle(handle string) (*models.User, error) {
	row := db.QueryRow(`
    SELECT id, username, email, atproto_did, most_recent_at_session_id, created_at, updated_at, plyrfm_handle
    FROM users
    WHERE plyrfm_handle = ?`, handle)

	user := &models.User{}
	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.ATProtoDID, &user.MostRecentAtProtoSessionID,
		&user.CreatedAt, &user.UpdatedAt, &user.PlyrFMHandle)
	if err != nil {
		return nil, err
	}

	return user, nil
}

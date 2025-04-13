package db

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teal-fm/piper/models"
)

// DB is a wrapper around sql.DB
type DB struct {
	*sql.DB
}

func New(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		os.MkdirAll(dir, 755)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return nil, err
	}

	return &DB{db}, nil
}

func (db *DB) Initialize() error {
	// Create users table
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT,                      -- Made nullable, might not have username initially
		email TEXT UNIQUE,                  -- Made nullable
		spotify_id TEXT UNIQUE,             -- Spotify specific ID
		access_token TEXT,                  -- Spotify access token
		refresh_token TEXT,                 -- Spotify refresh token
		token_expiry TIMESTAMP,             -- Spotify token expiry
		atproto_did TEXT UNIQUE,            -- Atproto DID (identifier)
		atproto_access_token TEXT,          -- Atproto access token
		atproto_refresh_token TEXT,         -- Atproto refresh token
		atproto_token_expiry TIMESTAMP,     -- Atproto token expiry
		atproto_scope TEXT,                 -- Atproto token scope
		atproto_token_type TEXT,            -- Atproto token type
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- Use default
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP  -- Use default
	)`)
	if err != nil {
		return err
	}

	// Create tracks table
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		artist TEXT NOT NULL, -- should be JSONB in PostgreSQL if we ever switch
		album TEXT NOT NULL,
		url TEXT NOT NULL,
		timestamp TIMESTAMP,
		duration_ms INTEGER,
		progress_ms INTEGER,
		service_base_url TEXT,
		isrc TEXT,
		has_stamped BOOLEAN,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS atproto_auth_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		state TEXT NOT NULL,
		did TEXT,
		pds_url TEXT NOT NULL,
		authserver_issuer TEXT NOT NULL,
		pkce_verifier TEXT NOT NULL,
		dpop_authserver_nonce TEXT NOT NULL,
		dpop_private_jwk TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return err
	}

	return nil
}

// create user without spotify id
func (db *DB) CreateUser(user *models.User) (int64, error) {
	now := time.Now()

	result, err := db.Exec(`
	INSERT INTO users (username, email, created_at, updated_at)
	VALUES (?, ?, ?, ?)`,
		user.Username, user.Email, now, now)

	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// Add spotify session to user, returning the updated user
func (db *DB) AddSpotifySession(userID int64, username, email, spotifyId, accessToken, refreshToken string, tokenExpiry time.Time) (*models.User, error) {
	now := time.Now()

	_, err := db.Exec(`
	UPDATE users SET username = ?, email = ?, spotify_id = ?, access_token = ?, refresh_token = ?, token_expiry = ?, created_at = ?, updated_at = ?
	WHERE id == ?
	`,
		username, email, spotifyId, accessToken, refreshToken, tokenExpiry, now, now, userID)
	if err != nil {
		return nil, err
	}

	user, err := db.GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	return user, err
}

func (db *DB) GetUserByID(ID int64) (*models.User, error) {
	user := &models.User{}

	err := db.QueryRow(`
	SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
	FROM users WHERE id = ?`, ID).Scan(
		&user.ID, &user.Username, &user.Email, &user.SpotifyID,
		&user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
		&user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return user, nil
}

func (db *DB) GetUserBySpotifyID(spotifyID string) (*models.User, error) {
	user := &models.User{}

	err := db.QueryRow(`
	SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
	FROM users WHERE spotify_id = ?`, spotifyID).Scan(
		&user.ID, &user.Username, &user.Email, &user.SpotifyID,
		&user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
		&user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return user, nil
}

func (db *DB) UpdateUserToken(userID int64, accessToken, refreshToken string, expiry time.Time) error {
	now := time.Now()

	_, err := db.Exec(`
	UPDATE users
	SET access_token = ?, refresh_token = ?, token_expiry = ?, updated_at = ?
	WHERE id = ?`,
		accessToken, refreshToken, expiry, now, userID)

	return err
}

func (db *DB) SaveTrack(userID int64, track *models.Track) (int64, error) {
	// Convert the Artist array to a string for storage
	artistString := ""
	if len(track.Artist) > 0 {
		bytes, err := json.Marshal(track.Artist)
		if err != nil {
			return 0, err
		} else {
			artistString = string(bytes)
		}
	}

	var trackID int64

	err := db.QueryRow(`
	INSERT INTO tracks (user_id, name, artist, album, url, timestamp, duration_ms, progress_ms, service_base_url, isrc, has_stamped)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	RETURNING id`,
		userID, track.Name, artistString, track.Album, track.URL, track.Timestamp,
		track.DurationMs, track.ProgressMs, track.ServiceBaseUrl, track.ISRC, track.HasStamped).Scan(&trackID)

	return trackID, err
}

func (db *DB) UpdateTrack(trackID int64, track *models.Track) error {
	// Convert the Artist array to a string for storage
	// In a production environment, you'd want to use proper JSON serialization
	artistString := ""
	if len(track.Artist) > 0 {
		bytes, err := json.Marshal(track.Artist)
		if err != nil {
			return err
		} else {
			artistString = string(bytes)
		}
	}

	_, err := db.Exec(`
	UPDATE tracks
	SET name = ?,
		artist = ?,
		album = ?,
		url = ?,
		timestamp = ?,
		duration_ms = ?,
		progress_ms = ?,
		service_base_url = ?,
		isrc = ?,
		has_stamped = ?
	WHERE id = ?`,
		track.Name, artistString, track.Album, track.URL, track.Timestamp,
		track.DurationMs, track.ProgressMs, track.ServiceBaseUrl, track.ISRC, track.HasStamped,
		trackID)

	return err
}

func (db *DB) GetRecentTracks(userID int64, limit int) ([]*models.Track, error) {
	// convert previous-format artist strings to current-format

	rows, err := db.Query(`
    SELECT id, name, artist, album, url, timestamp, duration_ms, progress_ms, service_base_url, isrc, has_stamped
    FROM tracks
    WHERE user_id = ?
    ORDER BY timestamp DESC
    LIMIT ?`, userID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []*models.Track

	for rows.Next() {
		var artistString string
		track := &models.Track{}
		err := rows.Scan(
			&track.PlayID,
			&track.Name,
			&artistString, // Scan into a string first
			&track.Album,
			&track.URL,
			&track.Timestamp,
			&track.DurationMs,
			&track.ProgressMs,
			&track.ServiceBaseUrl,
			&track.ISRC,
			&track.HasStamped,
		)

		if err != nil {
			return nil, err
		}

		// Convert the artist string to the Artist array structure
		var artists []models.Artist
		err = json.Unmarshal([]byte(artistString), &artists)
		if err != nil {
			// fallback to previous format
			artists = []models.Artist{{Name: artistString}}
		}
		track.Artist = artists
		tracks = append(tracks, track)
	}

	return tracks, nil
}

func (db *DB) GetUsersWithExpiredTokens() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
    FROM users
    WHERE refresh_token IS NOT NULL AND token_expiry < ?
    ORDER BY id`, time.Now())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User

	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.SpotifyID,
			&user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
			&user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func (db *DB) GetAllActiveUsers() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
    FROM users
    WHERE access_token IS NOT NULL AND token_expiry > ?
    ORDER BY id`, time.Now())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User

	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.SpotifyID,
			&user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
			&user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

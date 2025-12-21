package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teal-fm/piper/models"
)

type DB struct {
	*sql.DB
	logger *log.Logger
}

func New(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
    if dir != "." && dir != "/" {
        os.MkdirAll(dir, 0755)
    }

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Test the connection
	if err = db.Ping(); err != nil {
		return nil, err
	}
	logger := log.New(os.Stdout, "db: ", log.LstdFlags|log.Lmsgprefix)

	return &DB{db, logger}, nil
}

func (db *DB) Initialize() error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT,                      -- Made nullable, might not have username initially
		email TEXT UNIQUE,                  -- Made nullable
		atproto_did TEXT UNIQUE,            -- Atproto DID (identifier)
		most_recent_at_session_id TEXT,     -- Most recent oAuth session id
		spotify_id TEXT UNIQUE,             -- Spotify specific ID
		access_token TEXT,                  -- Spotify access token
		refresh_token TEXT,                 -- Spotify refresh token
		token_expiry TIMESTAMP,             -- Spotify token expiry
		lastfm_username TEXT,               -- Last.fm username
		applemusic_user_token TEXT,         -- Apple Music MusicKit user token
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- Use default
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP  -- Use default
	)`)
	if err != nil {
		return err
	}

	// Add missing columns to users table if they don't exist
	_, err = db.Exec(`ALTER TABLE users ADD COLUMN applemusic_user_token TEXT`)
	if err != nil && err.Error() != "duplicate column name: applemusic_user_token" {
		return err
	}

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		recording_mbid TEXT, -- Added
		artist TEXT NOT NULL, -- should be JSONB in PostgreSQL if we ever switch
		album TEXT NOT NULL,
		release_mbid TEXT, -- Added
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
		CREATE TABLE IF NOT EXISTS atproto_state (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			state TEXT NOT NULL,
			authserver_url TEXT,
			account_did TEXT,
			scopes TEXT,
			request_uri TEXT,
			authserver_token_endpoint TEXT,
			authserver_revocation_endpoint TEXT,
			pkce_verifier TEXT,
			dpop_authserver_nonce TEXT,
			dpop_privatekey_multibase TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	  CREATE INDEX IF NOT EXISTS atproto_state_state ON atproto_state(state);

`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS atproto_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			look_up_key TEXT NOT NULL,
			account_did TEXT,
			session_id TEXT,
			host_url TEXT,
			authserver_url TEXT,
			authserver_token_endpoint TEXT,
			authserver_revocation_endpoint TEXT,
			scopes TEXT,
			access_token TEXT,
			refresh_token TEXT,
			dpop_authserver_nonce TEXT,
			dpop_host_nonce TEXT,
			dpop_privatekey_multibase TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	CREATE INDEX IF NOT EXISTS idx_atproto_sessions_look_up_key ON atproto_sessions(look_up_key);
`)
	if err != nil {
		return err
	}

	// Add columns recording_mbid and release_mbid to tracks table if they don't exist
	_, err = db.Exec(`ALTER TABLE tracks ADD COLUMN recording_mbid TEXT`)
	if err != nil && err.Error() != "duplicate column name: recording_mbid" {
		// Handle errors other than 'duplicate column'
		return err
	}
	_, err = db.Exec(`ALTER TABLE tracks ADD COLUMN release_mbid TEXT`)
	if err != nil && err.Error() != "duplicate column name: release_mbid" {
		// Handle errors other than 'duplicate column'
		return err
	}

	return nil
}

// Apple Music developer token persistence
func (db *DB) ensureAppleMusicTokenTable() error {
    _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS applemusic_token (
            token TEXT,
            expires_at TIMESTAMP
        )`)
    return err
}

func (db *DB) GetAppleMusicDeveloperToken() (string, time.Time, bool, error) {
    if err := db.ensureAppleMusicTokenTable(); err != nil {
        return "", time.Time{}, false, err
    }
    var token string
    var exp time.Time
    err := db.QueryRow(`SELECT token, expires_at FROM applemusic_token LIMIT 1`).Scan(&token, &exp)
    if err == sql.ErrNoRows {
        return "", time.Time{}, false, nil
    }
    if err != nil {
        return "", time.Time{}, false, err
    }
    return token, exp, true, nil
}

func (db *DB) SaveAppleMusicDeveloperToken(token string, exp time.Time) error {
    if err := db.ensureAppleMusicTokenTable(); err != nil {
        return err
    }
    // Replace existing single row
    _, err := db.Exec(`DELETE FROM applemusic_token`)
    if err != nil {
        return err
    }
    _, err = db.Exec(`INSERT INTO applemusic_token (token, expires_at) VALUES (?, ?)`, token, exp)
    return err
}

// create user without spotify id
func (db *DB) CreateUser(user *models.User) (int64, error) {
	now := time.Now().UTC()

	result, err := db.Exec(`
	INSERT INTO users (username, email, created_at, updated_at)
	VALUES (?, ?, ?, ?)`,
		user.Username, user.Email, now, now)

	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// add spotify session to user, returning the updated user
func (db *DB) AddSpotifySession(userID int64, username, email, spotifyId, accessToken, refreshToken string, tokenExpiry time.Time) (*models.User, error) {
	now := time.Now().UTC()

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
    SELECT id, 
           username,
           email,
           atproto_did,
           most_recent_at_session_id,
           spotify_id,
           access_token,
           refresh_token,
           token_expiry,
           lastfm_username,
           applemusic_user_token,
           created_at,
           updated_at
    FROM users WHERE id = ?`, ID).Scan(
        &user.ID, &user.Username, &user.Email, &user.ATProtoDID, &user.MostRecentAtProtoSessionID, &user.SpotifyID,
        &user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
        &user.LastFMUsername, &user.AppleMusicUserToken,
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
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, lastfm_username, applemusic_user_token, created_at, updated_at
    FROM users WHERE spotify_id = ?`, spotifyID).Scan(
        &user.ID, &user.Username, &user.Email, &user.SpotifyID,
        &user.AccessToken, &user.RefreshToken, &user.TokenExpiry,
        &user.LastFMUsername, &user.AppleMusicUserToken,
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
	now := time.Now().UTC()

	_, err := db.Exec(`
	UPDATE users
	SET access_token = ?, refresh_token = ?, token_expiry = ?, updated_at = ?
	WHERE id = ?`,
		accessToken, refreshToken, expiry, now, userID)

	return err
}

func (db *DB) UpdateAppleMusicUserToken(userID int64, userToken string) error {
    now := time.Now().UTC()
    _, err := db.Exec(`
    UPDATE users
    SET applemusic_user_token = ?, updated_at = ?
    WHERE id = ?`,
        userToken, now, userID)
    return err
}

// ClearAppleMusicUserToken removes the stored Apple Music user token for a user
func (db *DB) ClearAppleMusicUserToken(userID int64) error {
    now := time.Now().UTC()
    _, err := db.Exec(`
        UPDATE users
        SET applemusic_user_token = NULL, updated_at = ?
        WHERE id = ?`,
        now, userID)
    return err
}

// GetAllAppleMusicLinkedUsers returns users who have an Apple Music user token set
func (db *DB) GetAllAppleMusicLinkedUsers() ([]*models.User, error) {
    rows, err := db.Query(`
        SELECT id, username, email, atproto_did, most_recent_at_session_id,
               spotify_id, access_token, refresh_token, token_expiry,
               lastfm_username, applemusic_user_token, created_at, updated_at
        FROM users
        WHERE applemusic_user_token IS NOT NULL AND applemusic_user_token != ''
        ORDER BY id`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var users []*models.User
    for rows.Next() {
        u := &models.User{}
        if err := rows.Scan(
            &u.ID, &u.Username, &u.Email, &u.ATProtoDID, &u.MostRecentAtProtoSessionID,
            &u.SpotifyID, &u.AccessToken, &u.RefreshToken, &u.TokenExpiry,
            &u.LastFMUsername, &u.AppleMusicUserToken, &u.CreatedAt, &u.UpdatedAt,
        ); err != nil {
            return nil, err
        }
        users = append(users, u)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return users, nil
}

func (db *DB) SaveTrack(userID int64, track *models.Track) (int64, error) {
	// marshal artist json
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
	INSERT INTO tracks (user_id, name, recording_mbid, artist, album, release_mbid, url, timestamp, duration_ms, progress_ms, service_base_url, isrc, has_stamped)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	RETURNING id`,
		userID, track.Name, track.RecordingMBID, artistString, track.Album, track.ReleaseMBID, track.URL, track.Timestamp,
		track.DurationMs, track.ProgressMs, track.ServiceBaseUrl, track.ISRC, track.HasStamped).Scan(&trackID)

	return trackID, err
}

func (db *DB) UpdateTrack(trackID int64, track *models.Track) error {
	// marshal artist json
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
	    recording_mbid = ?,
		artist = ?,
		album = ?,
		release_mbid = ?,
		url = ?,
		timestamp = ?,
		duration_ms = ?,
		progress_ms = ?,
		service_base_url = ?,
		isrc = ?,
		has_stamped = ?
	WHERE id = ?`,
		track.Name, track.RecordingMBID, artistString, track.Album, track.ReleaseMBID, track.URL, track.Timestamp,
		track.DurationMs, track.ProgressMs, track.ServiceBaseUrl, track.ISRC, track.HasStamped,
		trackID)

	return err
}

func (db *DB) GetRecentTracks(userID int64, limit int) ([]*models.Track, error) {
	rows, err := db.Query(`
    SELECT id, name, recording_mbid, artist, album, release_mbid, url, timestamp, duration_ms, progress_ms, service_base_url, isrc, has_stamped
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
			&track.RecordingMBID, // Scan new field
			&artistString,        // scan to be unmarshaled later
			&track.Album,
			&track.ReleaseMBID, // Scan new field
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

		// unmarshal artist json
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

// SpotifyQueryMapping maps Spotify sql query results to user structs
func SpotifyQueryMapping(rows *sql.Rows) ([]*models.User, error) {

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

func (db *DB) GetUsersWithExpiredTokens() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
    FROM users
    WHERE refresh_token IS NOT NULL AND token_expiry < ?
    ORDER BY id`, time.Now().UTC())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return SpotifyQueryMapping(rows)

}

func (db *DB) GetAllActiveUsers() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
    FROM users
    WHERE access_token IS NOT NULL
    ORDER BY id`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return SpotifyQueryMapping(rows)
}

func (db *DB) GetAllActiveUsersWithUnExpiredTokens() ([]*models.User, error) {
	rows, err := db.Query(`
    SELECT id, username, email, spotify_id, access_token, refresh_token, token_expiry, created_at, updated_at
    FROM users
    WHERE access_token IS NOT NULL AND token_expiry > ?
    ORDER BY id`, time.Now().UTC())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return SpotifyQueryMapping(rows)
}

// debug to view current user's information
// put everything in an 'any' type
func (db *DB) DebugViewUserInformation(userID int64) (map[string]any, error) {
	// Use Query instead of QueryRow to get access to column names and ensure only one row is processed
	rows, err := db.Query(`
				SELECT *
				FROM users
				WHERE id = ? LIMIT 1`, userID)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Check if there's a row to process
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			// Error during rows.Next() or preparing the result set
			return nil, fmt.Errorf("error checking for row: %w", err)
		}
		// No rows found, which is a valid outcome but might be considered an error in some contexts.
		// Returning sql.ErrNoRows is conventional.
		return nil, sql.ErrNoRows
	}

	// Prepare scan arguments: pointers to interface{} slices
	values := make([]any, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	// Scan the row values
	err = rows.Scan(scanArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Check for errors that might have occurred during iteration (after Scan)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error after scanning row: %w", err)
	}

	// Create the result map
	resultMap := make(map[string]any, len(cols))
	for i, colName := range cols {
		val := values[i]
		// SQLite often returns []byte for TEXT columns, convert to string for usability.
		// Also handle potential nil values appropriately.
		if b, ok := val.([]byte); ok {
			resultMap[colName] = string(b)
		} else {
			resultMap[colName] = val // Keep nil as nil, numbers as numbers, etc.
		}
	}

	return resultMap, nil
}

func (db *DB) GetLastKnownTimestamp(userID int64) (*time.Time, error) {
	var lastTimestamp time.Time
	err := db.QueryRow(`
		SELECT timestamp
		FROM tracks
		WHERE user_id = ?
		ORDER BY timestamp DESC
		LIMIT 1`, userID).Scan(&lastTimestamp)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query last scrobble timestamp for user %d: %w", userID, err)
	}

	return &lastTimestamp, nil
}

//

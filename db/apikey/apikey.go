package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
)

// ApiKey represents an API key for authenticating requests
type ApiKey struct {
	ID        string
	KeyPrefix string
	UserID    int64
	Name      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Manager ApiKeyManager manages API keys
type Manager struct {
	db      *db.DB
	apiKeys map[string]*ApiKey
	mu      sync.RWMutex
}

// NewApiKeyManager creates a new API key manager
func NewApiKeyManager(database *db.DB) *Manager {
	// Initialize API keys table if it doesn't exist
	_, err := database.Exec(`
	CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		key_hash TEXT UNIQUE,
		key_prefix TEXT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		created_at TIMESTAMP,
		expires_at TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)

	if err != nil {
		log.Printf("Error creating api_keys table: %v", err)
	}
	for _, statement := range []string{
		`ALTER TABLE api_keys ADD COLUMN key_hash TEXT`,
		`ALTER TABLE api_keys ADD COLUMN key_prefix TEXT`,
	} {
		if _, alterErr := database.Exec(statement); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			log.Printf("Error updating api_keys table: %v", alterErr)
		}
	}
	if err := migrateLegacyAPIKeys(database); err != nil {
		log.Printf("Error migrating legacy API keys: %v", err)
	}
	if _, err := database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash)`); err != nil {
		log.Printf("Error indexing API key hashes: %v", err)
	}

	am := &Manager{
		db:      database,
		apiKeys: make(map[string]*ApiKey),
	}

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			am.cleanupExpiredApiKeys()
		}
	}()

	return am
}

// cleanupExpiredApiKeys removes expired API keys from the in-memory map and the database
func (am *Manager) cleanupExpiredApiKeys() {
	now := time.Now().UTC()

	am.mu.Lock()
	for id, apiKey := range am.apiKeys {
		if now.After(apiKey.ExpiresAt) {
			delete(am.apiKeys, id)
		}
	}
	am.mu.Unlock()

	_, err := am.db.Exec("DELETE FROM api_keys WHERE expires_at < ?", now)
	if err != nil {
		log.Printf("Error deleting expired API keys from database: %v", err)
	}
}

// CreateApiKey creates a new API key for a user
func (am *Manager) CreateApiKey(userID int64, name string, validityDays int) (*ApiKey, string, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	rawKey, err := randomToken(32)
	if err != nil {
		return nil, "", err
	}
	apiKeyID, err := randomToken(16)
	if err != nil {
		return nil, "", err
	}
	keyHash := hashAPIKey(rawKey)
	keyPrefix := rawKey
	if len(keyPrefix) > 8 {
		keyPrefix = keyPrefix[:8]
	}

	now := time.Now().UTC()
	expiresAt := now.AddDate(0, 0, validityDays) // Default to validityDays days validity

	apiKey := &ApiKey{
		ID:        apiKeyID,
		KeyPrefix: keyPrefix,
		UserID:    userID,
		Name:      name,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	// Store API key in memory
	am.apiKeys[keyHash] = apiKey

	// Store API key in database
	_, err = am.db.Exec(`
	INSERT INTO api_keys (id, key_hash, key_prefix, user_id, name, created_at, expires_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)`,
		apiKeyID, keyHash, keyPrefix, userID, name, now, expiresAt)

	if err != nil {
		delete(am.apiKeys, keyHash)
		return nil, "", err
	}

	return apiKey, rawKey, nil
}

// GetApiKey retrieves an API key by ID
func (am *Manager) GetApiKey(apiKeyID string) (*ApiKey, bool) {
	keyHash := hashAPIKey(apiKeyID)
	// First check in-memory cache
	am.mu.RLock()
	apiKey, exists := am.apiKeys[keyHash]
	am.mu.RUnlock()

	if exists {
		// Check if API key is expired
		if time.Now().UTC().After(apiKey.ExpiresAt) {
			if err := am.DeleteApiKey(apiKeyID); err != nil {
				log.Printf("Error deleting an expired API key: %v", err)
			}
			return nil, false
		}
		return apiKey, true
	}

	// If not in memory, check database
	apiKey = &ApiKey{}
	err := am.db.QueryRow(`
	SELECT id, COALESCE(key_prefix, ''), user_id, name, created_at, expires_at
	FROM api_keys WHERE key_hash = ?`, keyHash).Scan(
		&apiKey.ID, &apiKey.KeyPrefix, &apiKey.UserID, &apiKey.Name, &apiKey.CreatedAt, &apiKey.ExpiresAt)

	if err != nil {
		return nil, false
	}

	if time.Now().UTC().After(apiKey.ExpiresAt) {
		if err := am.DeleteApiKey(apiKeyID); err != nil {
			log.Printf("Error deleting an expired API key: %v", err)
		}
		return nil, false
	}

	// Add to in-memory cache
	am.mu.Lock()
	am.apiKeys[keyHash] = apiKey
	am.mu.Unlock()

	return apiKey, true
}

// DeleteApiKey removes an API key
func (am *Manager) DeleteApiKey(apiKeyID string) error {
	am.mu.Lock()
	for keyHash, apiKey := range am.apiKeys {
		if apiKey.ID == apiKeyID {
			delete(am.apiKeys, keyHash)
		}
	}
	am.mu.Unlock()

	_, err := am.db.Exec("DELETE FROM api_keys WHERE id = ?", apiKeyID)
	return err
}

// GetUserApiKeys retrieves all API keys for a user
func (am *Manager) GetUserApiKeys(userID int64) ([]*ApiKey, error) {
	rows, err := am.db.Query(`
	SELECT id, COALESCE(key_prefix, ''), user_id, name, created_at, expires_at
	FROM api_keys 
	WHERE user_id = ? 
	ORDER BY created_at DESC`, userID)

	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			fmt.Println("Error closing API keys rows: %w", err)
		}
	}(rows)

	var apiKeys []*ApiKey
	for rows.Next() {
		apiKey := &ApiKey{}
		err := rows.Scan(
			&apiKey.ID,
			&apiKey.KeyPrefix,
			&apiKey.UserID,
			&apiKey.Name,
			&apiKey.CreatedAt,
			&apiKey.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		apiKeys = append(apiKeys, apiKey)
	}

	return apiKeys, nil
}

func randomToken(byteLength int) (string, error) {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashAPIKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

func migrateLegacyAPIKeys(database *db.DB) error {
	rows, err := database.Query(`SELECT id FROM api_keys WHERE key_hash IS NULL OR key_hash = ''`)
	if err != nil {
		return err
	}
	var legacyKeys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			_ = rows.Close()
			return err
		}
		legacyKeys = append(legacyKeys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, rawKey := range legacyKeys {
		newID, err := randomToken(16)
		if err != nil {
			return err
		}
		prefix := rawKey
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		if _, err := database.Exec(`UPDATE api_keys SET id = ?, key_hash = ?, key_prefix = ? WHERE id = ?`, newID, hashAPIKey(rawKey), prefix, rawKey); err != nil {
			return err
		}
	}
	return nil
}

// ExtractApiKey extracts the API key from the request
func ExtractApiKey(r *http.Request) (string, error) {
	// Try to get from Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && (strings.ToLower(parts[0]) == "bearer" || strings.ToLower(parts[0]) == "token") {
			return parts[1], nil
		}
	}

	// Then try from query parameter
	apiKey := r.URL.Query().Get("api_key")
	if apiKey != "" {
		return apiKey, nil
	}

	return "", errors.New("no API key found in request")
}

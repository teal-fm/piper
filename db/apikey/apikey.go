package apikey

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		created_at TIMESTAMP,
		expires_at TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)

	if err != nil {
		log.Printf("Error creating api_keys table: %v", err)
	}

	return &Manager{
		db:      database,
		apiKeys: make(map[string]*ApiKey),
	}
}

// CreateApiKey creates a new API key for a user
func (am *Manager) CreateApiKey(userID int64, name string, validityDays int) (*ApiKey, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Generate random API key
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	apiKeyID := base64.URLEncoding.EncodeToString(b)

	now := time.Now().UTC()
	expiresAt := now.AddDate(0, 0, validityDays) // Default to validityDays days validity

	apiKey := &ApiKey{
		ID:        apiKeyID,
		UserID:    userID,
		Name:      name,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	// Store API key in memory
	am.apiKeys[apiKeyID] = apiKey

	// Store API key in database
	_, err := am.db.Exec(`
	INSERT INTO api_keys (id, user_id, name, created_at, expires_at)
	VALUES (?, ?, ?, ?, ?)`,
		apiKeyID, userID, name, now, expiresAt)

	if err != nil {
		return nil, err
	}

	return apiKey, nil
}

// GetApiKey retrieves an API key by ID
func (am *Manager) GetApiKey(apiKeyID string) (*ApiKey, bool) {
	// First check in-memory cache
	am.mu.RLock()
	apiKey, exists := am.apiKeys[apiKeyID]
	am.mu.RUnlock()

	if exists {
		// Check if API key is expired
		if time.Now().UTC().After(apiKey.ExpiresAt) {
			err := am.DeleteApiKey(apiKeyID)
			fmt.Println("Error deleting an expired API key: %w", err)
			if err != nil {
				return nil, false
			}
			return nil, false
		}
		return apiKey, true
	}

	// If not in memory, check database
	apiKey = &ApiKey{ID: apiKeyID}
	err := am.db.QueryRow(`
	SELECT user_id, name, created_at, expires_at
	FROM api_keys WHERE id = ?`, apiKeyID).Scan(
		&apiKey.UserID, &apiKey.Name, &apiKey.CreatedAt, &apiKey.ExpiresAt)

	if err != nil {
		return nil, false
	}

	if time.Now().UTC().After(apiKey.ExpiresAt) {
		err := am.DeleteApiKey(apiKeyID)
		fmt.Println("Error deleting an expired API key: %w", err)
		if err != nil {
			return nil, false
		}
		return nil, false
	}

	// Add to in-memory cache
	am.mu.Lock()
	am.apiKeys[apiKeyID] = apiKey
	am.mu.Unlock()

	return apiKey, true
}

// DeleteApiKey removes an API key
func (am *Manager) DeleteApiKey(apiKeyID string) error {
	am.mu.Lock()
	delete(am.apiKeys, apiKeyID)
	am.mu.Unlock()

	_, err := am.db.Exec("DELETE FROM api_keys WHERE id = ?", apiKeyID)
	return err
}

// GetUserApiKeys retrieves all API keys for a user
func (am *Manager) GetUserApiKeys(userID int64) ([]*ApiKey, error) {
	rows, err := am.db.Query(`
	SELECT id, user_id, name, created_at, expires_at
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

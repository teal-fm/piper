package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/db/apikey"
)

// session/session.go
type Session struct {
	ID                  string
	UserID              int64
	ATprotoDID          string
	ATprotoAccessToken  string
	ATprotoRefreshToken string
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type SessionManager struct {
	db        *db.DB
	sessions  map[string]*Session // use in memory cache if necessary
	apiKeyMgr *apikey.ApiKeyManager
	mu        sync.RWMutex
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	// Initialize session table if it doesn't exist
	database, err := db.New("./data/piper.db")
	if err != nil {
		log.Printf("Error connecting to database for sessions, falling back to in memory only: %v", err)
		// back up to in memory only
		return &SessionManager{
			sessions: make(map[string]*Session),
		}
	}

	_, err = database.Exec(`
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at TIMESTAMP,
		expires_at TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)

	if err != nil {
		log.Printf("Error creating sessions table: %v", err)
	}

	// Create API key manager
	apiKeyMgr := apikey.NewApiKeyManager(database)

	return &SessionManager{
		db:        database,
		sessions:  make(map[string]*Session),
		apiKeyMgr: apiKeyMgr,
	}
}

// create a new session for a user
func (sm *SessionManager) CreateSession(userID int64) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// random session id
	b := make([]byte, 32)
	rand.Read(b)
	sessionID := base64.URLEncoding.EncodeToString(b)

	now := time.Now()
	expiresAt := now.Add(24 * time.Hour) // 24-hour session

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	// store session in memory
	sm.sessions[sessionID] = session

	// store session in database if available
	if sm.db != nil {
		_, err := sm.db.Exec(`
		INSERT INTO sessions (id, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)`,
			sessionID, userID, now, expiresAt)

		if err != nil {
			log.Printf("Error storing session in database: %v", err)
		}
	}

	return session
}

// retrieve a session by ID
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	// First check in-memory cache
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if exists {
		// Check if session is expired
		if time.Now().After(session.ExpiresAt) {
			sm.DeleteSession(sessionID)
			return nil, false
		}
		return session, true
	}

	// If not in memory and we have a database, check there
	if sm.db != nil {
		session = &Session{ID: sessionID}

		err := sm.db.QueryRow(`
		SELECT user_id, created_at, expires_at
		FROM sessions WHERE id = ?`, sessionID).Scan(
			&session.UserID, &session.CreatedAt, &session.ExpiresAt)

		if err != nil {
			return nil, false
		}

		if time.Now().After(session.ExpiresAt) {
			sm.DeleteSession(sessionID)
			return nil, false
		}

		// add to in-memory cache
		sm.mu.Lock()
		sm.sessions[sessionID] = session
		sm.mu.Unlock()

		return session, true
	}

	return nil, false
}

// remove a session
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	if sm.db != nil {
		_, err := sm.db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
		if err != nil {
			log.Printf("Error deleting session from database: %v", err)
		}
	}
}

// set a session cookie for the user
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, session *Session) {
	cookie := &http.Cookie{
		Name:     "session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		Expires:  session.ExpiresAt,
	}
	http.SetCookie(w, cookie)
}

// ClearSessionCookie clears the session cookie
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)
}

// HandleLogout handles user logout
func (sm *SessionManager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		sm.DeleteSession(cookie.Value)
	}

	sm.ClearSessionCookie(w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GetAPIKeyManager returns the API key manager
func (sm *SessionManager) GetAPIKeyManager() *apikey.ApiKeyManager {
	return sm.apiKeyMgr
}

// CreateAPIKey creates a new API key for a user
func (sm *SessionManager) CreateAPIKey(userID int64, name string, validityDays int) (*apikey.ApiKey, error) {
	return sm.apiKeyMgr.CreateApiKey(userID, name, validityDays)
}

// WithAuth is a middleware that checks if a user is authenticated via cookies or API key
func WithAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// First try API key authentication (for API requests)
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr == nil && apiKeyStr != "" {
			// Validate API key
			apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
			if valid {
				// Add user ID to context
				ctx := WithUserID(r.Context(), apiKey.UserID)
				r = r.WithContext(ctx)

				// Set a flag in the context that this is an API request
				ctx = WithAPIRequest(r.Context(), true)
				r = r.WithContext(ctx)

				handler(w, r)
				return
			}
		}

		// Fall back to cookie authentication (for browser requests)
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login/spotify", http.StatusSeeOther)
			return
		}

		// Verify cookie session
		session, exists := sm.GetSession(cookie.Value)
		if !exists {
			http.Redirect(w, r, "/login/spotify", http.StatusSeeOther)
			return
		}

		// Add session information to request context
		ctx := WithUserID(r.Context(), session.UserID)
		r = r.WithContext(ctx)

		handler(w, r)
	}
}

func WithPossibleAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authenticated := false // Default to not authenticated

		// 1. Try API key authentication
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr == nil && apiKeyStr != "" {
			apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
			if valid {
				// API Key valid: Add UserID, API flag, and set auth status
				ctx = WithUserID(ctx, apiKey.UserID)
				ctx = WithAPIRequest(ctx, true)
				authenticated = true
				// Update request context and call handler
				r = r.WithContext(WithAuthStatus(ctx, authenticated))
				handler(w, r)
				return
			}
			// If API key was provided but invalid, we still proceed without auth
		}

		// 2. If no valid API key, try cookie authentication
		if !authenticated { // Only check cookies if API key didn't authenticate
			cookie, err := r.Cookie("session")
			if err == nil { // Cookie exists
				session, exists := sm.GetSession(cookie.Value)
				if exists {
					// Session valid: Add UserID and set auth status
					ctx = WithUserID(ctx, session.UserID)
					// ctx = WithAPIRequest(ctx, false) // Not strictly needed, default is false
					authenticated = true
				}
				// If session cookie exists but is invalid/expired, we proceed without auth
			}
		}

		// 3. Set final auth status (could be true or false) and call handler
		r = r.WithContext(WithAuthStatus(ctx, authenticated))
		handler(w, r)
	}
}

// WithAPIAuth is a middleware specifically for API-only endpoints (no cookie fallback, returns 401 instead of redirect)
func WithAPIAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try API key authentication
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr != nil || apiKeyStr == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "API key is required"}`))
			return
		}

		// Validate API key
		apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid or expired API key"}`))
			return
		}

		// Add user ID to context
		ctx := WithUserID(r.Context(), apiKey.UserID)
		// Mark as API request
		ctx = WithAPIRequest(ctx, true)
		r = r.WithContext(ctx)

		handler(w, r)
	}
}

// Context keys
type contextKey int

const (
	userIDKey contextKey = iota
	apiRequestKey
	authStatusKey
)

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func GetUserID(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey).(int64)
	return userID, ok
}

func WithAuthStatus(ctx context.Context, isAuthed bool) context.Context {
	return context.WithValue(ctx, authStatusKey, isAuthed)
}

func WithAPIRequest(ctx context.Context, isAPI bool) context.Context {
	return context.WithValue(ctx, apiRequestKey, isAPI)
}

func IsAPIRequest(ctx context.Context) bool {
	isAPI, ok := ctx.Value(apiRequestKey).(bool)
	return ok && isAPI
}

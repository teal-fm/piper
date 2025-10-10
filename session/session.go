package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/db/apikey"
)

// session/session.go
type Session struct {

	//need to re work this. May add onto it for atproto oauth. But need to be careful about that expiresd
	//Maybe a speerate oauth session store table and it has a created date? yeah do that then can look it up by session id from this table for user actions

	ID               string
	UserID           int64
	ATProtoSessionID string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type SessionManager struct {
	db        *db.DB
	sessions  map[string]*Session // use in memory cache if necessary
	apiKeyMgr *apikey.ApiKeyManager
	mu        sync.RWMutex
}

func NewSessionManager(database *db.DB) *SessionManager {

	_, err := database.Exec(`
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,		
		at_proto_session_id TEXT NOT NULL,
		created_at TIMESTAMP,
		expires_at TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)

	if err != nil {
		log.Printf("Error creating sessions table: %v", err)
	}

	apiKeyMgr := apikey.NewApiKeyManager(database)

	return &SessionManager{
		db:        database,
		sessions:  make(map[string]*Session),
		apiKeyMgr: apiKeyMgr,
	}
}

// create a new session for a user
func (sm *SessionManager) CreateSession(userID int64, atProtoSessionId string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// random session id
	b := make([]byte, 32)
	rand.Read(b)
	sessionID := base64.URLEncoding.EncodeToString(b)

	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour) // 24-hour session

	session := &Session{
		ID:               sessionID,
		UserID:           userID,
		ATProtoSessionID: atProtoSessionId,
		CreatedAt:        now,
		ExpiresAt:        expiresAt,
	}

	// store session in memory
	sm.sessions[sessionID] = session

	// store session in database if available
	if sm.db != nil {
		_, err := sm.db.Exec(`
		INSERT INTO sessions (id, user_id, at_proto_session_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
			sessionID, userID, atProtoSessionId, now, expiresAt)

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
		if time.Now().UTC().After(session.ExpiresAt) {
			sm.DeleteSession(sessionID)
			return nil, false
		}
		return session, true
	}

	// if not in memory and we have a database, check there
	if sm.db != nil {
		session = &Session{ID: sessionID}

		err := sm.db.QueryRow(`
		SELECT user_id, at_proto_session_id, created_at, expires_at
		FROM sessions WHERE id = ?`, sessionID).Scan(
			&session.UserID, &session.ATProtoSessionID, &session.CreatedAt, &session.ExpiresAt)

		if err != nil {
			return nil, false
		}

		if time.Now().UTC().After(session.ExpiresAt) {
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

func (sm *SessionManager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	//TODO should we clear atproto oauth session as well?
	if err == nil {
		sm.DeleteSession(cookie.Value)
	}

	sm.ClearSessionCookie(w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (sm *SessionManager) GetAPIKeyManager() *apikey.ApiKeyManager {
	return sm.apiKeyMgr
}

func (sm *SessionManager) CreateAPIKey(userID int64, name string, validityDays int) (*apikey.ApiKey, error) {
	return sm.apiKeyMgr.CreateApiKey(userID, name, validityDays)
}

// middleware that checks if a user is authenticated via cookies or API key
func WithAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// first: check API keys
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr == nil && apiKeyStr != "" {
			apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
			if valid {
				ctx := WithUserID(r.Context(), apiKey.UserID)
				r = r.WithContext(ctx)

				// set a flag for api requests
				ctx = WithAPIRequest(r.Context(), true)
				r = r.WithContext(ctx)

				handler(w, r)
				return
			}
		}

		// if not found, check cookies for session value
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login/spotify", http.StatusSeeOther)
			return
		}

		session, exists := sm.GetSession(cookie.Value)
		if !exists {
			http.Redirect(w, r, "/login/spotify", http.StatusSeeOther)
			return
		}

		ctx := WithUserID(r.Context(), session.UserID)
		r = r.WithContext(ctx)

		handler(w, r)
	}
}

// middleware that checks if a user is authenticated but doesn't error out if not
func WithPossibleAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authenticated := false

		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr == nil && apiKeyStr != "" {
			apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
			if valid {
				ctx = WithUserID(ctx, apiKey.UserID)
				ctx = WithAPIRequest(ctx, true)
				authenticated = true
				r = r.WithContext(WithAuthStatus(ctx, authenticated))
				handler(w, r)
				return
			}
		}

		if !authenticated {
			cookie, err := r.Cookie("session")
			if err == nil {
				session, exists := sm.GetSession(cookie.Value)
				if exists {
					ctx = WithUserID(ctx, session.UserID)
					authenticated = true
				}
			}
		}

		r = r.WithContext(WithAuthStatus(ctx, authenticated))
		handler(w, r)
	}
}

// middleware that only accepts API keys
func WithAPIAuth(handler http.HandlerFunc, sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)
		if apiKeyErr != nil || apiKeyStr == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "API key is required"}`))
			return
		}

		apiKey, valid := sm.apiKeyMgr.GetApiKey(apiKeyStr)
		if !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid or expired API key"}`))
			return
		}

		ctx := WithUserID(r.Context(), apiKey.UserID)
		ctx = WithAPIRequest(ctx, true)
		r = r.WithContext(ctx)

		handler(w, r)
	}
}

func (sm *SessionManager) HandleDebug(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := GetUserID(ctx)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "User ID not found in context"}`))
		return
	}

	res, err := sm.db.DebugViewUserInformation(userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`{"error": "Failed to retrieve user information: %v"}`, err)))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}

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

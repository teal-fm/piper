package oauth

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/teal-fm/piper/session"
)

// TokenReceiver interface for services that can receive OAuth tokens
type TokenReceiver interface {
	SetAccessToken(token string) int64
}

// OAuthServiceManager manages multiple OAuth services
type OAuthServiceManager struct {
	oauth2Services map[string]*OAuth2Service
	sessionManager *session.SessionManager
	mu             sync.RWMutex
}

// NewOAuthServiceManager creates a new OAuthServiceManager
func NewOAuthServiceManager() *OAuthServiceManager {
	return &OAuthServiceManager{
		oauth2Services: make(map[string]*OAuth2Service),
		sessionManager: session.NewSessionManager(),
	}
}

// RegisterOAuth2Service adds a new OAuth2 service with PKCE to the manager
func (m *OAuthServiceManager) RegisterOAuth2Service(name string, service *OAuth2Service) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oauth2Services[name] = service
}

// GetOAuth2Service retrieves an OAuth2 service by name
func (m *OAuthServiceManager) GetOAuth2Service(name string) (*OAuth2Service, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	service, exists := m.oauth2Services[name]
	return service, exists
}

// HandleLogin creates a handler for the login endpoint for a specific service
func (m *OAuthServiceManager) HandleLogin(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		oauth2Service, oauth2Exists := m.oauth2Services[serviceName]
		m.mu.RUnlock()

		if oauth2Exists {
			oauth2Service.HandleLogin(w, r)
			return
		}

		http.Error(w, fmt.Sprintf("OAuth service '%s' not found", serviceName), http.StatusNotFound)
	}
}

// HandleCallback creates a handler for the callback endpoint for a specific service
func (m *OAuthServiceManager) HandleCallback(serviceName string, tokenReceiver TokenReceiver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		oauth2Service, oauth2Exists := m.oauth2Services[serviceName]
		m.mu.RUnlock()

		var userID int64

		if oauth2Exists {
			// Handle OAuth2 with PKCE callback
			userID = oauth2Service.HandleCallback(w, r, tokenReceiver)
		} else {
			http.Error(w, fmt.Sprintf("OAuth service '%s' not found", serviceName), http.StatusNotFound)
			return
		}

		if userID > 0 {
			// Create session for the user
			session := m.sessionManager.CreateSession(userID)

			// Set session cookie
			m.sessionManager.SetSessionCookie(w, session)

			log.Printf("Created session for user %d", userID)
		}

		// Redirect to homepage
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

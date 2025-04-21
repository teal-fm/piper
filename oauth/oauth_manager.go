// Modify piper/oauth/oauth_manager.go
package oauth

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/teal-fm/piper/session"
)

// manages multiple oauth client services
type OAuthServiceManager struct {
	services       map[string]AuthService
	sessionManager *session.SessionManager
	mu             sync.RWMutex
}

func NewOAuthServiceManager(sessionManager *session.SessionManager) *OAuthServiceManager {
	return &OAuthServiceManager{
		services:       make(map[string]AuthService),
		sessionManager: sessionManager,
	}
}

// registers any service that impls AuthService
func (m *OAuthServiceManager) RegisterService(name string, service AuthService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[name] = service
	log.Printf("Registered auth service: %s", name)
}

// get an AuthService by registered name
func (m *OAuthServiceManager) GetService(name string) (AuthService, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	service, exists := m.services[name]
	return service, exists
}

func (m *OAuthServiceManager) HandleLogin(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		service, exists := m.services[serviceName]
		m.mu.RUnlock()

		if exists {
			service.HandleLogin(w, r)
			return
		}

		log.Printf("Auth service '%s' not found for login request", serviceName)
		http.Error(w, fmt.Sprintf("Auth service '%s' not found", serviceName), http.StatusNotFound)
	}
}

func (m *OAuthServiceManager) HandleCallback(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		service, exists := m.services[serviceName]
		m.mu.RUnlock()

		log.Printf("Logging in with service %s", serviceName)

		if !exists {
			log.Printf("Auth service '%s' not found for callback request", serviceName)
			http.Error(w, fmt.Sprintf("OAuth service '%s' not found", serviceName), http.StatusNotFound)
			return
		}

		userID, err := service.HandleCallback(w, r)

		if err != nil {
			log.Printf("Error handling callback for service '%s': %v", serviceName, err)
			http.Error(w, fmt.Sprintf("Error handling callback for service '%s'", serviceName), http.StatusInternalServerError)
			return
		}

		if userID > 0 {
			session := m.sessionManager.CreateSession(userID)

			m.sessionManager.SetSessionCookie(w, session)

			log.Printf("Created session for user %d via service %s", userID, serviceName)

			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			log.Printf("Callback for service '%s' did not result in a valid user ID.", serviceName)
			// todo: redirect to an error page
			// right now this just redirects home but we don't want this behaviour ideally
			http.Redirect(w, r, "/", http.StatusSeeOther)
		}
	}
}

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
	services       map[string]AuthService // Changed from *OAuth2Service to AuthService interface
	sessionManager *session.SessionManager
	mu             sync.RWMutex
}

func NewOAuthServiceManager() *OAuthServiceManager {
	return &OAuthServiceManager{
		services:       make(map[string]AuthService), // Initialize the new map
		sessionManager: session.NewSessionManager(),
	}
}

// RegisterService registers any service that implements the AuthService interface.
func (m *OAuthServiceManager) RegisterService(name string, service AuthService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[name] = service
	log.Printf("Registered auth service: %s", name)
}

// GetService retrieves a registered AuthService by name.
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
			service.HandleLogin(w, r) // Call interface method
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

		// Call the service's HandleCallback, which now returns the user ID
		userID, err := service.HandleCallback(w, r) // Call interface method

		if err != nil {
			log.Printf("Error handling callback for service '%s': %v", serviceName, err)
			http.Error(w, fmt.Sprintf("Error handling callback for service '%s'", serviceName), http.StatusInternalServerError)
			return
		}

		if userID > 0 {
			// Create session for the user
			session := m.sessionManager.CreateSession(userID)

			// Set session cookie
			m.sessionManager.SetSessionCookie(w, session)

			log.Printf("Created session for user %d via service %s", userID, serviceName)

			// Redirect to homepage after successful login and session creation
			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			log.Printf("Callback for service '%s' did not result in a valid user ID.", serviceName)
			// Optionally redirect to an error page or show an error message
			// For now, just redirecting home, but this might hide errors.
			// Consider adding error handling based on why userID might be 0.
			http.Redirect(w, r, "/", http.StatusSeeOther) // Or redirect to a login/error page
		}
	}
}

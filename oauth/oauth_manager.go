// Package oauth Modify piper/oauth/oauth_manager.go
package oauth

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

// ServiceManager OAuthServiceManager manages multiple oauth client services
type ServiceManager struct {
	services map[string]AuthService
	mu       sync.RWMutex
	logger   *log.Logger
}

func NewOAuthServiceManager() *ServiceManager {
	return &ServiceManager{
		services: make(map[string]AuthService),
		logger:   log.New(log.Writer(), "oauth: ", log.LstdFlags|log.Lmsgprefix),
	}
}

// RegisterService registers any service that impls AuthService
func (m *ServiceManager) RegisterService(name string, service AuthService) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[name] = service
	m.logger.Printf("Registered auth service: %s", name)
}

// GetService get an AuthService by registered name
func (m *ServiceManager) GetService(name string) (AuthService, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	service, exists := m.services[name]
	return service, exists
}

func (m *ServiceManager) HandleLogin(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		service, exists := m.services[serviceName]
		m.mu.RUnlock()

		if exists {
			service.HandleLogin(w, r)
			return
		}

		m.logger.Printf("Auth service '%s' not found for login request", serviceName)
		http.Error(w, fmt.Sprintf("Auth service '%s' not found", serviceName), http.StatusNotFound)
	}
}

func (m *ServiceManager) HandleLogout(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		service, exists := m.services[serviceName]
		m.mu.RUnlock()

		if exists {
			service.HandleLogout(w, r)
			return
		}

		m.logger.Printf("Auth service '%s' not found for login request", serviceName)
		http.Error(w, fmt.Sprintf("Auth service '%s' not found", serviceName), http.StatusNotFound)
	}
}

func (m *ServiceManager) HandleCallback(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		service, exists := m.services[serviceName]
		m.mu.RUnlock()

		m.logger.Printf("Logging in with service %s", serviceName)

		if !exists {
			m.logger.Printf("Auth service '%s' not found for callback request", serviceName)
			http.Error(w, fmt.Sprintf("OAuth service '%s' not found", serviceName), http.StatusNotFound)
			return
		}

		userID, err := service.HandleCallback(w, r)

		if err != nil {
			m.logger.Printf("Error handling callback for service '%s': %v", serviceName, err)
			http.Error(w, fmt.Sprintf("Error handling callback for service '%s'", serviceName), http.StatusInternalServerError)
			return
		}

		if userID > 0 {

			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			m.logger.Printf("Callback for service '%s' did not result in a valid user ID.", serviceName)
			// todo: redirect to an error page
			// right now this just redirects home but we don't want this behaviour ideally
			http.Redirect(w, r, "/", http.StatusSeeOther)
		}
	}
}

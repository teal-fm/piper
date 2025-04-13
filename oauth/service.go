// Create piper/oauth/auth_service.go
package oauth

import (
	"net/http"
)

// AuthService defines the interface for different authentication services
// that can be managed by the OAuthServiceManager.
type AuthService interface {
	// HandleLogin initiates the login flow for the specific service.
	HandleLogin(w http.ResponseWriter, r *http.Request)
	// HandleCallback handles the callback from the authentication provider,
	// processes the response (e.g., exchanges code for token), finds or creates
	// the user in the local system, and returns the user ID.
	// Returns 0 if authentication failed or user could not be determined.
	HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error)
}

type TokenReceiver interface {
	// SetAccessToken stores the access token for the user and returns the user ID.
	// If the user is already logged in, the current ID is provided.
	SetAccessToken(token string, currentId int64, hasSession bool) (int64, error)
}

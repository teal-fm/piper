package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

// OAuth2Service represents an OAuth2 service with PKCE support
type OAuth2Service struct {
	config        oauth2.Config
	state         string
	codeVerifier  string
	codeChallenge string
}

// generateRandomState creates a random state string for CSRF protection
func generateRandomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// NewOAuth2Service creates a new OAuth2Service with PKCE support
func NewOAuth2Service(clientID, clientSecret, redirectURI string, scopes []string, provider string) *OAuth2Service {
	var endpoint oauth2.Endpoint

	// Select the appropriate provider endpoint
	switch strings.ToLower(provider) {
	case "spotify":
		endpoint = spotify.Endpoint
	default:
		// Use custom endpoints if not a predefined provider
		endpoint = oauth2.Endpoint{
			AuthURL:  "https://example.com/auth",
			TokenURL: "https://example.com/token",
		}
	}

	// Create code verifier and challenge for PKCE
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	return &OAuth2Service{
		config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURI,
			Scopes:       scopes,
			Endpoint:     endpoint,
		},
		state:         generateRandomState(),
		codeVerifier:  codeVerifier,
		codeChallenge: codeChallenge,
	}
}

// generateCodeVerifier creates a random code verifier for PKCE
func generateCodeVerifier() string {
	// Generate a random string of 32-96 bytes as per RFC 7636
	b := make([]byte, 64) // Using 64 bytes (512 bits)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// generateCodeChallenge creates a code challenge from the code verifier using S256 method
func generateCodeChallenge(verifier string) string {
	// S256 method: SHA256 hash of the code verifier
	h := sha256.New()
	h.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// HandleLogin redirects the user to the authorization page with PKCE
func (o *OAuth2Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Set up authorization options with PKCE
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", o.codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}

	// Redirect to authorization page
	authURL := o.config.AuthCodeURL(o.state, opts...)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// HandleCallback processes the callback from the OAuth provider using PKCE
func (o *OAuth2Service) HandleCallback(w http.ResponseWriter, r *http.Request, tokenReceiver TokenReceiver) int64 {
	// Verify state
	state := r.URL.Query().Get("state")
	if state != o.state {
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return 0
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No code provided", http.StatusBadRequest)
		return 0
	}

	// Exchange code for access token using PKCE
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", o.codeVerifier),
	}

	token, err := o.config.Exchange(context.Background(), code, opts...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error exchanging code for token: %v", err), http.StatusInternalServerError)
		return 0
	}

	// Store access token
	userID := tokenReceiver.SetAccessToken(token.AccessToken)

	return userID
}

// GetToken returns the OAuth2 token using the authorization code
func (o *OAuth2Service) GetToken(code string) (*oauth2.Token, error) {
	// Exchange code for token using PKCE
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", o.codeVerifier),
	}

	return o.config.Exchange(context.Background(), code, opts...)
}

// GetClient returns an authenticated HTTP client
func (o *OAuth2Service) GetClient(token *oauth2.Token) *http.Client {
	return o.config.Client(context.Background(), token)
}

// RefreshToken refreshes an OAuth2 token
func (o *OAuth2Service) RefreshToken(token *oauth2.Token) (*oauth2.Token, error) {
	source := o.config.TokenSource(context.Background(), token)
	return oauth2.ReuseTokenSource(token, source).Token()
}

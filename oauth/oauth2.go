// Modify piper/oauth/oauth2.go
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/teal-fm/piper/session"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

type OAuth2Service struct {
	config        oauth2.Config
	state         string
	codeVerifier  string
	codeChallenge string
	// Added TokenReceiver field to handle user lookup/creation based on token
	tokenReceiver TokenReceiver
}

func GenerateRandomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func NewOAuth2Service(clientID, clientSecret, redirectURI string, scopes []string, provider string, tokenReceiver TokenReceiver) *OAuth2Service {
	var endpoint oauth2.Endpoint

	switch strings.ToLower(provider) {
	case "spotify":
		endpoint = spotify.Endpoint
	// Add other providers like Last.fm here
	default:
		// Placeholder for unconfigured providers
		log.Printf("Warning: OAuth2 provider '%s' not explicitly configured. Using placeholder endpoints.", provider)
		endpoint = oauth2.Endpoint{
			AuthURL:  "https://example.com/auth", // Replace with actual endpoints if needed
			TokenURL: "https://example.com/token",
		}
	}

	codeVerifier := GenerateCodeVerifier()
	codeChallenge := GenerateCodeChallenge(codeVerifier)

	return &OAuth2Service{
		config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURI,
			Scopes:       scopes,
			Endpoint:     endpoint,
		},
		state:         GenerateRandomState(),
		codeVerifier:  codeVerifier,
		codeChallenge: codeChallenge,
		tokenReceiver: tokenReceiver, // Store the token receiver
	}
}

// generateCodeVerifier creates a random code verifier for PKCE
func GenerateCodeVerifier() string {
	b := make([]byte, 64)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// generateCodeChallenge creates a code challenge from the code verifier using S256 method
func GenerateCodeChallenge(verifier string) string {
	h := sha256.New()
	h.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// HandleLogin implements the AuthService interface method.
func (o *OAuth2Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", o.codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	authURL := o.config.AuthCodeURL(o.state, opts...)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (o *OAuth2Service) HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error) {
	state := r.URL.Query().Get("state")
	if state != o.state {
		log.Printf("OAuth2 Callback Error: State mismatch. Expected '%s', got '%s'", o.state, state)
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return 0, errors.New("state mismatch")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		errMsg := r.URL.Query().Get("error")
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("OAuth2 Callback Error: No code provided. Error: '%s', Description: '%s'", errMsg, errDesc)
		http.Error(w, fmt.Sprintf("Authorization failed: %s (%s)", errMsg, errDesc), http.StatusBadRequest)
		return 0, errors.New("no code provided")
	}

	if o.tokenReceiver == nil {
		log.Printf("OAuth2 Callback Error: TokenReceiver is not configured for this service.")
		http.Error(w, "Internal server configuration error", http.StatusInternalServerError)
		return 0, errors.New("token receiver not configured")
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", o.codeVerifier),
	}

	log.Println(code)

	token, err := o.config.Exchange(context.Background(), code, opts...)
	if err != nil {
		log.Printf("OAuth2 Callback Error: Failed to exchange code for token: %v", err)
		http.Error(w, fmt.Sprintf("Error exchanging code for token: %v", err), http.StatusInternalServerError)
		return 0, errors.New("failed to exchange code for token")
	}

	userId, hasSession := session.GetUserID(r.Context())

	// Use the token receiver to store the token and get the user ID
	userID, err := o.tokenReceiver.SetAccessToken(token.AccessToken, userId, hasSession)
	if err != nil {
		log.Printf("OAuth2 Callback Info: TokenReceiver did not return a valid user ID for token: %s...", token.AccessToken[:min(10, len(token.AccessToken))])
	}

	log.Printf("OAuth2 Callback Success: Exchanged code for token, UserID: %d", userID)
	return userID, nil
}

// GetToken remains unchanged
func (o *OAuth2Service) GetToken(code string) (*oauth2.Token, error) {
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", o.codeVerifier),
	}
	return o.config.Exchange(context.Background(), code, opts...)
}

// GetClient remains unchanged
func (o *OAuth2Service) GetClient(token *oauth2.Token) *http.Client {
	return o.config.Client(context.Background(), token)
}

// RefreshToken remains unchanged
func (o *OAuth2Service) RefreshToken(token *oauth2.Token) (*oauth2.Token, error) {
	source := o.config.TokenSource(context.Background(), token)
	return oauth2.ReuseTokenSource(token, source).Token()
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

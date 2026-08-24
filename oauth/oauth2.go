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
	"sync"
	"time"

	"golang.org/x/oauth2"
)

type userIDGetter func(context.Context) int64

// Service implements the PKCE authorization-code flow with single-use CSRF
// states. It is safe for concurrent use.
type Service struct {
	config        oauth2.Config
	tokenReceiver TokenReceiver
	store         *memoryStateStore
	logger        *log.Logger
	getUserID     userIDGetter
}

type stateEntry struct {
	verifier  string
	expiresAt time.Time
}

type memoryStateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{
		entries: make(map[string]stateEntry),
	}
}

func (s *memoryStateStore) Set(state, verifier string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, v := range s.entries {
		if now.After(v.expiresAt) {
			delete(s.entries, k)
		}
	}

	s.entries[state] = stateEntry{verifier: verifier, expiresAt: now.Add(ttl)}
}

func (s *memoryStateStore) GetAndDelete(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[state]
	if !ok {
		return "", false
	}

	delete(s.entries, state)
	if time.Now().After(e.expiresAt) {
		return "", false
	}

	return e.verifier, true
}

const randAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

func randText(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read: %v", err))
	}

	for i := range b {
		b[i] = randAlphabet[int(b[i])%len(randAlphabet)]
	}

	return string(b)
}

func NewOAuth2Service(cfg oauth2.Config, tokenReceiver TokenReceiver, logger *log.Logger, getUserID userIDGetter) *Service {
	return &Service{
		config:        cfg,
		tokenReceiver: tokenReceiver,
		store:         newMemoryStateStore(),
		logger:        logger,
		getUserID:     getUserID,
	}
}

func generateCodeChallenge(verifier string) string {
	h := sha256.New()
	h.Write([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// HandleLogin redirects to the provider's authorization endpoint with a
// single-use state and S256 PKCE challenge.
func (o *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := randText(26)
	verifier := randText(64)
	challenge := generateCodeChallenge(verifier)

	o.store.Set(state, verifier, 10*time.Minute)

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}

	authURL := o.config.AuthCodeURL(state, opts...)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (o *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// TODO not implemented yet. not sure what the api call is for this package
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type completeLoginParams struct {
	State         string
	Code          string
	ProviderError string
	ProviderDesc  string
	UserID        int64
}

// HandleCallback handles the provider redirect. It requires query parameters
// state and code (or error/error_description on provider denial).
//
// On success it returns the authenticated user ID. On failure it writes a
// generic error (never reflecting provider strings) with 400 for client
// errors and 500 for server errors and returns a sentinel for errors.Is.
func (o *Service) HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error) {
	params := completeLoginParams{
		State:         r.URL.Query().Get("state"),
		Code:          r.URL.Query().Get("code"),
		ProviderError: r.URL.Query().Get("error"),
		ProviderDesc:  r.URL.Query().Get("error_description"),
		UserID:        o.getUserID(r.Context()),
	}

	userID, err := o.completeLogin(r.Context(), params)
	if err != nil {
		status := httpStatusForOAuthError(err)
		http.Error(w, err.Error(), status)
		return 0, err
	}

	return userID, nil
}

var (
	errStateMismatch  = errors.New("state mismatch")
	errNoCode         = errors.New("no code provided")
	errNoReceiver     = errors.New("token receiver not configured")
	errExchangeFailed = errors.New("failed to exchange code for token")
)

func httpStatusForOAuthError(err error) int {
	if errors.Is(err, errNoReceiver) || errors.Is(err, errExchangeFailed) {
		return http.StatusInternalServerError
	}

	return http.StatusBadRequest
}

// State is consumed before any other check so a missing code still
// invalidates the entry and provider errors are only logged for a valid
// state, preventing CSRF bypass via forged error.
func (o *Service) completeLogin(ctx context.Context, p completeLoginParams) (int64, error) {
	verifier, ok := o.store.GetAndDelete(p.State)
	if !ok {
		o.logger.Printf("OAuth2 Callback Error: State mismatch or expired. Got '%s'", p.State)
		return 0, errStateMismatch
	}

	if p.Code == "" {
		if p.ProviderError != "" || p.ProviderDesc != "" {
			o.logger.Printf("OAuth2 Callback Error: provider returned error for state '%s': error=%q desc=%q", p.State, p.ProviderError, p.ProviderDesc)
		} else {
			o.logger.Printf("OAuth2 Callback Error: No code provided for state '%s'", p.State)
		}
		return 0, errNoCode
	}

	if o.tokenReceiver == nil {
		o.logger.Printf("OAuth2 Callback Error: TokenReceiver is not configured")
		return 0, errNoReceiver
	}

	return o.exchangeAndStore(ctx, p.Code, verifier, p.UserID)
}

func (o *Service) exchangeAndStore(ctx context.Context, code, verifier string, uid int64) (int64, error) {
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", verifier),
	}

	token, err := o.config.Exchange(ctx, code, opts...)
	if err != nil {
		o.logger.Printf("OAuth2 Callback Error: Failed to exchange code for token: %v", err)
		return 0, errExchangeFailed
	}

	userID, err := o.tokenReceiver.SetAccessToken(token.AccessToken, token.RefreshToken, uid)
	if err != nil {
		o.logger.Printf(
			"OAuth2 Callback Info: TokenReceiver failed for token %q...: %v",
			token.AccessToken[:min(len(token.AccessToken), 10)],
			err,
		)
	}

	o.logger.Printf("OAuth2 Callback Success: Exchanged code for token, UserID: %d", userID)

	return userID, nil
}

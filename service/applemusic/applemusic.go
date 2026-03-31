package applemusic

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/service/arbiter"
	atprotoservice "github.com/teal-fm/piper/service/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
)

type liveTrackState struct {
	track      *models.Track
	currentURL string
	observedAt time.Time
}

type Service struct {
	teamID         string
	keyID          string
	privateKeyPath string

	mu           sync.RWMutex
	cachedToken  string
	cachedExpiry time.Time

	// optional DB-backed persistence
	getToken  func() (string, time.Time, bool, error)
	saveToken func(string, time.Time) error

	// ingestion deps
	DB                *db.DB
	atprotoService    *atprotoauth.AuthService
	mbService         *musicbrainz.Service
	playingNowService interface {
		PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
		ClearPlayingNow(ctx context.Context, userID int64) error
	}
	httpClient *http.Client
	logger     *log.Logger

	liveStates    map[int64]*liveTrackState
	lastStamped   map[int64]*models.Track
	nowPlayingTTL time.Duration
	clock         func() time.Time
	arbiter       *arbiter.Coordinator
}

func NewService(teamID, keyID, privateKeyPath string) *Service {
	return &Service{
		teamID:         teamID,
		keyID:          keyID,
		privateKeyPath: privateKeyPath,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		logger:         log.New(os.Stdout, "applemusic: ", log.LstdFlags|log.Lmsgprefix),
		liveStates:     make(map[int64]*liveTrackState),
		lastStamped:    make(map[int64]*models.Track),
		nowPlayingTTL:  time.Duration(viper.GetInt("applemusic.now_playing_ttl_seconds")) * time.Second,
		clock:          time.Now,
	}
}

// WithPersistence wires DB-backed getters/setters for token caching
func (s *Service) WithPersistence(
	get func() (string, time.Time, bool, error),
	save func(string, time.Time) error,
) *Service {
	s.getToken = get
	s.saveToken = save
	return s
}

// WithDeps wires services needed for ingestion
func (s *Service) WithDeps(database *db.DB, atproto *atprotoauth.AuthService, mb *musicbrainz.Service, playingNowService interface {
	PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
	ClearPlayingNow(ctx context.Context, userID int64) error
}) *Service {
	s.DB = database
	s.atprotoService = atproto
	s.mbService = mb
	s.playingNowService = playingNowService
	return s
}

func (s *Service) SetCoordinator(coordinator *arbiter.Coordinator) {
	s.arbiter = coordinator
}

func (s *Service) GetLiveTrack(userID int64) (*models.Track, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.liveStates[userID]
	if state == nil || state.track == nil {
		return nil, false
	}

	if s.nowPlayingTTL <= 0 {
		return nil, false
	}
	if s.now().Sub(state.observedAt) > s.nowPlayingTTL {
		return nil, false
	}

	return cloneTrack(state.track), true
}

func (s *Service) GetLastStampedTrack(userID int64) *models.Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneTrack(s.lastStamped[userID])
}

func (s *Service) HandleDeveloperToken(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	token, exp, err := s.GenerateDeveloperTokenWithForce(force)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to generate token: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(fmt.Sprintf(`{"token":"%s","expiresAt":"%s"}`, token, exp.UTC().Format(time.RFC3339))))
	if err != nil {
		s.logger.Printf("failed to write response: %v", err)
	}
}

// GenerateDeveloperTokenWithForce allows bypassing caches when force is true.
func (s *Service) GenerateDeveloperTokenWithForce(force bool) (string, time.Time, error) {
	if !force {
		return s.GenerateDeveloperToken()
	}

	// Bypass caches and regenerate
	privKey, err := s.loadPrivateKey()
	if err != nil {
		return "", time.Time{}, err
	}

	if s.keyID == "" {
		return "", time.Time{}, errors.New("applemusic key_id is not configured")
	}

	now := time.Now().UTC()
	exp := now.Add(180 * 24 * time.Hour).Add(-1 * time.Hour)

	builder := jwt.NewBuilder().
		Issuer(s.teamID).
		IssuedAt(now).
		Expiration(exp)

	unsignedToken, err := builder.Build()
	if err != nil {
		return "", time.Time{}, err
	}

	headers := jws.NewHeaders()
	_ = headers.Set(jws.KeyIDKey, s.keyID)
	signed, err := jwt.Sign(unsignedToken, jwt.WithKey(jwa.ES256(), privKey, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", time.Time{}, err
	}

	final := string(signed)

	s.mu.Lock()
	s.cachedToken = final
	s.cachedExpiry = exp
	s.mu.Unlock()

	if s.saveToken != nil {
		_ = s.saveToken(final, exp)
	}

	return final, exp, nil
}

// GenerateDeveloperToken returns a cached valid token or creates a new one.
func (s *Service) GenerateDeveloperToken() (string, time.Time, error) {
	if s.keyID == "" {
		return "", time.Time{}, errors.New("applemusic key_id is not configured")
	}
	s.mu.RLock()
	if s.cachedToken != "" && time.Until(s.cachedExpiry) > 5*time.Minute {
		token := s.cachedToken
		exp := s.cachedExpiry
		s.mu.RUnlock()
		// Validate cached token claims (aud, iss) to avoid serving bad tokens
		if s.isTokenStructurallyValid(token) {
			return token, exp, nil
		}
	} else {
		s.mu.RUnlock()
	}

	// Try DB cache if available
	if s.getToken != nil {
		if t, e, ok, err := s.getToken(); err == nil && ok {
			if time.Until(e) > 5*time.Minute && s.isTokenStructurallyValid(t) {
				s.mu.Lock()
				s.cachedToken = t
				s.cachedExpiry = e
				s.mu.Unlock()
				return t, e, nil
			}
		}
	}

	privKey, err := s.loadPrivateKey()
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now().UTC()
	// Apple allows up to 6 months validity; choose 6 months minus a small buffer
	exp := now.Add(180 * 24 * time.Hour).Add(-1 * time.Hour)

	builder := jwt.NewBuilder().
		Issuer(s.teamID).
		IssuedAt(now).
		Expiration(exp)

	unsignedToken, err := builder.Build()
	if err != nil {
		return "", time.Time{}, err
	}

	headers := jws.NewHeaders()
	_ = headers.Set(jws.KeyIDKey, s.keyID)
	signed, err := jwt.Sign(unsignedToken, jwt.WithKey(jwa.ES256(), privKey, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", time.Time{}, err
	}

	final := string(signed)

	s.mu.Lock()
	s.cachedToken = final
	s.cachedExpiry = exp
	s.mu.Unlock()

	if s.saveToken != nil {
		_ = s.saveToken(final, exp)
	}

	return final, exp, nil
}

// isTokenStructurallyValid parses without verification and checks claims for iss and exp
func (s *Service) isTokenStructurallyValid(token string) bool {
	if token == "" {
		return false
	}
	parsed, err := jwt.Parse([]byte(token), jwt.WithVerify(false))
	if err != nil {
		return false
	}
	// Check issuer
	issuer, _ := parsed.Issuer()
	if issuer != s.teamID {
		return false
	}
	// Check expiration not too close
	expiration, _ := parsed.Expiration()
	if time.Until(expiration) <= 5*time.Minute {
		return false
	}
	return true
}

func (s *Service) loadPrivateKey() (*ecdsa.PrivateKey, error) {
	if s.privateKeyPath == "" {
		return nil, errors.New("applemusic private key path not configured")
	}
	pemBytes, err := os.ReadFile(s.privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || len(block.Bytes) == 0 {
		return nil, errors.New("invalid PEM data for private key")
	}
	pkcs8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKCS#8 key: %w", err)
	}
	key, ok := pkcs8.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not ECDSA")
	}
	return key, nil
}

// ------- Recent Played Tracks ingestion -------

// AppleRecentTrack models a subset of Apple Music API track response
type AppleRecentTrack struct {
	ID         string `json:"id"`
	Attributes struct {
		Name             string  `json:"name"`
		ArtistName       string  `json:"artistName"`
		AlbumName        string  `json:"albumName"`
		DurationInMillis *int64  `json:"durationInMillis"`
		Isrc             *string `json:"isrc"`
		URL              string  `json:"url"`
		PlayParams       *struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"playParams"`
	} `json:"attributes"`
}

// Generates a hash representing the track name, album name, and artist name,
// to be used for comparing subsequent uploaded Apple Music tracks
func generateUploadHash(track *AppleRecentTrack) string {
	input := track.Attributes.Name + track.Attributes.AlbumName + track.Attributes.ArtistName
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("am_uploaded_%x", hash)
}

type recentPlayedResponse struct {
	Data []AppleRecentTrack `json:"data"`
}

// FetchRecentPlayedTracks calls Apple Music API for a user token
func (s *Service) FetchRecentPlayedTracks(ctx context.Context, userToken string, limit int) ([]AppleRecentTrack, error) {
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	devToken, _, err := s.GenerateDeveloperToken()
	if err != nil {
		return nil, err
	}
	endpoint := &url.URL{Scheme: "https", Host: "api.music.apple.com", Path: "/v1/me/recent/played/tracks"}
	q := endpoint.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	req.Header.Set("Music-User-Token", userToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			s.logger.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)

	// Read the full response body to log it
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple music api error: %s", resp.Status)
	}

	var parsed recentPlayedResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, err
	}
	return parsed.Data, nil
}

// toTrack converts AppleRecentTrack to internal models.Track
func (s *Service) toTrack(t AppleRecentTrack) *models.Track {
	var duration int64
	if t.Attributes.DurationInMillis != nil {
		duration = *t.Attributes.DurationInMillis
	}
	isrc := ""
	if t.Attributes.Isrc != nil {
		isrc = *t.Attributes.Isrc
	}

	// Similar stamping logic to Spotify: stamp if played more than half (or 30 seconds whichever is greater)
	// Since Apple Music recent played tracks don't provide play progress, we assume full plays
	isStamped := duration > 30000 && duration >= duration/2

	track := &models.Track{
		Name:           t.Attributes.Name,
		Artist:         []models.Artist{{Name: t.Attributes.ArtistName}},
		Album:          t.Attributes.AlbumName,
		URL:            t.Attributes.URL,
		DurationMs:     duration,
		ProgressMs:     duration, // Assume full play since Apple Music doesn't provide partial plays
		ServiceBaseUrl: "music.apple.com",
		ISRC:           isrc,
		HasStamped:     isStamped,
		Timestamp:      time.Now().UTC(),
	}

	// If an Apple Music track has no URL, it's an uploaded track; generate an uploadHash so that the
	// track can be distinguished from other uploaded tracks
	if track.URL == "" {
		track.URL = generateUploadHash(&t)
	}

	if s.mbService != nil {
		hydrated, err := musicbrainz.HydrateTrack(s.mbService, *track)
		if err == nil && hydrated != nil {
			track = hydrated
		}
	}
	return track
}

// GetCurrentAppleMusicTrack fetches the most recent Apple Music track for a user
func (s *Service) GetCurrentAppleMusicTrack(ctx context.Context, user *models.User) (*AppleRecentTrack, error) {
	if user.AppleMusicUserToken == nil || *user.AppleMusicUserToken == "" {
		return nil, nil
	}

	// Only fetch the most recent track (limit=1)
	items, err := s.FetchRecentPlayedTracks(ctx, *user.AppleMusicUserToken, 1)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	return &items[0], nil
}

// ProcessUser checks for new Apple Music tracks and processes them
func (s *Service) ProcessUser(ctx context.Context, user *models.User) error {
	if user.AppleMusicUserToken == nil || *user.AppleMusicUserToken == "" {
		return nil
	}

	// Fetch only the most recent track
	currentAppleTrack, err := s.GetCurrentAppleMusicTrack(ctx, user)
	if err != nil {
		s.logger.Printf("failed to get current Apple Music track for user %d: %v", user.ID, err)
		return err
	}

	if currentAppleTrack == nil {
		s.logger.Printf("no current Apple Music track for user %d", user.ID)
		s.mu.Lock()
		delete(s.liveStates, user.ID)
		s.mu.Unlock()
		if s.playingNowService != nil {
			allowed := true
			if s.arbiter != nil {
				allowed, err = s.arbiter.CanClearNowPlaying(user.ID, models.ServiceAppleMusic)
				if err != nil {
					s.logger.Printf("Error checking Apple Music clear ownership for user %d: %v", user.ID, err)
					allowed = false
				}
			}
			if allowed {
				if err := s.playingNowService.ClearPlayingNow(ctx, user.ID); err != nil {
					s.logger.Printf("Error clearing playing now for user %d: %v", user.ID, err)
				}
			}
		}
		return nil
	}

	currentURL := currentAppleTrack.Attributes.URL
	if currentURL == "" {
		currentURL = generateUploadHash(currentAppleTrack)
	}

	s.mu.RLock()
	existingState := s.liveStates[user.ID]
	s.mu.RUnlock()

	isNewObservation := existingState == nil || existingState.currentURL != currentURL
	var track *models.Track
	if !isNewObservation && existingState != nil {
		track = cloneTrack(existingState.track)
	} else {
		track = s.toTrack(*currentAppleTrack)
	}
	if track == nil || strings.TrimSpace(track.Name) == "" || len(track.Artist) == 0 {
		s.logger.Printf("invalid track data for user %d", user.ID)
		return nil
	}

	if isNewObservation {
		s.mu.Lock()
		if s.liveStates == nil {
			s.liveStates = make(map[int64]*liveTrackState)
		}
		s.liveStates[user.ID] = &liveTrackState{
			track:      cloneTrack(track),
			currentURL: currentURL,
			observedAt: s.now().UTC(),
		}
		s.mu.Unlock()
	}

	if isNewObservation && s.playingNowService != nil {
		allowed := true
		if s.arbiter != nil {
			allowed, err = s.arbiter.CanPublishNowPlaying(user.ID, models.ServiceAppleMusic, track)
			if err != nil {
				s.logger.Printf("Error checking Apple Music publish ownership for user %d: %v", user.ID, err)
				allowed = false
			}
		}
		if allowed {
			if err := s.playingNowService.PublishPlayingNow(ctx, user.ID, track); err != nil {
				s.logger.Printf("Error publishing playing now for user %d: %v", user.ID, err)
			}
		}
	}

	if !isNewObservation {
		s.logger.Printf("track unchanged for user %d: %s by %s", user.ID, track.Name, track.Artist[0].Name)
		return nil
	}

	allowed := true
	if s.arbiter != nil {
		allowed, err = s.arbiter.CanScrobble(user.ID, models.ServiceAppleMusic, track)
		if err != nil {
			s.logger.Printf("Error checking Apple Music scrobble ownership for user %d: %v", user.ID, err)
			allowed = false
		}
	}
	if !allowed {
		s.logger.Printf("Skipping Apple Music scrobble for user %d because a higher priority service owns the session", user.ID)
		return nil
	}

	lastTracks, err := s.DB.GetRecentTracks(user.ID, 1)
	if err != nil {
		s.logger.Printf("failed to get last tracks for user %d: %v", user.ID, err)
	}
	if len(lastTracks) > 0 && lastTracks[0].URL == currentURL {
		s.logger.Printf("skipping duplicate Apple Music track for user %d: %s by %s", user.ID, track.Name, track.Artist[0].Name)
		return nil
	}

	if _, err := s.DB.SaveTrack(user.ID, track); err != nil {
		s.logger.Printf("failed saving apple track for user %d: %v", user.ID, err)
		return err
	}

	s.mu.Lock()
	if s.lastStamped == nil {
		s.lastStamped = make(map[int64]*models.Track)
	}
	s.lastStamped[user.ID] = cloneTrack(track)
	s.mu.Unlock()

	s.logger.Printf("saved new track for user %d: %s by %s", user.ID, track.Name, track.Artist[0].Name)

	if user.ATProtoDID != nil && user.MostRecentAtProtoSessionID != nil && s.atprotoService != nil {
		if err := atprotoservice.SubmitPlayToPDS(ctx, *user.ATProtoDID, *user.MostRecentAtProtoSessionID, track, s.atprotoService); err != nil {
			s.logger.Printf("failed submit to PDS for user %d: %v", user.ID, err)
		}
	}

	return nil
}

func cloneTrack(track *models.Track) *models.Track {
	if track == nil {
		return nil
	}
	cloned := *track
	if len(track.Artist) > 0 {
		cloned.Artist = append([]models.Artist(nil), track.Artist...)
	}
	return &cloned
}

func (s *Service) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// StartListeningTracker periodically fetches recent plays for Apple Music linked users
func (s *Service) StartListeningTracker(interval time.Duration) {
	if s.DB == nil {
		if s.logger != nil {
			s.logger.Printf("DB not configured; Apple Music tracker disabled")
		}
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		s.runOnce(context.Background())
		for range ticker.C {
			s.runOnce(context.Background())
		}
	}()
}

func (s *Service) runOnce(ctx context.Context) {
	users, err := s.DB.GetAllAppleMusicLinkedUsers()
	if err != nil {
		s.logger.Printf("error loading Apple Music users: %v", err)
		return
	}
	for _, u := range users {
		if ctx.Err() != nil {
			return
		}
		if err := s.ProcessUser(ctx, u); err != nil {
			s.logger.Printf("error processing user %d: %v", u.ID, err)
		}
	}
}

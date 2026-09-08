package listenbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	atprotoservice "github.com/teal-fm/piper/service/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
	"golang.org/x/time/rate"
)

const (
	defaultAPIURL    = "https://api.listenbrainz.org"
	initialSyncLimit = 25
	updateSyncLimit  = 1000
)

type playingNowPublisher interface {
	PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
	ClearPlayingNow(ctx context.Context, userID int64) error
}

type Service struct {
	db                 *db.DB
	httpClient         *http.Client
	limiter            *rate.Limiter
	apiURL             string
	userAgent          string
	musicBrainzService *musicbrainz.Service
	atprotoService     *atprotoauth.AuthService
	playingNowService  playingNowPublisher

	mu                 sync.Mutex
	lastSeenNowPlaying map[int64]string
	logger             *log.Logger
}

type listensResponse struct {
	Payload struct {
		Count      int                          `json:"count"`
		UserID     string                       `json:"user_id"`
		PlayingNow bool                         `json:"playing_now"`
		Listens    []models.ListenBrainzPayload `json:"listens"`
	} `json:"payload"`
}

type tokenValidationResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Valid    bool   `json:"valid"`
	UserName string `json:"user_name"`
}

func NewService(database *db.DB, apiURL, userAgent string, musicBrainzService *musicbrainz.Service, atprotoService *atprotoauth.AuthService, playingNowService playingNowPublisher) *Service {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = models.SubmissionAgent + " (https://teal.fm)"
	}

	return &Service{
		db: database,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		// ListenBrainz asks unauthenticated and authenticated clients to stay at
		// or below one API request per second.
		limiter:            rate.NewLimiter(rate.Every(time.Second), 1),
		apiURL:             apiURL,
		userAgent:          userAgent,
		musicBrainzService: musicBrainzService,
		atprotoService:     atprotoService,
		playingNowService:  playingNowService,
		lastSeenNowPlaying: make(map[int64]string),
		logger:             log.New(os.Stdout, "listenbrainz: ", log.LstdFlags|log.Lmsgprefix),
	}
}

// ValidateToken verifies a ListenBrainz token and returns its MusicBrainz ID.
func (s *Service) ValidateToken(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("token cannot be empty")
	}

	var validation tokenValidationResponse
	if err := s.getJSON(ctx, "/1/validate-token", token, nil, &validation); err != nil {
		return "", fmt.Errorf("validating ListenBrainz token: %w", err)
	}
	if !validation.Valid || validation.UserName == "" {
		return "", errors.New("ListenBrainz token is invalid")
	}
	return validation.UserName, nil
}

// StartListeningTracker polls all linked accounts until the process exits.
func (s *Service) StartListeningTracker(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	s.logger.Printf("ListenBrainz listening tracker started with interval %v", interval)
	s.syncAllUsers(context.Background())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.syncAllUsers(context.Background())
	}
}

func (s *Service) syncAllUsers(ctx context.Context) {
	users, err := s.db.GetAllUsersWithListenBrainz()
	if err != nil {
		s.logger.Printf("Could not load linked users: %v", err)
		return
	}

	linked := make(map[int64]struct{}, len(users))
	for _, user := range users {
		linked[user.ID] = struct{}{}
		if err := s.SyncUser(ctx, user); err != nil {
			s.logger.Printf("Could not sync user %d: %v", user.ID, err)
		}
	}

	s.mu.Lock()
	for userID := range s.lastSeenNowPlaying {
		if _, ok := linked[userID]; !ok {
			delete(s.lastSeenNowPlaying, userID)
		}
	}
	s.mu.Unlock()
}

// SyncUser fetches the account's current listen and all new completed listens.
func (s *Service) SyncUser(ctx context.Context, user *models.User) error {
	if user == nil || user.ListenBrainzUsername == nil || user.ListenBrainzToken == nil {
		return errors.New("user does not have a linked ListenBrainz account")
	}

	var syncErrors []error
	if err := s.syncPlayingNow(ctx, user); err != nil {
		syncErrors = append(syncErrors, err)
	}
	if err := s.syncListens(ctx, user); err != nil {
		syncErrors = append(syncErrors, err)
	}
	return errors.Join(syncErrors...)
}

// UnloadUser drops transient state after an account is unlinked.
func (s *Service) UnloadUser(userID int64) {
	s.mu.Lock()
	delete(s.lastSeenNowPlaying, userID)
	s.mu.Unlock()
}

func (s *Service) syncPlayingNow(ctx context.Context, user *models.User) error {
	path := "/1/user/" + url.PathEscape(*user.ListenBrainzUsername) + "/playing-now"
	var response listensResponse
	if err := s.getJSON(ctx, path, *user.ListenBrainzToken, nil, &response); err != nil {
		return fmt.Errorf("fetching playing-now listen for %s: %w", *user.ListenBrainzUsername, err)
	}

	if len(response.Payload.Listens) == 0 {
		s.mu.Lock()
		_, published := s.lastSeenNowPlaying[user.ID]
		delete(s.lastSeenNowPlaying, user.ID)
		s.mu.Unlock()
		if published && s.playingNowService != nil {
			if err := s.playingNowService.ClearPlayingNow(ctx, user.ID); err != nil {
				return fmt.Errorf("clearing playing-now listen for %s: %w", *user.ListenBrainzUsername, err)
			}
		}
		return nil
	}

	track := response.Payload.Listens[0].ConvertToTrack()
	track.HasStamped = false
	signature := nowPlayingSignature(&track)

	s.mu.Lock()
	unchanged := s.lastSeenNowPlaying[user.ID] == signature
	if !unchanged {
		s.lastSeenNowPlaying[user.ID] = signature
	}
	s.mu.Unlock()
	if unchanged || s.playingNowService == nil {
		return nil
	}
	if err := s.playingNowService.PublishPlayingNow(ctx, user.ID, &track); err != nil {
		s.mu.Lock()
		delete(s.lastSeenNowPlaying, user.ID)
		s.mu.Unlock()
		return fmt.Errorf("publishing playing-now listen for %s: %w", *user.ListenBrainzUsername, err)
	}
	return nil
}

func (s *Service) syncListens(ctx context.Context, user *models.User) error {
	lastKnown := user.ListenBrainzSyncedAt

	query := url.Values{}
	if lastKnown == nil {
		query.Set("count", strconv.Itoa(initialSyncLimit))
	} else {
		query.Set("count", strconv.Itoa(updateSyncLimit))
		// min_ts is exclusive. Moving the watermark back one second lets us
		// collect another listen that shares the latest stored timestamp.
		query.Set("min_ts", strconv.FormatInt(lastKnown.Unix()-1, 10))
	}

	path := "/1/user/" + url.PathEscape(*user.ListenBrainzUsername) + "/listens"
	var response listensResponse
	if err := s.getJSON(ctx, path, *user.ListenBrainzToken, query, &response); err != nil {
		return fmt.Errorf("fetching listens for %s: %w", *user.ListenBrainzUsername, err)
	}

	listens := response.Payload.Listens
	sort.SliceStable(listens, func(i, j int) bool {
		if listens[i].ListenedAt == nil {
			return false
		}
		if listens[j].ListenedAt == nil {
			return true
		}
		return *listens[i].ListenedAt < *listens[j].ListenedAt
	})

	for i := range listens {
		listen := &listens[i]
		if listen.ListenedAt == nil || listen.TrackMetadata.TrackName == "" || listen.TrackMetadata.ArtistName == "" {
			continue
		}

		track := listen.ConvertToTrack()
		exists, err := s.db.HasTrackListen(user.ID, db.SourceListenBrainz, track.Name, track.Timestamp)
		if err != nil {
			return err
		}
		if exists {
			if err := s.db.SaveListenBrainzSyncTimestamp(user.ID, track.Timestamp); err != nil {
				return err
			}
			advanceUserCursor(user, track.Timestamp)
			continue
		}

		if track.RecordingMBID == nil && s.musicBrainzService != nil {
			hydrated, err := musicbrainz.HydrateTrack(s.musicBrainzService, track)
			if err != nil {
				s.logger.Printf("Could not hydrate %s by %s: %v", track.Name, track.Artist[0].Name, err)
			} else if hydrated != nil {
				track = *hydrated
			}
		}

		if _, err := s.db.SaveTrack(user.ID, db.SourceListenBrainz, &track); err != nil {
			return fmt.Errorf("saving %s by %s: %w", track.Name, track.Artist[0].Name, err)
		}
		if user.ATProtoDID != nil && user.MostRecentAtProtoSessionID != nil && s.atprotoService != nil {
			if err := atprotoservice.SubmitPlayToPDS(ctx, *user.ATProtoDID, *user.MostRecentAtProtoSessionID, &track, s.atprotoService); err != nil {
				s.logger.Printf("Could not submit %s by %s for user %d: %v", track.Name, track.Artist[0].Name, user.ID, err)
			}
		}
		if err := s.db.SaveListenBrainzSyncTimestamp(user.ID, track.Timestamp); err != nil {
			return err
		}
		advanceUserCursor(user, track.Timestamp)
	}
	return nil
}

func (s *Service) getJSON(ctx context.Context, path, token string, query url.Values, target any) error {
	if err := s.limiter.Wait(ctx); err != nil {
		return err
	}

	requestURL := s.apiURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ListenBrainz returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding ListenBrainz response: %w", err)
	}
	return nil
}

func nowPlayingSignature(track *models.Track) string {
	artist := ""
	if len(track.Artist) > 0 {
		artist = track.Artist[0].Name
	}
	return artist + "\x00" + track.Album + "\x00" + track.Name
}

func advanceUserCursor(user *models.User, timestamp time.Time) {
	if user.ListenBrainzSyncedAt == nil || timestamp.After(*user.ListenBrainzSyncedAt) {
		value := timestamp
		user.ListenBrainzSyncedAt = &value
	}
}

package plyrfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	atprotoservice "github.com/teal-fm/piper/service/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
)

const (
	// plyr.fm API base URL - configurable via env
	defaultAPIBaseURL = "https://api.plyr.fm"
)

// NowPlayingResponse matches the response from plyr.fm's /now-playing/by-handle endpoint
type NowPlayingResponse struct {
	TrackName      string  `json:"track_name"`
	ArtistName     string  `json:"artist_name"`
	AlbumName      *string `json:"album_name"`
	DurationMs     int64   `json:"duration_ms"`
	ProgressMs     int64   `json:"progress_ms"`
	IsPlaying      bool    `json:"is_playing"`
	TrackID        int64   `json:"track_id"`
	FileID         string  `json:"file_id"`
	TrackURL       string  `json:"track_url"`
	ImageURL       *string `json:"image_url"`
	ServiceBaseURL string  `json:"service_base_url"`
}

// trackState holds the last seen track and whether it's been stamped
type trackState struct {
	track      *NowPlayingResponse
	hasStamped bool
}

type PlyrFMService struct {
	db                 *db.DB
	httpClient         *http.Client
	apiBaseURL         string
	musicBrainzService *musicbrainz.MusicBrainzService
	atprotoService     *atprotoauth.ATprotoAuthService
	playingNowService  interface {
		PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
		ClearPlayingNow(ctx context.Context, userID int64) error
	}
	// track last seen state per user to detect changes
	lastSeenTracks map[string]*trackState
	mu             sync.Mutex
	logger         *log.Logger
}

func NewPlyrFMService(
	database *db.DB,
	apiBaseURL string,
	musicBrainzService *musicbrainz.MusicBrainzService,
	atprotoService *atprotoauth.ATprotoAuthService,
	playingNowService interface {
		PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
		ClearPlayingNow(ctx context.Context, userID int64) error
	},
) *PlyrFMService {
	logger := log.New(os.Stdout, "plyrfm: ", log.LstdFlags|log.Lmsgprefix)

	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}

	return &PlyrFMService{
		db: database,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		apiBaseURL:         apiBaseURL,
		musicBrainzService: musicBrainzService,
		atprotoService:     atprotoService,
		playingNowService:  playingNowService,
		lastSeenTracks:     make(map[string]*trackState),
		logger:             logger,
	}
}

// GetNowPlaying fetches the current playing track for a plyr.fm handle
func (s *PlyrFMService) GetNowPlaying(ctx context.Context, handle string) (*NowPlayingResponse, error) {
	url := fmt.Sprintf("%s/now-playing/by-handle/%s", s.apiBaseURL, handle)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch now playing for %s: %w", handle, err)
	}
	defer resp.Body.Close()

	// 204 means nothing playing
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plyr.fm API error for %s: status %d, body: %s", handle, resp.StatusCode, string(body))
	}

	var nowPlaying NowPlayingResponse
	if err := json.NewDecoder(resp.Body).Decode(&nowPlaying); err != nil {
		return nil, fmt.Errorf("failed to decode response for %s: %w", handle, err)
	}

	return &nowPlaying, nil
}

// convertToModelsTrack converts plyr.fm response to piper's Track model
func (s *PlyrFMService) convertToModelsTrack(np *NowPlayingResponse) *models.Track {
	artists := []models.Artist{
		{
			Name: np.ArtistName,
		},
	}

	album := ""
	if np.AlbumName != nil {
		album = *np.AlbumName
	}

	return &models.Track{
		Name:           np.TrackName,
		Artist:         artists,
		Album:          album,
		URL:            np.TrackURL,
		DurationMs:     np.DurationMs,
		ProgressMs:     np.ProgressMs,
		ServiceBaseUrl: "plyr.fm",
		Timestamp:      time.Now().UTC(),
		HasStamped:     false,
	}
}

// isNewTrack checks if the current track is different from last seen
func (s *PlyrFMService) isNewTrack(handle string, current *NowPlayingResponse) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.lastSeenTracks[handle]
	if state == nil || state.track == nil {
		return true
	}

	// compare by track ID (most reliable)
	return state.track.TrackID != current.TrackID
}

// hasBeenStamped checks if the current track has already been stamped
func (s *PlyrFMService) hasBeenStamped(handle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.lastSeenTracks[handle]
	if state == nil {
		return false
	}
	return state.hasStamped
}

// markAsStamped marks the current track as stamped
func (s *PlyrFMService) markAsStamped(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state := s.lastSeenTracks[handle]; state != nil {
		state.hasStamped = true
	}
}

// updateLastSeen updates the last seen track for a handle (resets stamped status for new tracks)
func (s *PlyrFMService) updateLastSeen(handle string, track *NowPlayingResponse, isNew bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if isNew {
		// new track - reset stamped status
		s.lastSeenTracks[handle] = &trackState{track: track, hasStamped: false}
	} else if state := s.lastSeenTracks[handle]; state != nil {
		// same track - just update the track data (progress, etc)
		state.track = track
	}
}

// clearLastSeen clears the last seen track when nothing is playing
func (s *PlyrFMService) clearLastSeen(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastSeenTracks, handle)
}

// processUser fetches and processes now-playing for a single user
func (s *PlyrFMService) processUser(ctx context.Context, user *models.User, handle string) error {
	nowPlaying, err := s.GetNowPlaying(ctx, handle)
	if err != nil {
		return fmt.Errorf("error fetching now playing: %w", err)
	}

	// nothing playing - clear state
	if nowPlaying == nil || !nowPlaying.IsPlaying {
		s.clearLastSeen(handle)
		if s.playingNowService != nil {
			if err := s.playingNowService.ClearPlayingNow(ctx, user.ID); err != nil {
				s.logger.Printf("error clearing playing now for user %s: %v", handle, err)
			}
		}
		return nil
	}

	// check if this is a new track
	isNew := s.isNewTrack(handle, nowPlaying)
	s.updateLastSeen(handle, nowPlaying, isNew)

	// convert to piper track format
	track := s.convertToModelsTrack(nowPlaying)

	// publish playing now status
	if s.playingNowService != nil {
		if err := s.playingNowService.PublishPlayingNow(ctx, user.ID, track); err != nil {
			s.logger.Printf("error publishing playing now for user %s: %v", handle, err)
		}
	}

	// if new track, log and save to DB
	if isNew {
		s.logger.Printf("user %s is listening to: %s by %s", handle, track.Name, track.Artist[0].Name)

		// save track to DB
		if _, err := s.db.SaveTrack(user.ID, track); err != nil {
			s.logger.Printf("error saving track for user %s: %v", handle, err)
		}
	}

	// check stamping threshold: >50% played OR >30s played (standard scrobble rules)
	meetsThreshold := nowPlaying.ProgressMs > nowPlaying.DurationMs/2 || nowPlaying.ProgressMs > 30000
	alreadyStamped := s.hasBeenStamped(handle)

	if meetsThreshold && !alreadyStamped {
		s.markAsStamped(handle)
		s.logger.Printf("user %s stamped track: %s by %s", handle, track.Name, track.Artist[0].Name)

		// submit to ATProto PDS if configured
		if user.ATProtoDID != nil && *user.ATProtoDID != "" && user.MostRecentAtProtoSessionID != nil {
			// optionally hydrate with MusicBrainz data
			trackToSubmit := track
			if s.musicBrainzService != nil {
				hydratedTrack, err := musicbrainz.HydrateTrack(s.musicBrainzService, *track)
				if err != nil {
					s.logger.Printf("error hydrating track: %v, using original", err)
				} else {
					trackToSubmit = hydratedTrack
				}
			}

			if err := s.SubmitTrackToPDS(*user.ATProtoDID, *user.MostRecentAtProtoSessionID, trackToSubmit, ctx); err != nil {
				s.logger.Printf("error submitting track to PDS for user %s: %v", handle, err)
			} else {
				s.logger.Printf("submitted track to PDS for user %s", handle)
			}
		}
	}

	return nil
}

// SubmitTrackToPDS submits a track to the user's ATProto PDS
func (s *PlyrFMService) SubmitTrackToPDS(did string, sessionID string, track *models.Track, ctx context.Context) error {
	return atprotoservice.SubmitPlayToPDS(ctx, did, sessionID, track, s.atprotoService)
}

// StartListeningTracker starts the periodic tracker for plyr.fm users
func (s *PlyrFMService) StartListeningTracker(interval time.Duration) {
	ticker := time.NewTicker(interval)

	go func() {
		// initial fetch
		s.fetchAllUserTracks(context.Background())

		for range ticker.C {
			s.logger.Println("fetching plyr.fm tracks...")
			s.fetchAllUserTracks(context.Background())
			s.logger.Println("finished plyr.fm fetch cycle")
		}
	}()

	s.logger.Printf("plyr.fm listening tracker started with interval %v", interval)
}

// fetchAllUserTracks fetches tracks for all users with plyr.fm handles configured
func (s *PlyrFMService) fetchAllUserTracks(ctx context.Context) {
	users, err := s.db.GetAllUsersWithPlyrFM()
	if err != nil {
		s.logger.Printf("error loading plyr.fm users: %v", err)
		return
	}

	if len(users) == 0 {
		s.logger.Println("no plyr.fm users configured")
		return
	}

	s.logger.Printf("processing %d plyr.fm users", len(users))

	for _, user := range users {
		if ctx.Err() != nil {
			s.logger.Println("context cancelled, stopping fetch")
			break
		}

		if user.PlyrFMHandle == nil || *user.PlyrFMHandle == "" {
			continue
		}

		if err := s.processUser(ctx, user, *user.PlyrFMHandle); err != nil {
			s.logger.Printf("error processing user %s: %v", *user.PlyrFMHandle, err)
		}
	}
}

package lastfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"

	"github.com/teal-fm/piper/service/musicbrainz"
	"golang.org/x/time/rate"
)

const (
	lastfmAPIBaseURL = "https://ws.audioscrobbler.com/2.0/"
	defaultLimit     = 1 // Default number of tracks to fetch per user
)

type LastFMService struct {
	db                 *db.DB
	httpClient         *http.Client
	limiter            *rate.Limiter
	apiKey             string
	Usernames          []string
	musicBrainzService *musicbrainz.MusicBrainzService
	atprotoService     *atprotoauth.ATprotoAuthService
	playingNowService  interface {
		PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
		ClearPlayingNow(ctx context.Context, userID int64) error
	}
	lastSeenNowPlaying map[string]Track
	mu                 sync.Mutex
	logger             *log.Logger
}

func NewLastFMService(db *db.DB, apiKey string, musicBrainzService *musicbrainz.MusicBrainzService, atprotoService *atprotoauth.ATprotoAuthService, playingNowService interface {
	PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error
	ClearPlayingNow(ctx context.Context, userID int64) error
}) *LastFMService {
	logger := log.New(os.Stdout, "lastfm: ", log.LstdFlags|log.Lmsgprefix)

	return &LastFMService{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// Last.fm unofficial rate limit is ~5 requests per second
		limiter:            rate.NewLimiter(rate.Every(200*time.Millisecond), 1),
		apiKey:             apiKey,
		Usernames:          make([]string, 0),
		atprotoService:     atprotoService,
		musicBrainzService: musicBrainzService,
		playingNowService:  playingNowService,
		lastSeenNowPlaying: make(map[string]Track),
		mu:                 sync.Mutex{},
		logger:             logger,
	}
}

func (l *LastFMService) loadUsernames() error {
	u, err := l.db.GetAllUsersWithLastFM()
	if err != nil {
		l.logger.Printf("Error loading users with Last.fm from DB: %v", err)
		return fmt.Errorf("failed to load users from database: %w", err)
	}
	usernames := make([]string, len(u))
	for i, user := range u {
		// print out user stuff
		if user.LastFMUsername != nil { // Check if the username is set
			usernames[i] = *user.LastFMUsername
		} else {
			l.logger.Printf("User ID %d has Last.fm enabled but no username set", user.ID)
		}
	}

	// filter empty usernames (shouldn't happen?)
	filteredUsernames := make([]string, 0, len(usernames))
	for _, name := range usernames {
		if name != "" {
			filteredUsernames = append(filteredUsernames, name)
		}
	}

	l.Usernames = filteredUsernames
	l.logger.Printf("Loaded %d Last.fm usernames", len(l.Usernames))

	return nil
}

// getRecentTracks fetches the most recent tracks for a given Last.fm user.
func (l *LastFMService) getRecentTracks(ctx context.Context, username string, limit int) (*RecentTracksResponse, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}

	params := url.Values{}
	params.Set("method", "user.getrecenttracks")
	params.Set("user", username)
	params.Set("api_key", l.apiKey)
	params.Set("format", "json")
	params.Set("limit", strconv.Itoa(limit)) // Fetch a few more to handle duplicates/now playing

	apiURL := lastfmAPIBaseURL + "?" + params.Encode()

	if err := l.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", username, err)
	}

	l.logger.Printf("Fetching recent tracks for user: %s", username)
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recent tracks for %s: %w", username, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		// Handle specific Last.fm error codes if necessary
		// e.g., {"error": 6, "message": "User not found"}
		return nil, fmt.Errorf("last.fm API error for %s: status %d, body: %s", username, resp.StatusCode, string(bodyBytes))
	}

	var recentTracksResp RecentTracksResponse
	bodyBytes, err := io.ReadAll(resp.Body) // Read body first for potential decoding errors
	if err != nil {
		return nil, fmt.Errorf("failed to read response body for %s: %w", username, err)
	}
	if err := json.Unmarshal(bodyBytes, &recentTracksResp); err != nil {
		// Log the body content that failed to decode
		l.logger.Printf("Failed to decode response body for %s: %s", username, string(bodyBytes))
		return nil, fmt.Errorf("failed to decode response for %s: %w", username, err)
	}

	if len(recentTracksResp.RecentTracks.Tracks) > 0 {
		l.logger.Printf("Fetched %d tracks for %s. Most recent: %s - %s",
			len(recentTracksResp.RecentTracks.Tracks),
			username,
			recentTracksResp.RecentTracks.Tracks[0].Artist.Text,
			recentTracksResp.RecentTracks.Tracks[0].Name)
	} else {
		l.logger.Printf("No recent tracks found for %s", username)
	}

	return &recentTracksResp, nil
}

func (l *LastFMService) StartListeningTracker(interval time.Duration) {
	if err := l.loadUsernames(); err != nil {
		l.logger.Printf("Failed to perform initial username load: %v", err)
		// Decide if we should proceed without initial load or return error
	}

	if len(l.Usernames) == 0 {
		l.logger.Println("No Last.fm users configured. Tracker will run but fetch cycles will be skipped until users are added.")
	} else {
		l.logger.Printf("Found %d Last.fm users.", len(l.Usernames))
	}

	ticker := time.NewTicker(interval)
	go func() {
		// Initial fetch immediately
		if len(l.Usernames) > 0 {
			l.fetchAllUserTracks(context.Background())
		} else {
			l.logger.Println("Skipping initial fetch cycle as no users are configured.")
		}

		for {
			select {
			case <-ticker.C:
				// refresh usernames periodically from db
				if err := l.loadUsernames(); err != nil {
					l.logger.Printf("Error reloading usernames in ticker: %v", err)
					// Continue ticker loop even if reload fails? Or log and potentially stop?
					continue // Continue for now
				}
				if len(l.Usernames) > 0 {
					l.fetchAllUserTracks(context.Background())
				} else {
					l.logger.Println("No Last.fm users configured. Skipping fetch cycle.")
				}
				// TODO: Implement graceful shutdown using context cancellation
				// case <-ctx.Done():
				//  l.logger.Println("Stopping Last.fm listening tracker.")
				//	ticker.Stop()
				//  return
			}
		}
	}()

	l.logger.Printf("Last.fm Listening Tracker started with interval %v", interval)
}

// fetchAllUserTracks iterates through users and fetches their tracks.
func (l *LastFMService) fetchAllUserTracks(ctx context.Context) {
	l.logger.Printf("Starting fetch cycle for %d users...", len(l.Usernames))
	var wg sync.WaitGroup                             // Use WaitGroup to fetch concurrently (optional)
	fetchErrors := make(chan error, len(l.Usernames)) // Channel for errors

	for _, username := range l.Usernames {
		if ctx.Err() != nil {
			l.logger.Printf("Context cancelled before starting fetch for user %s.", username)
			break // Exit loop if context is cancelled
		}

		wg.Add(1)
		go func(uname string) { // Launch fetch and process in a goroutine per user
			defer wg.Done()
			if ctx.Err() != nil {
				l.logger.Printf("Context cancelled during fetch cycle for user %s.", uname)
				return // Exit goroutine if context is cancelled
			}

			// Fetch slightly more than 1 track to better handle edge cases
			// where the latest is 'now playing' or duplicates exist.
			const fetchLimit = 5
			recentTracks, err := l.getRecentTracks(ctx, uname, fetchLimit)
			if err != nil {
				l.logger.Printf("Error fetching tracks for %s: %v", uname, err)
				fetchErrors <- fmt.Errorf("fetch failed for %s: %w", uname, err) // Report error
				return
			}

			if recentTracks == nil || len(recentTracks.RecentTracks.Tracks) == 0 {
				l.logger.Printf("No tracks returned for user %s", uname)
				return
			}

			// Process the fetched tracks
			if err := l.processTracks(ctx, uname, recentTracks.RecentTracks.Tracks); err != nil {
				l.logger.Printf("Error processing tracks for %s: %v", uname, err)
				fetchErrors <- fmt.Errorf("process failed for %s: %w", uname, err) // Report error
			}
		}(username)
	}

	wg.Wait()          // Wait for all goroutines to complete
	close(fetchErrors) // Close the error channel

	// Log any errors that occurred during the fetch cycle
	errorCount := 0
	for err := range fetchErrors {
		l.logger.Printf("Fetch cycle error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		l.logger.Printf("Finished fetch cycle with %d errors.", errorCount)
	} else {
		l.logger.Println("Finished fetch cycle successfully.")
	}
}

func (l *LastFMService) processTracks(ctx context.Context, username string, tracks []Track) error {
	if l.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	user, err := l.db.GetUserByLastFM(username)
	if err != nil {
		return fmt.Errorf("failed to get user ID for %s: %w", username, err)
	}

	lastKnownTimestamp, err := l.db.GetLastKnownTimestamp(user.ID)
	if err != nil {
		return fmt.Errorf("failed to get last scrobble timestamp for %s: %w", username, err)
	}

	if lastKnownTimestamp == nil {
		l.logger.Printf("no previous scrobble timestamp found for user %s. processing latest track.", username)
	} else {
		l.logger.Printf("last known scrobble for %s was at %s", username, lastKnownTimestamp.Format(time.RFC3339))
	}

	var (
		processedCount      int
		latestProcessedTime time.Time
	)

	// handle now playing track separately
	if len(tracks) > 0 && tracks[0].Attr != nil && tracks[0].Attr.NowPlaying == "true" {
		nowPlayingTrack := tracks[0]
		l.logger.Printf("now playing track for %s: %s - %s", username, nowPlayingTrack.Artist.Text, nowPlayingTrack.Name)
		l.mu.Lock()
		lastSeen, existed := l.lastSeenNowPlaying[username]
		// if our current track matches with last seen
		// just compare artist/album/name for now
		if existed && lastSeen.Album == nowPlayingTrack.Album && lastSeen.Name == nowPlayingTrack.Name && lastSeen.Artist == nowPlayingTrack.Artist {
			l.logger.Printf("current track matches last seen track for %s", username)
		} else {
			l.logger.Printf("current track does not match last seen track for %s", username)
			// aha! we record this!
			l.lastSeenNowPlaying[username] = nowPlayingTrack

			// Publish playing now status
			if l.playingNowService != nil {
				// Convert Last.fm track to models.Track format
				piperTrack := l.convertLastFMTrackToModelsTrack(nowPlayingTrack)
				if err := l.playingNowService.PublishPlayingNow(ctx, user.ID, piperTrack); err != nil {
					l.logger.Printf("Error publishing playing now for user %s: %v", username, err)
				}
			}
		}
		l.mu.Unlock()
	} else {
		// No now playing track - clear playing now status
		if l.playingNowService != nil {
			if err := l.playingNowService.ClearPlayingNow(ctx, user.ID); err != nil {
				l.logger.Printf("Error clearing playing now for user %s: %v", username, err)
			}
		}
	}

	// find last non-now-playing track
	var lastNonNowPlaying *Track
	for i := len(tracks) - 1; i >= 0; i-- {
		if tracks[i].Attr == nil || tracks[i].Attr.NowPlaying != "true" {
			lastNonNowPlaying = &tracks[i]
			break
		}
	}

	if lastNonNowPlaying == nil {
		l.logger.Printf("no non-now-playing tracks found for user %s.", username)
		return nil
	}

	latestTrackTime := lastNonNowPlaying.Date

	// print both
	l.logger.Printf("latestTrackTime: %s\n", latestTrackTime)
	l.logger.Printf("lastKnownTimestamp: %s\n", lastKnownTimestamp)

	if lastKnownTimestamp != nil && lastKnownTimestamp.Equal(latestTrackTime.Time) {
		l.logger.Printf("no new tracks to process for user %s.", username)
		return nil
	}

	for _, track := range tracks {
		if track.Date == nil {
			l.logger.Printf("skipping track without timestamp for %s: %s - %s", username, track.Artist.Text, track.Name)
			continue
		}

		trackTime := track.Date.Time
		// before or at last known
		if lastKnownTimestamp != nil && (trackTime.Before(*lastKnownTimestamp) || trackTime.Equal(*lastKnownTimestamp)) {
			if processedCount == 0 {
				l.logger.Printf("reached already known scrobbles for user %s (track time: %s, last known: %s).",
					username, trackTime.Format(time.RFC3339), lastKnownTimestamp.Format(time.RFC3339))
			}
			break
		}

		baseTrack := models.Track{
			Name:           track.Name,
			URL:            track.URL,
			ServiceBaseUrl: "last.fm",
			Album:          track.Album.Text,
			Timestamp:      trackTime,
			Artist: []models.Artist{
				{
					Name: track.Artist.Text,
				},
			},
			// this is submitted after the track has been scrobbled on LFM
			HasStamped: true,
		}

		hydratedTrack, err := musicbrainz.HydrateTrack(l.musicBrainzService, baseTrack)
		if err != nil {
			l.logger.Printf("error hydrating track for user %s: %s - %s: %v", username, track.Artist.Text, track.Name, err)
			// we can use the track without MBIDs, it's still valid
			hydratedTrack = &baseTrack
		}
		l.db.SaveTrack(user.ID, hydratedTrack)
		l.logger.Printf("Submitting track")
		err = l.SubmitTrackToPDS(*user.ATProtoDID, *user.MostRecentAtProtoSessionID, hydratedTrack, ctx)
		if err != nil {
			l.logger.Printf("error submitting track for user %s: %s - %s: %v", username, track.Artist.Text, track.Name, err)
		}
		processedCount++

		if trackTime.After(latestProcessedTime) {
			latestProcessedTime = trackTime
		}

		if lastKnownTimestamp != nil {
			break
		}
	}

	if processedCount > 0 {
		l.logger.Printf("processed %d new track(s) for user %s. latest timestamp: %s",
			processedCount, username, latestProcessedTime.Format(time.RFC3339))
	}

	return nil
}

func (l *LastFMService) SubmitTrackToPDS(did string, mostRecentAtProtoSessionID string, track *models.Track, ctx context.Context) error {
	client, err := l.atprotoService.GetATProtoClient(did, mostRecentAtProtoSessionID, ctx)
	if err != nil || client == nil {
		return err
	}

	// printout the session details
	l.logger.Printf("Submitting track for the did: %+v\n", client.AccountDID.String())

	artists := make([]*teal.AlphaFeedDefs_Artist, 0, len(track.Artist))
	for _, a := range track.Artist {
		artist := &teal.AlphaFeedDefs_Artist{
			ArtistName: a.Name,
			ArtistMbId: a.MBID,
		}
		artists = append(artists, artist)
	}

	var durationPtr *int64
	if track.DurationMs > 0 {
		durationSeconds := track.DurationMs / 1000
		durationPtr = &durationSeconds
	}

	playedTimeStr := track.Timestamp.Format(time.RFC3339)
	submissionAgent := viper.GetString("app.submission_agent")
	if submissionAgent == "" {
		submissionAgent = "piper/v0.0.2" // Default if not configured
	}

	// track -> tealfm track
	tfmTrack := teal.AlphaFeedPlay{
		LexiconTypeID: "fm.teal.alpha.feed.play", // Assuming this is the correct Lexicon ID
		// tfm specifies duration in seconds
		Duration:  durationPtr, // Pointer required
		TrackName: track.Name,
		// should be unix timestamp
		PlayedTime:            &playedTimeStr, // Pointer required
		Artists:               artists,
		ReleaseMbId:           track.ReleaseMBID,   // Pointer required
		ReleaseName:           &track.Album,        // Pointer required
		RecordingMbId:         track.RecordingMBID, // Pointer required
		SubmissionClientAgent: &submissionAgent,    // Pointer required
	}

	input := comatproto.RepoCreateRecord_Input{
		Collection: "fm.teal.alpha.feed.play",
		Repo:       client.AccountDID.String(),
		Record:     &lexutil.LexiconTypeDecoder{Val: &tfmTrack},
	}

	if _, err := comatproto.RepoCreateRecord(ctx, client, &input); err != nil {
		return err
	}

	// submit track to PDS

	return nil
}

// convertLastFMTrackToModelsTrack converts a Last.fm Track to models.Track format
func (l *LastFMService) convertLastFMTrackToModelsTrack(track Track) *models.Track {
	// Create artist array
	artists := []models.Artist{
		{
			Name: track.Artist.Text,
			// Note: Last.fm doesn't provide MBID in now playing, would need separate lookup
		},
	}

	// Set timestamp to current time for now playing
	timestamp := time.Now()

	piperTrack := &models.Track{
		Name:           track.Name,
		Artist:         artists,
		Album:          track.Album.Text, // Album is a struct with Text field
		Timestamp:      timestamp,
		ServiceBaseUrl: "lastfm",
		HasStamped:     false, // Playing now tracks aren't stamped yet
	}

	// Add URL if available
	if track.URL != "" {
		piperTrack.URL = track.URL
	}

	// Try to extract MBID if available (Last.fm sometimes provides this)
	if track.MBID != "" { // MBID is capitalized
		piperTrack.RecordingMBID = &track.MBID
	}

	return piperTrack
}

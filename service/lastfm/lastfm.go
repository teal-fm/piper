package lastfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/musicbrainz"
	"golang.org/x/time/rate"
)

const (
	lastfmAPIBaseURL = "https://ws.audioscrobbler.com/2.0/"
	defaultLimit     = 1 // Default number of tracks to fetch per user
)

// Structs to represent the Last.fm API response for user.getrecenttracks
type RecentTracksResponse struct {
	RecentTracks RecentTracks `json:"recenttracks"`
}

type RecentTracks struct {
	Tracks []Track      `json:"track"`
	Attr   TrackXMLAttr `json:"@attr"`
}

type Track struct {
	Artist     Artist     `json:"artist"`
	Streamable string     `json:"streamable"` // Typically "0" or "1"
	Image      []Image    `json:"image"`
	MBID       string     `json:"mbid"` // MusicBrainz ID for the track
	Album      Album      `json:"album"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Date       *TrackDate `json:"date,omitempty"` // Use pointer for optional fields
	NowPlaying *struct {  // Custom handling for @attr.nowplaying
		NowPlaying string `json:"nowplaying"` // Field name corrected to match struct tag
	} `json:"@attr,omitempty"` // This captures the @attr object within the track
}

type Artist struct {
	MBID string `json:"mbid"` // MusicBrainz ID for the artist
	Text string `json:"#text"`
}

type Image struct {
	Size string `json:"size"`  // "small", "medium", "large", "extralarge"
	Text string `json:"#text"` // URL of the image
}

type Album struct {
	MBID string `json:"mbid"`  // MusicBrainz ID for the album
	Text string `json:"#text"` // Album name
}

type TrackDate struct {
	UTS  string `json:"uts"`   // Unix timestamp string
	Text string `json:"#text"` // Human-readable date string
}

type TrackXMLAttr struct {
	User       string `json:"user"`
	TotalPages string `json:"totalPages"`
	Page       string `json:"page"`
	PerPage    string `json:"perPage"`
	Total      string `json:"total"`
}

type LastFMService struct {
	db                 *db.DB
	httpClient         *http.Client
	limiter            *rate.Limiter
	apiKey             string
	Usernames          []string
	musicBrainzService *musicbrainz.MusicBrainzService
	// Removed in-memory map, assuming DB handles last seen state
	// lastSeenTrackDate map[string]time.Time
	// mu sync.Mutex // Keep mutex if other shared state is added later
}

func NewLastFMService(db *db.DB, apiKey string, musicBrainzService *musicbrainz.MusicBrainzService) *LastFMService {
	return &LastFMService{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// Last.fm unofficial rate limit is ~5 requests per second
		limiter:   rate.NewLimiter(rate.Every(200*time.Millisecond), 1),
		apiKey:    apiKey,
		Usernames: make([]string, 0),
		// lastSeenTrackDate: make(map[string]time.Time), // Removed
		musicBrainzService: musicBrainzService,
	}
}

func (l *LastFMService) loadUsernames() error {
	u, err := l.db.GetAllUsersWithLastFM()
	if err != nil {
		log.Printf("Error loading users with Last.fm from DB: %v", err)
		return fmt.Errorf("failed to load users from database: %w", err)
	}
	usernames := make([]string, len(u))
	for i, user := range u {
		// Assuming the User struct has a LastFMUsername field
		if user.LastFMUsername != nil { // Check if the username is set
			usernames[i] = *user.LastFMUsername
		} else {
			log.Printf("User ID %d has Last.fm enabled but no username set", user.ID)
			// Handle this case - maybe skip the user or log differently
		}
	}

	// Filter out empty usernames if any were added due to missing data
	filteredUsernames := make([]string, 0, len(usernames))
	for _, name := range usernames {
		if name != "" {
			filteredUsernames = append(filteredUsernames, name)
		}
	}

	l.Usernames = filteredUsernames
	log.Printf("Loaded %d Last.fm usernames", len(l.Usernames))

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

	log.Printf("Fetching recent tracks for user: %s", username)
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
		log.Printf("Failed to decode response body for %s: %s", username, string(bodyBytes))
		return nil, fmt.Errorf("failed to decode response for %s: %w", username, err)
	}

	if len(recentTracksResp.RecentTracks.Tracks) > 0 {
		log.Printf("Fetched %d tracks for %s. Most recent: %s - %s",
			len(recentTracksResp.RecentTracks.Tracks),
			username,
			recentTracksResp.RecentTracks.Tracks[0].Artist.Text,
			recentTracksResp.RecentTracks.Tracks[0].Name)
	} else {
		log.Printf("No recent tracks found for %s", username)
	}

	return &recentTracksResp, nil
}

func (l *LastFMService) StartListeningTracker(interval time.Duration) {
	if err := l.loadUsernames(); err != nil {
		log.Printf("Failed to perform initial username load: %v", err)
		// Decide if we should proceed without initial load or return error
	}

	if len(l.Usernames) == 0 {
		log.Println("No Last.fm users configured. Tracker will run but fetch cycles will be skipped until users are added.")
	} else {
		log.Printf("Found %d Last.fm users.", len(l.Usernames))
	}

	ticker := time.NewTicker(interval)
	go func() {
		// Initial fetch immediately
		if len(l.Usernames) > 0 {
			l.fetchAllUserTracks(context.Background())
		} else {
			log.Println("Skipping initial fetch cycle as no users are configured.")
		}

		for {
			select {
			case <-ticker.C:
				// refresh usernames periodically from db
				if err := l.loadUsernames(); err != nil {
					log.Printf("Error reloading usernames in ticker: %v", err)
					// Continue ticker loop even if reload fails? Or log and potentially stop?
					continue // Continue for now
				}
				if len(l.Usernames) > 0 {
					l.fetchAllUserTracks(context.Background())
				} else {
					log.Println("No Last.fm users configured. Skipping fetch cycle.")
				}
				// TODO: Implement graceful shutdown using context cancellation
				// case <-ctx.Done():
				//  log.Println("Stopping Last.fm listening tracker.")
				//	ticker.Stop()
				//  return
			}
		}
	}()

	log.Printf("Last.fm Listening Tracker started with interval %v", interval)
}

// fetchAllUserTracks iterates through users and fetches their tracks.
func (l *LastFMService) fetchAllUserTracks(ctx context.Context) {
	log.Printf("Starting fetch cycle for %d users...", len(l.Usernames))
	var wg sync.WaitGroup                             // Use WaitGroup to fetch concurrently (optional)
	fetchErrors := make(chan error, len(l.Usernames)) // Channel for errors

	for _, username := range l.Usernames {
		if ctx.Err() != nil {
			log.Printf("Context cancelled before starting fetch for user %s.", username)
			break // Exit loop if context is cancelled
		}

		wg.Add(1)
		go func(uname string) { // Launch fetch and process in a goroutine per user
			defer wg.Done()
			if ctx.Err() != nil {
				log.Printf("Context cancelled during fetch cycle for user %s.", uname)
				return // Exit goroutine if context is cancelled
			}

			// Fetch slightly more than 1 track to better handle edge cases
			// where the latest is 'now playing' or duplicates exist.
			const fetchLimit = 5
			recentTracks, err := l.getRecentTracks(ctx, uname, fetchLimit)
			if err != nil {
				log.Printf("Error fetching tracks for %s: %v", uname, err)
				fetchErrors <- fmt.Errorf("fetch failed for %s: %w", uname, err) // Report error
				return
			}

			if recentTracks == nil || len(recentTracks.RecentTracks.Tracks) == 0 {
				log.Printf("No tracks returned for user %s", uname)
				return
			}

			// Process the fetched tracks
			if err := l.processTracks(uname, recentTracks.RecentTracks.Tracks); err != nil {
				log.Printf("Error processing tracks for %s: %v", uname, err)
				fetchErrors <- fmt.Errorf("process failed for %s: %w", uname, err) // Report error
			}
		}(username)
	}

	wg.Wait()          // Wait for all goroutines to complete
	close(fetchErrors) // Close the error channel

	// Log any errors that occurred during the fetch cycle
	errorCount := 0
	for err := range fetchErrors {
		log.Printf("Fetch cycle error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		log.Printf("Finished fetch cycle with %d errors.", errorCount)
	} else {
		log.Println("Finished fetch cycle successfully.")
	}
}

// processTracks processes the fetched tracks for a user, adding new scrobbles to the DB.
func (l *LastFMService) processTracks(username string, tracks []Track) error {
	if l.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// get uid
	user, err := l.db.GetUserByLastFM(username)
	if err != nil {
		return fmt.Errorf("failed to get user ID for %s: %w", username, err)
	}

	lastKnownTimestamp, err := l.db.GetLastScrobbleTimestamp(user.ID) // Hypothetical DB call
	if err != nil {
		return fmt.Errorf("failed to get last scrobble timestamp for %s: %w", username, err)
	}

	found := lastKnownTimestamp == nil
	if found {
		log.Printf("No previous scrobble timestamp found for user %s. Processing latest track.", username)
	} else {
		log.Printf("Last known scrobble for %s was at %s", username, lastKnownTimestamp.Format(time.RFC3339))
	}

	processedCount := 0
	var latestProcessedTime time.Time

	for i := len(tracks) - 1; i >= 0; i-- {
		track := tracks[i]

		// skip now playing
		if track.NowPlaying != nil && track.NowPlaying.NowPlaying == "true" {
			log.Printf("Skipping 'now playing' track for %s: %s - %s", username, track.Artist.Text, track.Name)
			continue
		}

		// skip tracks w/out valid date (should be none, but just in case)
		if track.Date == nil || track.Date.UTS == "" {
			log.Printf("Skipping track without timestamp for %s: %s - %s", username, track.Artist.Text, track.Name)
			continue
		}

		// parse uts (unix timestamp string)
		uts, err := strconv.ParseInt(track.Date.UTS, 10, 64)
		if err != nil {
			log.Printf("Error parsing timestamp '%s' for track %s - %s: %v", track.Date.UTS, track.Artist.Text, track.Name, err)
			continue
		}
		trackTime := time.Unix(uts, 0)

		if lastKnownTimestamp != nil && !trackTime.After(*lastKnownTimestamp) {
			if processedCount == 0 {
				log.Printf("Reached already known scrobbles for user %s (Track time: %s, Last known: %s).",
					username,
					trackTime.Format(time.RFC3339),
					lastKnownTimestamp.UTC().Format(time.RFC3339))
			}
			break
		}

		unhydratedArtist := []models.Artist{
			{
				Name: track.Artist.Text,
				MBID: track.Artist.MBID,
			},
		}

		mTrack := models.Track{
			Name:           track.Name,
			URL:            track.URL,
			ServiceBaseUrl: "last.fm",
			Album:          track.Album.Text,
			Timestamp:      time.Unix(uts, 0),
			Artist:         unhydratedArtist,
		}

		// Fix based on diagnostic: Assume HydrateTrack returns (*models.Track, error)
		hydratedTrackPtr, err := musicbrainz.HydrateTrack(l.musicBrainzService, mTrack)
		if err != nil {
			// Log hydration error specifically
			log.Printf("Error hydrating track details for user %s, track %s - %s: %v", username, track.Artist.Text, track.Name, err)
			// fallback to original track if hydration fails
			hydratedTrackPtr = &mTrack
			continue
		}

		l.db.SaveTrack(user.ID, hydratedTrackPtr)

		processedCount++

		if trackTime.After(latestProcessedTime) {
			latestProcessedTime = trackTime
		}

		if found {
			break
		}
	}

	if processedCount > 0 {
		log.Printf("Successfully processed %d new track(s) for user %s. Latest timestamp in batch: %s",
			processedCount, username, latestProcessedTime.Format(time.RFC3339))
	}

	return nil
}

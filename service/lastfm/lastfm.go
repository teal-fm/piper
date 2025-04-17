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
	"time"

	"github.com/teal-fm/piper/db"
	"golang.org/x/time/rate"
)

const (
	lastfmAPIBaseURL = "https://ws.audioscrobbler.com/2.0/"
	defaultLimit     = 50 // Default number of tracks to fetch per user
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
		NowPlaying string `json:"nowplaying"`
	} `json:"@attr,omitempty"`
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
	db         *db.DB
	httpClient *http.Client
	limiter    *rate.Limiter
	apiKey     string
	Usernames  []string
}

func NewLastFMService(db *db.DB, apiKey string) *LastFMService {
	return &LastFMService{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// Last.fm unofficial rate limit is ~5 requests per second
		limiter:   rate.NewLimiter(rate.Every(200*time.Millisecond), 1),
		apiKey:    apiKey,
		Usernames: make([]string, 0),
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
	params.Set("limit", strconv.Itoa(limit))

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
		return nil, fmt.Errorf("last.fm API error for %s: status %d, body: %s", username, resp.StatusCode, string(bodyBytes))
	}

	var recentTracksResp RecentTracksResponse
	if err := json.NewDecoder(resp.Body).Decode(&recentTracksResp); err != nil {
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
	}

	if len(l.Usernames) == 0 {
		log.Println("No Last.fm users configured! Will start listening tracker anyways.")
	}

	ticker := time.NewTicker(interval)
	go func() {
		l.fetchAllUserTracks(context.Background())

		for {
			select {
			case <-ticker.C:
				// refresh usernames periodically from db
				if err := l.loadUsernames(); err != nil {
					log.Printf("Error reloading usernames in ticker: %v", err)
				}
				if len(l.Usernames) > 0 {
					l.fetchAllUserTracks(context.Background())
				} else {
					log.Println("No Last.fm users configured. Skipping fetch cycle.")
				}
				// Add a way to stop the goroutine if needed, e.g., via a context or channel
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
	for _, username := range l.Usernames {
		if ctx.Err() != nil {
			log.Printf("Context cancelled during fetch cycle for user %s.", username)
			return
		}
		recentTracks, err := l.getRecentTracks(ctx, username, defaultLimit)
		if err != nil {
			log.Printf("Error fetching tracks for %s: %v", username, err)
			continue
		}

		// TODO: Process the fetched tracks (e.g., store in DB, update stats)
		_ = recentTracks // Avoid unused variable warning for now
		// Example processing:
		// l.processTracks(username, recentTracks.RecentTracks.Tracks)

	}
	log.Println("Finished fetch cycle.")
}

// Placeholder for processing logic
// func (l *LastFMService) processTracks(username string, tracks []Track) {
//  log.Printf("Processing %d tracks for user %s", len(tracks), username)
//  // Implement logic to store tracks, update user scrobble counts, etc.
//  // Consider handling 'now playing' tracks differently (track.NowPlaying != nil)
//  // Be mindful of duplicates if the interval is short. Store the last fetched timestamp?
// }

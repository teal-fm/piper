package musicbrainz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

// ArtistCredit API Types
type ArtistCredit struct {
	Artist struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		SortName string `json:"sort-name,omitempty"`
	} `json:"artist"`
	Joinphrase string `json:"joinphrase,omitempty"`
	Name       string `json:"name"`
}

type ReleaseGroup struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	PrimaryType    string   `json:"primary-type,omitempty"`
	SecondaryTypes []string `json:"secondary-types,omitempty"`
}

type Release struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Status         string        `json:"status,omitempty"`
	Date           string        `json:"date,omitempty"` // YYYY-MM-DD, YYYY-MM, or YYYY
	Country        string        `json:"country,omitempty"`
	Disambiguation string        `json:"disambiguation,omitempty"`
	TrackCount     int           `json:"track-count,omitempty"`
	ReleaseGroup   *ReleaseGroup `json:"release-group,omitempty"`
}

type Recording struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Length       int            `json:"length,omitempty"` // milliseconds
	ISRCs        []string       `json:"isrcs,omitempty"`
	ArtistCredit []ArtistCredit `json:"artist-credit,omitempty"`
	Releases     []Release      `json:"releases,omitempty"`
}

type SearchResponse struct {
	Created    time.Time   `json:"created"`
	Count      int         `json:"count"`
	Offset     int         `json:"offset"`
	Recordings []Recording `json:"recordings"`
}

type SearchParams struct {
	Track   string
	Artist  string
	Release string
	ISRC    string
}

// cacheEntry holds the cached data and its expiration time.
type cacheEntry struct {
	recordings []Recording
	expiresAt  time.Time
}

type Service struct {
	db          *db.DB
	httpClient  *http.Client
	limiter     *rate.Limiter
	searchCache map[string]cacheEntry // In-memory cache for search results
	cacheMutex  sync.RWMutex          // Mutex to protect the cache
	cacheTTL    time.Duration         // Time-to-live for cache entries
	cleaner     MetadataCleaner       // Cleaner for cleaning up expired cache entries
	logger      *log.Logger           // Logger for logging
}

// NewMusicBrainzService creates a new service instance with rate limiting and caching.
func NewMusicBrainzService(db *db.DB) *Service {
	// MusicBrainz allows 1 request per second
	limiter := rate.NewLimiter(rate.Every(time.Second), 1)
	// Set a default cache TTL (e.g., 1 hour)
	defaultCacheTTL := 1 * time.Hour
	logger := log.New(os.Stdout, "musicbrainz: ", log.LstdFlags|log.Lmsgprefix)
	return &Service{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		limiter:     limiter,
		searchCache: make(map[string]cacheEntry),  // Initialize the cache map
		cacheTTL:    defaultCacheTTL,              // Set the cache TTL
		cleaner:     *NewMetadataCleaner("Latin"), // Initialize the cleaner
		// cacheMutex is zero-value ready
		logger: logger,
	}
}

func generateCacheKey(params SearchParams) string {
	return fmt.Sprintf("track=%s&artist=%s&release=%s&isrc=%s",
		url.QueryEscape(params.Track),
		url.QueryEscape(params.Artist),
		url.QueryEscape(params.Release),
		url.QueryEscape(params.ISRC))
}

func buildSearchQuery(params SearchParams) string {
	var queryParts []string
	if params.ISRC != "" {
		queryParts = append(queryParts, fmt.Sprintf(`isrc:"%s"`, params.ISRC))
	}
	if params.Track != "" {
		queryParts = append(queryParts, fmt.Sprintf(`recording:"%s"`, params.Track))
	}
	if params.Artist != "" {
		queryParts = append(queryParts, fmt.Sprintf(`artist:"%s"`, params.Artist))
	}
	if params.Release != "" {
		queryParts = append(queryParts, fmt.Sprintf(`release:"%s"`, params.Release))
	}
	return strings.Join(queryParts, " AND ")
}

func buildSearchEndpoint(query string) string {
	return fmt.Sprintf("https://musicbrainz.org/ws/2/recording?query=%s&fmt=json&inc=artists+releases+isrcs", url.QueryEscape(query))
}

func getCacheEntry(cache map[string]cacheEntry, cacheKey string) ([]Recording, bool) {
	entry, found := cache[cacheKey]
	now := time.Now().UTC()
	if found && now.Before(entry.expiresAt) {
		return entry.recordings, true
	}
	return nil, false
}

func setCacheEntry(cache map[string]cacheEntry, cacheKey string, recordings []Recording, ttl time.Duration) {
	cache[cacheKey] = cacheEntry{
		recordings: recordings,
		expiresAt:  time.Now().UTC().Add(ttl),
	}
}

func executeRequest(ctx context.Context, client *http.Client, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "piper/0.0.1 ( https://github.com/teal-fm/piper )")

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context error during request execution: %w", ctx.Err())
		}
		return nil, fmt.Errorf("failed to execute request to %s: %w", endpoint, err)
	}
	return resp, nil
}

func decodeResponse(resp *http.Response, endpoint string) (SearchResponse, error) {
	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("failed to decode response from %s: %w", endpoint, err)
	}
	return result, nil
}

func (s *Service) SearchMusicBrainz(ctx context.Context, params SearchParams) ([]Recording, error) {
	if params.Track == "" && params.Artist == "" && params.Release == "" && params.ISRC == "" {
		return nil, fmt.Errorf("at least one search parameter (Track, Artist, Release, ISRC) must be provided")
	}

	params.Track, _ = s.cleaner.CleanRecording(params.Track)
	params.Artist, _ = s.cleaner.CleanArtist(params.Artist)

	cacheKey := generateCacheKey(params)

	s.cacheMutex.RLock()
	if recordings, found := getCacheEntry(s.searchCache, cacheKey); found {
		s.cacheMutex.RUnlock()
		s.logger.Printf("Cache hit for MusicBrainz search: key=%s", cacheKey)
		return recordings, nil
	}
	s.cacheMutex.RUnlock()

	s.logger.Printf("Cache miss for MusicBrainz search: key=%s", cacheKey)

	query := buildSearchQuery(params)
	endpoint := buildSearchEndpoint(query)

	if err := s.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	resp, err := executeRequest(ctx, s.httpClient, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MusicBrainz API request to %s returned status %d", endpoint, resp.StatusCode)
	}

	result, err := decodeResponse(resp, endpoint)
	if err != nil {
		return nil, err
	}

	s.cacheMutex.Lock()
	setCacheEntry(s.searchCache, cacheKey, result.Recordings, s.cacheTTL)
	s.cacheMutex.Unlock()
	s.logger.Printf("Cached MusicBrainz search result for key=%s, TTL=%s", cacheKey, s.cacheTTL)

	return result.Recordings, nil
}

// isOfficialAlbum checks if a release is an official album (not a compilation, EP, promo, etc.)
func isOfficialAlbum(r *Release) bool {
	// Must be Official status (not Promotion, Bootleg, etc.)
	if r.Status != "" && r.Status != "Official" {
		return false
	}
	// Check release group type if available
	if r.ReleaseGroup != nil {
		// Must be an Album
		if r.ReleaseGroup.PrimaryType != "Album" {
			return false
		}
		// Must not be a compilation, soundtrack, etc.
		if len(r.ReleaseGroup.SecondaryTypes) > 0 {
			return false
		}
	}
	return true
}

// GetBestRelease selects the 'best' release from a list based on specific criteria.
// expectedAlbum is the album name we're looking for (e.g., from the user's listening data).
func (s *Service) GetBestRelease(releases []Release, trackTitle string, expectedAlbum string) *Release {
	if len(releases) == 0 {
		return nil
	}
	if len(releases) == 1 {
		// Return a pointer to the single element
		r := releases[0]
		return &r
	}

	// Sort releases: Prefer valid dates first, then sort by date, title, id.
	sort.SliceStable(releases, func(i, j int) bool {
		dateA := releases[i].Date
		dateB := releases[j].Date
		validDateA := len(dateA) >= 4 // Basic check for YYYY format or longer
		validDateB := len(dateB) >= 4

		// Put invalid/empty dates at the end
		if validDateA && !validDateB {
			return true
		}
		if !validDateA && validDateB {
			return false
		}
		// If both valid or both invalid, compare dates lexicographically
		if dateA != dateB {
			return dateA < dateB
		}
		// If dates are same, compare by title
		if releases[i].Title != releases[j].Title {
			return releases[i].Title < releases[j].Title
		}
		// If titles are same, compare by ID
		return releases[i].ID < releases[j].ID
	})

	// Normalize expected album for comparison
	expectedAlbumLower := strings.ToLower(strings.TrimSpace(expectedAlbum))

	// 1. If we have an expected album name, find an official release that matches it
	if expectedAlbumLower != "" {
		for i := range releases {
			release := &releases[i]
			releaseTitleLower := strings.ToLower(strings.TrimSpace(release.Title))
			// Check if release title matches expected album (exact or starts with for deluxe editions)
			if (releaseTitleLower == expectedAlbumLower || strings.HasPrefix(releaseTitleLower, expectedAlbumLower)) && isOfficialAlbum(release) {
				return release
			}
		}
	}

	// 2. Find oldest official album release where country is 'XW' or 'US' AND title is NOT track title
	for i := range releases {
		release := &releases[i]
		if (release.Country == "XW" || release.Country == "US") && release.Title != trackTitle && isOfficialAlbum(release) {
			return release
		}
	}

	// 3. Find oldest official album release where title is NOT track title
	for i := range releases {
		release := &releases[i]
		if release.Title != trackTitle && isOfficialAlbum(release) {
			return release
		}
	}

	// 4. Find oldest official release (any type) where title is NOT track title
	for i := range releases {
		release := &releases[i]
		if release.Title != trackTitle && release.Status == "Official" {
			return release
		}
	}

	// 5. Find any release where title is NOT track title
	for i := range releases {
		release := &releases[i]
		if release.Title != trackTitle {
			return release
		}
	}

	// 6. If none found, return the oldest release overall (which is the first one after sorting)
	s.logger.Printf("Could not find a suitable release for '%s', picking oldest: '%s' (%s)", trackTitle, releases[0].Title, releases[0].ID)
	r := releases[0]
	return &r
}

func HydrateTrack(mb *Service, track models.Track) (*models.Track, error) {
	ctx := context.Background()
	// array of strings
	artistArray := make([]string, len(track.Artist)) // Assuming Name is string type
	for i, a := range track.Artist {
		artistArray[i] = a.Name
	}

	params := SearchParams{
		Track:   track.Name,
		Artist:  strings.Join(artistArray, ", "),
		Release: track.Album,
		ISRC:    track.ISRC,
	}
	res, err := mb.SearchMusicBrainz(ctx, params)
	if err != nil {
		return nil, err
	}

	if len(res) == 0 {
		return nil, errors.New("no results found")
	}

	firstResult := res[0]
	firstResultAlbum := mb.GetBestRelease(firstResult.Releases, firstResult.Title, track.Album)

	var firstISRC string
	if len(firstResult.ISRCs) > 0 {
		firstISRC = firstResult.ISRCs[0]
	}

	artists := make([]models.Artist, len(firstResult.ArtistCredit))

	for i, a := range firstResult.ArtistCredit {
		artists[i] = models.Artist{
			Name: a.Name,
			ID:   a.Artist.ID,
			MBID: &a.Artist.ID,
		}
	}

	resTrack := models.Track{
		HasStamped:     track.HasStamped,
		PlayID:         track.PlayID,
		Name:           track.Name,
		URL:            track.URL,
		ServiceBaseUrl: track.ServiceBaseUrl,
		RecordingMBID:  &firstResult.ID,
		ISRC:           cmp.Or(track.ISRC, firstISRC),
		Timestamp:      track.Timestamp,
		ProgressMs:     track.ProgressMs,
		DurationMs:     int64(firstResult.Length),
		Artist:         artists,
	}

	if firstResultAlbum != nil {
		resTrack.Album = firstResultAlbum.Title
		resTrack.ReleaseMBID = &firstResultAlbum.ID
	} else {
		resTrack.Album = track.Album
	}

	return &resTrack, nil
}

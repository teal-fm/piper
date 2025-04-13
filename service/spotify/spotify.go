package spotify

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/session"
)

type SpotifyService struct {
	DB         *db.DB
	userTracks map[int64]*models.Track
	userTokens map[int64]string
	mu         sync.RWMutex
}

func NewSpotifyService(database *db.DB) *SpotifyService {
	return &SpotifyService{
		DB:         database,
		userTracks: make(map[int64]*models.Track),
		userTokens: make(map[int64]string),
	}
}

func (s *SpotifyService) SetAccessToken(token string, userId int64, hasSession bool) (int64, error) {
	// Identify the user synchronously instead of in a goroutine
	userID, err := s.identifyAndStoreUser(token, userId, hasSession)
	if err != nil {
		log.Printf("Error identifying and storing user: %v", err)
		return 0, err
	}
	return userID, nil
}

func (s *SpotifyService) identifyAndStoreUser(token string, userId int64, hasSession bool) (int64, error) {
	// Get Spotify user profile
	userProfile, err := s.fetchSpotifyProfile(token)
	if err != nil {
		log.Printf("Error fetching Spotify profile: %v", err)
		return 0, err
	}

	fmt.Printf("uid: %d hasSession: %t", userId, hasSession)

	// Check if user exists
	user, err := s.DB.GetUserBySpotifyID(userProfile.ID)
	if err != nil {
		// This error might mean DB connection issue, not just user not found.
		log.Printf("Error checking for user by Spotify ID %s: %v", userProfile.ID, err)
		return 0, err
	}

	tokenExpiryTime := time.Now().Add(1 * time.Hour) // Spotify tokens last ~1 hour

	// We don't intend users to log in via spotify!
	if user == nil {
		if !hasSession {
			log.Printf("User does not seem to exist")
			return 0, fmt.Errorf("user does not seem to exist")
		} else {
			// overwrite prev user
			user, err = s.DB.AddSpotifySession(userId, userProfile.DisplayName, userProfile.Email, userProfile.ID, token, "", tokenExpiryTime)
			if err != nil {
				log.Printf("Error adding Spotify session for user ID %d: %v", userId, err)
				return 0, err
			}
		}
	} else {
		// Update existing user's token and expiry
		err = s.DB.UpdateUserToken(user.ID, token, "", tokenExpiryTime)
		if err != nil {
			log.Printf("Error updating user token for user ID %d: %v", user.ID, err)
			// Consider if we should return 0 or the user ID even if update fails
			// Sticking to original behavior: log and continue
		} else {
			log.Printf("Updated token for existing user: %s (ID: %d)", user.Username, user.ID)
		}
	}
	// Keep the local 'user' object consistent (optional but good practice)
	user.AccessToken = &token
	user.TokenExpiry = &tokenExpiryTime

	// Store token in memory cache regardless of new/existing user
	s.mu.Lock()
	s.userTokens[user.ID] = token
	s.mu.Unlock()

	log.Printf("User authenticated via Spotify: %s (ID: %d)", user.Username, user.ID)
	return user.ID, nil
}

type spotifyProfile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// LoadAllUsers loads all active users from the database into memory
func (s *SpotifyService) LoadAllUsers() error {
	users, err := s.DB.GetAllActiveUsers()
	if err != nil {
		return fmt.Errorf("error loading users: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, user := range users {
		// Only load users with valid tokens
		if user.AccessToken != nil && user.TokenExpiry.After(time.Now()) {
			s.userTokens[user.ID] = *user.AccessToken
			count++
		}
	}

	log.Printf("Loaded %d active users with valid tokens", count)
	return nil
}

func (s *SpotifyService) RefreshToken(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.DB.GetUserBySpotifyID(userID)
	if err != nil {
		return fmt.Errorf("error loading user: %v", err)
	}

	if user.RefreshToken == nil {
		return fmt.Errorf("no refresh token for user %s", userID)
	}

	// Implement token refresh logic here using Spotify's token refresh endpoint
	// This would make a request to Spotify's token endpoint with grant_type=refresh_token

	// If successful, update the database and in-memory cache
	// we won't be now so just error out
	return fmt.Errorf("token refresh not implemented")
	//
	//s.userTokens[user.ID] = newToken
	//return nil
}

// RefreshExpiredTokens attempts to refresh expired tokens
func (s *SpotifyService) RefreshExpiredTokens() {
	users, err := s.DB.GetUsersWithExpiredTokens()
	if err != nil {
		log.Printf("Error fetching users with expired tokens: %v", err)
		return
	}

	refreshed := 0
	for _, user := range users {
		// Skip users without refresh tokens
		if user.RefreshToken == nil {
			continue
		}

		// Implement token refresh logic here using Spotify's token refresh endpoint
		// This would make a request to Spotify's token endpoint with grant_type=refresh_token

		// If successful, update the database and in-memory cache
		refreshed++
	}

	if refreshed > 0 {
		log.Printf("Refreshed tokens for %d users", refreshed)
	}
}

func (s *SpotifyService) fetchSpotifyProfile(token string) (*spotifyProfile, error) {
	req, err := http.NewRequest("GET", "https://api.spotify.com/v1/me", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("spotify API error (%d): %s", resp.StatusCode, body)
	}

	var profile spotifyProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func (s *SpotifyService) HandleCurrentTrack(w http.ResponseWriter, r *http.Request) {
	userID, ok := session.GetUserID(r.Context())
	if !ok {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	track, exists := s.userTracks[userID]
	s.mu.RUnlock()

	if !exists || track == nil {
		fmt.Fprintf(w, "No track currently playing")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(track)
}

func (s *SpotifyService) HandleTrackHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := session.GetUserID(r.Context())
	if !ok {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Get recent tracks from database
	tracks, err := s.DB.GetRecentTracks(userID, 20)
	if err != nil {
		http.Error(w, "Error retrieving track history", http.StatusInternalServerError)
		log.Printf("Error retrieving track history: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tracks)
}

func (s *SpotifyService) FetchCurrentTrack(userID int64) (*models.Track, error) {
	s.mu.RLock()
	token, exists := s.userTokens[userID]
	s.mu.RUnlock()

	if !exists || token == "" {
		return nil, fmt.Errorf("no access token for user %d", userID)
	}

	// Call Spotify API to get currently playing track
	req, err := http.NewRequest("GET", "https://api.spotify.com/v1/me/player/currently-playing", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// No track playing
	if resp.StatusCode == 204 {
		return nil, nil
	}

	// Token expired
	if resp.StatusCode == 401 {
		// attempt to refresh token
		if err := s.RefreshToken(strconv.FormatInt(userID, 10)); err != nil {
			s.mu.Lock()
			delete(s.userTokens, userID)
			s.mu.Unlock()
			return nil, fmt.Errorf("spotify token expired for user %d", userID)
		}
	}

	// Error response
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("spotify API error: %s", body)
	}

	// Parse response
	var response struct {
		Item struct {
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			} `json:"artists"`
			Album struct {
				Name string `json:"name"`
			} `json:"album"`
			ExternalIDs struct {
				ISRC string `json:"isrc"`
			} `json:"external_ids"`
			ExternalURLs struct {
				Spotify string `json:"spotify"`
			} `json:"external_urls"`
			DurationMs int `json:"duration_ms"`
		} `json:"item"`
		ProgressMS int `json:"progress_ms"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}

	// Extract artist names/ids
	var artists []models.Artist
	for _, artist := range response.Item.Artists {
		artists = append(artists, models.Artist{
			Name: artist.Name,
			ID:   artist.ID,
		})
	}

	// Create Track model
	track := &models.Track{
		Name:           response.Item.Name,
		Artist:         artists,
		Album:          response.Item.Album.Name,
		URL:            response.Item.ExternalURLs.Spotify,
		DurationMs:     int64(response.Item.DurationMs),
		ProgressMs:     int64(response.ProgressMS),
		ServiceBaseUrl: "open.spotify.com",
		ISRC:           response.Item.ExternalIDs.ISRC,
		HasStamped:     false,
		Timestamp:      time.Now(),
	}

	return track, nil
}

func (s *SpotifyService) StartListeningTracker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// Copy userIDs to avoid holding the lock too long
		s.mu.RLock()
		userIDs := make([]int64, 0, len(s.userTokens))
		for userID := range s.userTokens {
			userIDs = append(userIDs, userID)
		}
		s.mu.RUnlock()

		// Check each user's currently playing track
		for _, userID := range userIDs {
			track, err := s.FetchCurrentTrack(userID)
			if err != nil {
				log.Printf("Error fetching track for user %d: %v", userID, err)
				continue
			}

			// No change if no track is playing
			if track == nil {
				continue
			}

			// Check if this is a new track
			s.mu.RLock()
			currentTrack := s.userTracks[userID]
			s.mu.RUnlock()

			if currentTrack == nil {
				currentTracks, _ := s.DB.GetRecentTracks(userID, 1)
				if len(currentTracks) > 0 {
					currentTrack = currentTracks[0]
				}
			}

			// If track is different or we've played more than either half of the track or 30 seconds since the start
			// whichever is greater
			isNewTrack := currentTrack == nil ||
				currentTrack.Name != track.Name ||
				// just check the first one for now
				currentTrack.Artist[0].Name != track.Artist[0].Name

			// we stamp a track iff we've played more than half (or 30 seconds whichever is greater)
			isStamped := track.ProgressMs > track.DurationMs/2 && track.ProgressMs > 30000

			// if currentTrack.Timestamp minus track.Timestamp is greater than 30 seconds
			isLastTrackStamped := currentTrack != nil && time.Since(currentTrack.Timestamp) > 30*time.Second &&
				currentTrack.DurationMs > 30000

			// just log when we stamp tracks
			if isNewTrack && isLastTrackStamped && !currentTrack.HasStamped {
				log.Printf("User %d stamped (previous) track: %s by %s", userID, currentTrack.Name, currentTrack.Artist)
				currentTrack.HasStamped = true
				if currentTrack.PlayID != 0 {
					s.DB.UpdateTrack(currentTrack.PlayID, currentTrack)

					log.Printf("Updated!")
				}
			}

			if isStamped && !currentTrack.HasStamped {
				log.Printf("User %d stamped track: %s by %s", userID, track.Name, track.Artist)
				track.HasStamped = true
				// if currenttrack has a playid and the last track is the same as the current track
				if !isNewTrack && currentTrack.PlayID != 0 {
					s.DB.UpdateTrack(currentTrack.PlayID, track)

					// Update in memory
					s.mu.Lock()
					s.userTracks[userID] = track
					s.mu.Unlock()

					log.Printf("Updated!")
				}
			}

			if isNewTrack {
				// Save to database
				id, err := s.DB.SaveTrack(userID, track)
				if err != nil {
					log.Printf("Error saving track for user %d: %v", userID, err)
					continue
				}

				track.PlayID = id

				// Update in memory
				s.mu.Lock()
				s.userTracks[userID] = track
				s.mu.Unlock()

				log.Printf("User %d is listening to: %s by %s", userID, track.Name, track.Artist)
			}
		}
	}
}

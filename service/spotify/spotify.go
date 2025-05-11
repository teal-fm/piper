package spotify

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"context" // Added for context.Context

	"github.com/bluesky-social/indigo/api/atproto"      // Added for atproto.RepoCreateRecord_Input
	lexutil "github.com/bluesky-social/indigo/lex/util" // Added for lexutil.LexiconTypeDecoder
	"github.com/bluesky-social/indigo/xrpc"             // Added for xrpc.Client
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/api/teal" // Added for teal.AlphaFeedPlay
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/session"
)

type SpotifyService struct {
	DB             *db.DB
	atprotoService *atprotoauth.ATprotoAuthService // Added field
	mb             *musicbrainz.MusicBrainzService // Added field
	userTracks     map[int64]*models.Track
	userTokens     map[int64]string
	mu             sync.RWMutex
}

func NewSpotifyService(database *db.DB, atprotoService *atprotoauth.ATprotoAuthService, musicBrainzService *musicbrainz.MusicBrainzService) *SpotifyService {
	return &SpotifyService{
		DB:             database,
		atprotoService: atprotoService,
		mb:             musicBrainzService,
		userTracks:     make(map[int64]*models.Track),
		userTokens:     make(map[int64]string),
	}
}

func (s *SpotifyService) SubmitTrackToPDS(did string, track *models.Track, ctx context.Context) error {
	client, err := s.atprotoService.GetATProtoClient()
	if err != nil || client == nil {
		log.Printf("Error getting ATProto client: %v", err)
		return fmt.Errorf("failed to get ATProto client: %w", err)
	}

	xrpcClient := s.atprotoService.GetXrpcClient()
	if xrpcClient == nil {
		return errors.New("xrpc client is not available")
	}

	sess, err := s.DB.GetAtprotoSession(did, ctx, *client)
	if err != nil {
		return fmt.Errorf("couldn't get Atproto session for DID %s: %w", did, err)
	}

	artistArr := make([]string, 0, len(track.Artist))
	artistMbIdArr := make([]string, 0, len(track.Artist))
	for _, a := range track.Artist {
		artistArr = append(artistArr, a.Name)
		artistMbIdArr = append(artistMbIdArr, a.MBID)
	}

	var durationPtr *int64
	if track.DurationMs > 0 {
		durationSeconds := track.DurationMs / 1000
		durationPtr = &durationSeconds
	}

	playedTimeStr := track.Timestamp.Format(time.RFC3339)
	submissionAgent := viper.GetString("app.submission_agent")
	if submissionAgent == "" {
		submissionAgent = "piper/v0.0.1" // Default if not configured
	}

	tfmTrack := teal.AlphaFeedPlay{
		LexiconTypeID: "fm.teal.alpha.feed.play",
		Duration:      durationPtr,
		TrackName:     track.Name,
		PlayedTime:    &playedTimeStr,
		ArtistNames:   artistArr,
		ArtistMbIds:   artistMbIdArr,
		ReleaseMbId:   &track.ReleaseMBID,
		ReleaseName:   &track.Album,
		RecordingMbId: &track.RecordingMBID,
		// Optional: Spotify specific data if your lexicon supports it
		// SpotifyTrackID: &track.ServiceID,
		// SpotifyAlbumID: &track.ServiceAlbumID,
		// SpotifyArtistIDs: track.ServiceArtistIDs, // Assuming this is a []string
		SubmissionClientAgent: &submissionAgent,
	}

	input := atproto.RepoCreateRecord_Input{
		Collection: "fm.teal.alpha.feed.play", // Ensure this collection is correct
		Repo:       sess.DID,
		Record:     &lexutil.LexiconTypeDecoder{Val: &tfmTrack},
	}

	authArgs := db.AtpSessionToAuthArgs(sess)

	var out atproto.RepoCreateRecord_Output
	if err := xrpcClient.Do(ctx, authArgs, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		log.Printf("Error creating record for DID %s: %v. Input: %+v", did, err, input)
		return fmt.Errorf("failed to create record on PDS for DID %s: %w", did, err)
	}

	log.Printf("Successfully submitted track '%s' to PDS for DID %s. Record URI: %s", track.Name, did, out.Uri)
	return nil
}

func (s *SpotifyService) SetAccessToken(token string, userId int64, hasSession bool) (int64, error) {
	userID, err := s.identifyAndStoreUser(token, userId, hasSession)
	if err != nil {
		log.Printf("Error identifying and storing user: %v", err)
		return 0, err
	}
	return userID, nil
}

func (s *SpotifyService) identifyAndStoreUser(token string, userId int64, hasSession bool) (int64, error) {
	userProfile, err := s.fetchSpotifyProfile(token)
	if err != nil {
		log.Printf("Error fetching Spotify profile: %v", err)
		return 0, err
	}

	fmt.Printf("uid: %d hasSession: %t", userId, hasSession)

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
		err = s.DB.UpdateUserToken(user.ID, token, "", tokenExpiryTime)
		if err != nil {
			// for now log and continue
			log.Printf("Error updating user token for user ID %d: %v", user.ID, err)
		} else {
			log.Printf("Updated token for existing user: %s (ID: %d)", *user.Username, user.ID)
		}
	}
	user.AccessToken = &token
	user.TokenExpiry = &tokenExpiryTime

	s.mu.Lock()
	s.userTokens[user.ID] = token
	s.mu.Unlock()

	log.Printf("User authenticated via Spotify: %s (ID: %d)", *user.Username, user.ID)
	return user.ID, nil
}

type spotifyProfile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

func (s *SpotifyService) LoadAllUsers() error {
	users, err := s.DB.GetAllActiveUsers()
	if err != nil {
		return fmt.Errorf("error loading users: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, user := range users {
		// load users with valid tokens
		if user.AccessToken != nil && user.TokenExpiry.After(time.Now()) {
			s.userTokens[user.ID] = *user.AccessToken
			count++
		}
	}

	log.Printf("Loaded %d active users with valid tokens", count)
	return nil
}

// refreshTokenInner handles the actual Spotify token refresh logic.
// It returns the new access token or an error.
func (s *SpotifyService) refreshTokenInner(userID int64) (string, error) {
	user, err := s.DB.GetUserByID(userID)
	if err != nil {
		return "", fmt.Errorf("error loading user %d for refresh: %w", userID, err)
	}

	if user == nil {
		return "", fmt.Errorf("user %d not found for refresh", userID)
	}

	if user.RefreshToken == nil || *user.RefreshToken == "" {
		// If no refresh token, remove potentially stale access token from cache and return error
		s.mu.Lock()
		delete(s.userTokens, userID)
		s.mu.Unlock()
		return "", fmt.Errorf("no refresh token available for user %d", userID)
	}

	clientID := viper.GetString("spotify.client_id")
	clientSecret := viper.GetString("spotify.client_secret")
	if clientID == "" || clientSecret == "" {
		return "", errors.New("spotify client ID or secret not configured")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", *user.RefreshToken)

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create refresh request: %w", err)
	}

	authHeader := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", fmt.Errorf("failed to read refresh response body: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		// If refresh fails (e.g., bad refresh token), remove tokens from cache
		s.mu.Lock()
		delete(s.userTokens, userID)
		s.mu.Unlock()
		// Also clear the bad refresh token from the DB
		updateErr := s.DB.UpdateUserToken(userID, "", "", time.Now()) // Clear tokens
		if updateErr != nil {
			log.Printf("Failed to clear bad refresh token for user %d: %v", userID, updateErr)
		}
		return "", fmt.Errorf("spotify token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`              // Seconds
		RefreshToken string `json:"refresh_token,omitempty"` // Spotify might issue a new refresh token
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("failed to decode refresh response: %w", err)
	}

	newExpiry := time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	newRefreshToken := *user.RefreshToken // Default to old one
	if tokenResponse.RefreshToken != "" {
		newRefreshToken = tokenResponse.RefreshToken // Use new one if provided
	}

	// Update DB
	if err := s.DB.UpdateUserToken(userID, tokenResponse.AccessToken, newRefreshToken, newExpiry); err != nil {
		// Log error but continue, as we have the token in memory
		log.Printf("Error updating user token in DB for user %d after refresh: %v", userID, err)
	}

	// Update in-memory cache
	s.mu.Lock()
	s.userTokens[userID] = tokenResponse.AccessToken
	s.mu.Unlock()

	log.Printf("Successfully refreshed token for user %d", userID)
	return tokenResponse.AccessToken, nil
}

// RefreshToken attempts to refresh the token for a given user ID.
// It's less commonly needed now refreshTokenInner handles fetching the user.
func (s *SpotifyService) RefreshToken(userID int64) error {
	_, err := s.refreshTokenInner(userID)
	return err
}

// attempt to refresh expired tokens
func (s *SpotifyService) RefreshExpiredTokens() {
	users, err := s.DB.GetUsersWithExpiredTokens()
	if err != nil {
		log.Printf("Error fetching users with expired tokens: %v", err)
		return
	}

	refreshed := 0
	for _, user := range users {
		// skip users without refresh tokens
		if user.RefreshToken == nil {
			continue
		}

		_, err := s.refreshTokenInner(user.ID)

		if err != nil {
			// just print out errors here for now
			log.Printf("Error from service/spotify/spotify.go when refreshing tokens: %s", err.Error())
		}

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

	req, rErr := http.NewRequest("GET", "https://api.spotify.com/v1/me/player/currently-playing", nil)
	if rErr != nil {
		return nil, rErr
	}

	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	var resp *http.Response
	var err error

	// Retry logic: try once, if 401, refresh and try again
	for attempt := range 2 {
		// We need to be able to re-read the body if the request is retried,
		// but since this is a GET request with no body, we don't need to worry about it.
		resp, err = client.Do(req) // Use = instead of := inside loop
		if err != nil {
			// Network or other client error, don't retry
			return nil, fmt.Errorf("failed to execute spotify request on attempt %d: %w", attempt+1, err)
		}
		// Defer close inside the loop IF we continue, otherwise close after the loop
		// Simplified: Always defer close, it's idempotent for nil resp.Body
		// defer resp.Body.Close() // Moved defer outside loop to avoid potential issues

		// oops, token expired or other client error
		if resp.StatusCode == 401 && attempt == 0 { // Only refresh on 401 on the first attempt
			log.Printf("Spotify token potentially expired for user %d, attempting refresh...", userID)
			newAccessToken, refreshErr := s.refreshTokenInner(userID)
			if refreshErr != nil {
				log.Printf("Token refresh failed for user %d: %v", userID, refreshErr)
				// No point retrying if refresh failed
				return nil, fmt.Errorf("spotify token expired or invalid for user %d and refresh failed: %w", userID, refreshErr)
			}
			log.Printf("Token refreshed for user %d, retrying request...", userID)
			token = newAccessToken                           // Update token for the next attempt
			req.Header.Set("Authorization", "Bearer "+token) // Update header for retry
			continue                                         // Go to next attempt in the loop
		}

		// If it's not 200 or 204, or if it's 401 on the second attempt, break and return error
		if resp.StatusCode != 200 && resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("spotify API error (%d) for user %d after %d attempts: %s", resp.StatusCode, userID, attempt+1, string(body))
		}

		// If status is 200 or 204, break the loop, we have a valid response (or no content)
		break
	} // End of retry loop

	// Ensure body is closed regardless of loop outcome
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	// Handle final response after loop
	if resp == nil {
		// This should ideally not happen if client.Do succeeded but we check defensively
		return nil, fmt.Errorf("spotify request failed with no response after retries")
	}
	if resp.StatusCode == 204 {
		return nil, nil // Nothing playing
	}

	// Read body now that we know it's a successful 200
	bodyBytes, err := io.ReadAll(resp.Body) // Read the already fetched successful response body
	if err != nil {
		return nil, fmt.Errorf("failed to read successful spotify response body: %w", err)
	}

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

	err = json.Unmarshal(bodyBytes, &response) // Use bodyBytes here
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal spotify response: %w", err)
	}

	body, ioErr := io.ReadAll(resp.Body)
	if ioErr != nil {
		return nil, ioErr
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}

	var artists []models.Artist
	for _, artist := range response.Item.Artists {
		artists = append(artists, models.Artist{
			Name: artist.Name,
			ID:   artist.ID,
		})
	}

	// assemble Track
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
		// copy userIDs to avoid holding the lock too long
		s.mu.RLock()
		userIDs := make([]int64, 0, len(s.userTokens))
		for userID := range s.userTokens {
			userIDs = append(userIDs, userID)
		}
		s.mu.RUnlock()

		for _, userID := range userIDs {
			track, err := s.FetchCurrentTrack(userID)
			if err != nil {
				log.Printf("Error fetching track for user %d: %v", userID, err)
				continue
			}

			if track == nil {
				continue
			}

			s.mu.RLock()
			currentTrack := s.userTracks[userID]
			s.mu.RUnlock()

			if currentTrack == nil {
				currentTracks, _ := s.DB.GetRecentTracks(userID, 1)
				if len(currentTracks) > 0 {
					currentTrack = currentTracks[0]
				}
			}

			// if flagged true, we have a new track
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
				id, err := s.DB.SaveTrack(userID, track)
				if err != nil {
					log.Printf("Error saving track for user %d: %v", userID, err)
					continue
				}

				track.PlayID = id

				s.mu.Lock()
				s.userTracks[userID] = track
				s.mu.Unlock()

				// Submit to ATProto PDS
				// The 'track' variable is *models.Track and has been saved to DB, PlayID is populated.
				dbUser, errUser := s.DB.GetUserByID(userID) // Fetch user by their internal ID
				if errUser != nil {
					log.Printf("User %d: Error fetching user details for PDS submission: %v", userID, errUser)
				} else if dbUser == nil {
					log.Printf("User %d: User not found in DB. Skipping PDS submission.", userID)
				} else if dbUser.ATProtoDID == nil || *dbUser.ATProtoDID == "" {
					log.Printf("User %d (%d): ATProto DID not set. Skipping PDS submission for track '%s'.", userID, dbUser.ATProtoDID, track.Name)
				} else {
					// User has a DID, proceed with hydration and submission
					var trackToSubmitToPDS *models.Track = track // Default to the original track (already *models.Track)
					if s.mb != nil {                             // Check if MusicBrainz service is available
						// musicbrainz.HydrateTrack expects models.Track as second argument, so we pass *track
						// and it returns *models.Track
						hydratedTrack, errHydrate := musicbrainz.HydrateTrack(s.mb, *track)
						if errHydrate != nil {
							log.Printf("User %d (%d): Error hydrating track '%s' with MusicBrainz: %v. Proceeding with original track data for PDS.", userID, dbUser.ATProtoDID, track.Name, errHydrate)
						} else {
							log.Printf("User %d (%d): Successfully hydrated track '%s' with MusicBrainz.", userID, dbUser.ATProtoDID, track.Name)
							trackToSubmitToPDS = hydratedTrack // hydratedTrack is *models.Track
						}
					} else {
						log.Printf("User %d (%d): MusicBrainz service not configured. Proceeding with original track data for PDS.", userID, dbUser.ATProtoDID)
					}

					artistName := "Unknown Artist"
					if len(trackToSubmitToPDS.Artist) > 0 {
						artistName = trackToSubmitToPDS.Artist[0].Name
					}

					log.Printf("User %d (%d): Attempting to submit track '%s' by %s to PDS (DID: %s)", userID, dbUser.ATProtoDID, trackToSubmitToPDS.Name, artistName, *dbUser.ATProtoDID)
					// Use context.Background() for now, or pass down a context if available
					if errPDS := s.SubmitTrackToPDS(*dbUser.ATProtoDID, trackToSubmitToPDS, context.Background()); errPDS != nil {
						log.Printf("User %d (%d): Error submitting track '%s' to PDS: %v", userID, dbUser.ATProtoDID, trackToSubmitToPDS.Name, errPDS)
					} else {
						log.Printf("User %d (%d): Successfully submitted track '%s' to PDS.", userID, dbUser.ATProtoDID, trackToSubmitToPDS.Name)
					}
				}
				// End of PDS submission block

				log.Printf("User %d is listening to: %s by %s", userID, track.Name, track.Artist)
			}
		}
	}
}

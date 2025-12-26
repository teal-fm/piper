package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/db/apikey"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/pages"
	"github.com/teal-fm/piper/service/applemusic"
	atprotoservice "github.com/teal-fm/piper/service/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/playingnow"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

type HomeParams struct {
	NavBar pages.NavBar
}

func home(database *db.DB, pg *pages.Pages) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/html")

		userID, authenticated := session.GetUserID(r.Context())
		isLoggedIn := authenticated
		lastfmUsername := ""

		if isLoggedIn {
			user, err := database.GetUserByID(userID)
			fmt.Printf("User: %+v\n", user)
			if err == nil && user != nil && user.LastFMUsername != nil {
				lastfmUsername = *user.LastFMUsername
			} else if err != nil {
				log.Printf("Error fetching user %d details for home page: %v", userID, err)
			}
		}
		params := HomeParams{
			NavBar: pages.NavBar{
				IsLoggedIn:     isLoggedIn,
				LastFMUsername: lastfmUsername,
			},
		}
		err := pg.Execute("home", w, params)
		if err != nil {
			log.Printf("Error executing template: %v", err)
		}
	}
}

func handleLinkLastfmForm(database *db.DB, pg *pages.Pages) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, authenticated := session.GetUserID(r.Context())
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}

			lastfmUsername := r.FormValue("lastfm_username")
			if lastfmUsername == "" {
				http.Error(w, "Last.fm username cannot be empty", http.StatusBadRequest)
				return
			}

			err := database.AddLastFMUsername(userID, lastfmUsername)
			if err != nil {
				log.Printf("Error saving Last.fm username for user %d: %v", userID, err)
				http.Error(w, "Failed to save Last.fm username", http.StatusInternalServerError)
				return
			}

			log.Printf("Successfully linked Last.fm username '%s' for user ID %d", lastfmUsername, userID)

			http.Redirect(w, r, "/", http.StatusSeeOther)
		}

		currentUser, err := database.GetUserByID(userID)
		currentUsername := ""
		if err == nil && currentUser != nil && currentUser.LastFMUsername != nil {
			currentUsername = *currentUser.LastFMUsername
		} else if err != nil {
			log.Printf("Error fetching user %d for Last.fm form: %v", userID, err)
			// Don't fail, just show an empty form
		}

		w.Header().Set("Content-Type", "text/html")

		pageParams := struct {
			NavBar          pages.NavBar
			CurrentUsername string
		}{
			NavBar: pages.NavBar{
				IsLoggedIn:     authenticated,
				LastFMUsername: currentUsername,
			},
			CurrentUsername: currentUsername,
		}
		err = pg.Execute("lastFMForm", w, pageParams)
		if err != nil {
			log.Printf("Error executing template: %v", err)
		}
	}
}

func handleLinkLastfmSubmit(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context()) // Auth middleware ensures this exists

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		lastfmUsername := r.FormValue("lastfm_username")
		if lastfmUsername == "" {
			http.Error(w, "Last.fm username cannot be empty", http.StatusBadRequest)
			return
		}

		err := database.AddLastFMUsername(userID, lastfmUsername)
		if err != nil {
			log.Printf("Error saving Last.fm username for user %d: %v", userID, err)
			http.Error(w, "Failed to save Last.fm username", http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully linked Last.fm username '%s' for user ID %d", lastfmUsername, userID)

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleAppleMusicLink(pg *pages.Pages, am *applemusic.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		devToken, _, errTok := am.GenerateDeveloperToken()
		if errTok != nil {
			log.Printf("Error generating Apple Music developer token: %v", errTok)
			http.Error(w, "Failed to prepare Apple Music", http.StatusInternalServerError)
			return
		}
		data := struct {
			NavBar   pages.NavBar
			DevToken string
		}{DevToken: devToken}
		err := pg.Execute("applemusic_link", w, data)
		if err != nil {
			log.Printf("Error executing template: %v", err)
		}
	}
}

func apiCurrentTrack(spotifyService *spotify.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := session.GetUserID(r.Context())
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		tracks, err := spotifyService.DB.GetRecentTracks(userID, 1)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get current track: " + err.Error()})
			return
		}

		if len(tracks) == 0 {
			jsonResponse(w, http.StatusOK, nil)
			return
		}

		jsonResponse(w, http.StatusOK, tracks[0])
	}
}

func apiTrackHistory(spotifyService *spotify.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := session.GetUserID(r.Context())
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		limitStr := r.URL.Query().Get("limit")
		limit := 50 // Default limit
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		if limit > 200 {
			limit = 200
		}

		tracks, err := spotifyService.DB.GetRecentTracks(userID, limit)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get track history: " + err.Error()})
			return
		}

		jsonResponse(w, http.StatusOK, tracks)
	}
}

func apiMusicBrainzSearch(mbService *musicbrainz.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mbService == nil {
			jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "MusicBrainz service is not available"})
			return
		}

		params := musicbrainz.SearchParams{
			Track:   r.URL.Query().Get("track"),
			Artist:  r.URL.Query().Get("artist"),
			Release: r.URL.Query().Get("release"),
		}

		if params.Track == "" && params.Artist == "" && params.Release == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "At least one query parameter (track, artist, release) is required"})
			return
		}

		recordings, err := mbService.SearchMusicBrainz(r.Context(), params)
		if err != nil {
			log.Printf("Error searching MusicBrainz: %v", err) // Log the error
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to search MusicBrainz"})
			return
		}

		jsonResponse(w, http.StatusOK, recordings)
	}
}

func apiMeHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, authenticated := session.GetUserID(r.Context())
		if !authenticated {
			jsonResponse(w, http.StatusUnauthorized, map[string]any{"authenticated": false, "error": "Unauthorized"})
			return
		}

		user, err := database.GetUserByID(userID)
		if err != nil {
			log.Printf("apiMeHandler: Error fetching user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve user information"})
			return
		}
		if user == nil {
			jsonResponse(w, http.StatusNotFound, map[string]string{"error": "User not found"})
			return
		}

		lastfmUsername := ""
		if user.LastFMUsername != nil {
			lastfmUsername = *user.LastFMUsername
		}

		spotifyConnected := user.SpotifyID != nil

		response := map[string]any{
			"authenticated":     true,
			"user_id":           user.ID,
			"did":               user.ATProtoDID,
			"lastfm_username":   lastfmUsername,
			"spotify_connected": spotifyConnected,
		}
		// do not send Apple token value; just whether present
		response["applemusic_linked"] = user.AppleMusicUserToken != nil && *user.AppleMusicUserToken != ""
		if user.LastFMUsername == nil {
			response["lastfm_username"] = nil
		}

		jsonResponse(w, http.StatusOK, response)
	}
}

func apiGetLastfmUserHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context()) // Auth middleware ensures user is present
		user, err := database.GetUserByID(userID)
		if err != nil {
			log.Printf("apiGetLastfmUserHandler: Error fetching user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve user information"})
			return
		}
		if user == nil {
			jsonResponse(w, http.StatusNotFound, map[string]string{"error": "User not found"})
			return
		}

		var lastfmUsername *string
		if user.LastFMUsername != nil {
			lastfmUsername = user.LastFMUsername
		}
		jsonResponse(w, http.StatusOK, map[string]*string{"lastfm_username": lastfmUsername})
	}
}

func apiLinkLastfmHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context())

		var reqBody struct {
			LastFMUsername string `json:"lastfm_username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body: " + err.Error()})
			return
		}

		if reqBody.LastFMUsername == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Last.fm username cannot be empty"})
			return
		}

		err := database.AddLastFMUsername(userID, reqBody.LastFMUsername)
		if err != nil {
			log.Printf("apiLinkLastfmHandler: Error saving Last.fm username for user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save Last.fm username"})
			return
		}
		log.Printf("API: Successfully linked Last.fm username '%s' for user ID %d", reqBody.LastFMUsername, userID)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "Last.fm username updated successfully"})
	}
}

func apiUnlinkLastfmHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context())

		// TODO: add a clear username for user id fn
		err := database.AddLastFMUsername(userID, "")
		if err != nil {
			log.Printf("apiUnlinkLastfmHandler: Error unlinking Last.fm username for user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to unlink Last.fm username"})
			return
		}
		log.Printf("API: Successfully unlinked Last.fm username for user ID %d", userID)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "Last.fm username unlinked successfully"})
	}
}

// apiAppleMusicAuthorize stores a MusicKit user token for the current user
func apiAppleMusicAuthorize(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, authenticated := session.GetUserID(r.Context())
		if !authenticated {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		var req struct {
			UserToken string `json:"userToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
			return
		}
		if req.UserToken == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "userToken is required"})
			return
		}

		if err := database.UpdateAppleMusicUserToken(userID, req.UserToken); err != nil {
			log.Printf("apiAppleMusicAuthorize: failed to save token for user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save token"})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// apiAppleMusicUnlink clears the MusicKit user token for the current user
func apiAppleMusicUnlink(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, authenticated := session.GetUserID(r.Context())
		if !authenticated {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		if err := database.ClearAppleMusicUserToken(userID); err != nil {
			log.Printf("apiAppleMusicUnlink: failed to clear token for user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to unlink Apple Music"})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// apiSubmitListensHandler handles ListenBrainz-compatible submissions
func apiSubmitListensHandler(database *db.DB, atprotoService *atprotoauth.AuthService, playingNowService *playingnow.Service, mbService *musicbrainz.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, authenticated := session.GetUserID(r.Context())
		if !authenticated {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		// Parse the ListenBrainz submission
		var submission models.ListenBrainzSubmission
		if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
			log.Printf("apiSubmitListensHandler: Error decoding submission for user %d: %v", userID, err)
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON format"})
			return
		}

		// Validate listen_type
		validListenTypes := map[string]bool{
			"single":      true,
			"import":      true,
			"playing_now": true,
		}
		if !validListenTypes[submission.ListenType] {
			jsonResponse(w, http.StatusBadRequest, map[string]string{
				"error": "Invalid listen_type. Must be 'single', 'import', or 'playing_now'",
			})
			return
		}

		// Validate payload
		if len(submission.Payload) == 0 {
			jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Payload cannot be empty"})
			return
		}

		// Get user for PDS submission
		user, err := database.GetUserByID(userID)
		if err != nil {
			log.Printf("apiSubmitListensHandler: Error getting user %d: %v", userID, err)
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get user"})
			return
		}

		// Process each listen in the payload
		var processedTracks []models.Track
		var errors []string

		for i, listen := range submission.Payload {
			// Validate required fields
			if listen.TrackMetadata.ArtistName == "" {
				errors = append(errors, fmt.Sprintf("payload[%d]: artist_name is required", i))
				continue
			}
			if listen.TrackMetadata.TrackName == "" {
				errors = append(errors, fmt.Sprintf("payload[%d]: track_name is required", i))
				continue
			}

			// Convert to internal Track format
			track := listen.ConvertToTrack()

			// Attempt to hydrate with MusicBrainz data if service is available and track doesn't have MBIDs
			if mbService != nil && track.RecordingMBID == nil {
				hydratedTrack, err := musicbrainz.HydrateTrack(mbService, track)
				if err != nil {
					log.Printf("apiSubmitListensHandler: Could not hydrate track with MusicBrainz for user %d: %v (continuing with original data)", userID, err)
					// Continue with non-hydrated track
				} else if hydratedTrack != nil {
					track = *hydratedTrack
					log.Printf("apiSubmitListensHandler: Successfully hydrated track '%s' with MusicBrainz data", track.Name)
				}
			}

			// For 'playing_now' type, publish to PDS as actor status
			if submission.ListenType == "playing_now" {
				log.Printf("Received playing_now listen for user %d: %s - %s", userID, track.Artist[0].Name, track.Name)

				if user.ATProtoDID != nil && playingNowService != nil {
					if err := playingNowService.PublishPlayingNow(r.Context(), userID, &track); err != nil {
						log.Printf("apiSubmitListensHandler: Error publishing playing_now to PDS for user %d: %v", userID, err)
						// Don't fail the request, just log the error
					}
				}
				continue
			}

			// Store the track
			if _, err := database.SaveTrack(userID, &track); err != nil {
				log.Printf("apiSubmitListensHandler: Error saving track for user %d: %v", userID, err)
				errors = append(errors, fmt.Sprintf("payload[%d]: failed to save track", i))
				continue
			}

			// Submit to PDS as feed.play record
			if user.ATProtoDID != nil && atprotoService != nil {
				if err := atprotoservice.SubmitPlayToPDS(r.Context(), *user.ATProtoDID, *user.MostRecentAtProtoSessionID, &track, atprotoService); err != nil {
					log.Printf("apiSubmitListensHandler: Error submitting play to PDS for user %d: %v", userID, err)
					// Don't fail the request, just log the error
				}
			}

			processedTracks = append(processedTracks, track)
		}

		// Prepare response
		response := map[string]interface{}{
			"status":    "ok",
			"processed": len(processedTracks),
		}

		if len(errors) > 0 {
			response["errors"] = errors
			if len(processedTracks) == 0 {
				jsonResponse(w, http.StatusBadRequest, response)
				return
			}
		}

		log.Printf("Successfully processed %d ListenBrainz submissions for user %d (type: %s)",
			len(processedTracks), userID, submission.ListenType)

		jsonResponse(w, http.StatusOK, response)
	}
}

// apiMbTokenValidateHandler handles ListenBrainz token validation requests
func apiMbTokenValidateHandler(sm *session.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKeyStr, apiKeyErr := apikey.ExtractApiKey(r)

		if apiKeyErr != nil || apiKeyStr == "" {
			jsonResponse(w, http.StatusBadRequest, map[string]any{
				"code":    400,
				"message": "you need to specify a token",
				"valid":   false,
			})
			return
		}

		key, valid := sm.ApiKeyMgr.GetApiKey(apiKeyStr)
		if !valid {
			jsonResponse(w, http.StatusUnauthorized, map[string]any{
				"code":    401,
				"message": "invalid token",
				"valid":   false,
			})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"code":      200,
			"message":   "token valid",
			"valid":     true,
			"user_name": key.Name, // this is required to be set for pano scrobbler
		})
	}
}

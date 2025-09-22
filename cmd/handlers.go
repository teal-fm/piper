package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

func home(database *db.DB, pages *Pages) http.HandlerFunc {
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

		html := `
		<html>
		<head>
			<title>Piper - Spotify & Last.fm Tracker</title>
			<style>
				body {
					font-family: Arial, sans-serif;
					max-width: 800px;
					margin: 0 auto;
					padding: 20px;
					line-height: 1.6;
				}
				h1 {
					color: #1DB954; /* Spotify green */
				}
				.nav {
					display: flex;
					flex-wrap: wrap; /* Allow wrapping on smaller screens */
					margin-bottom: 20px;
				}
				.nav a {
					margin-right: 15px;
					margin-bottom: 5px; /* Add spacing below links */
					text-decoration: none;
					color: #1DB954;
					font-weight: bold;
				}
				.card {
					border: 1px solid #ddd;
					border-radius: 8px;
					padding: 20px;
					margin-bottom: 20px;
				}
				.service-status {
					font-style: italic;
					color: #555;
				}
			</style>
		</head>
		<body>
			<h1>Piper - Multi-User Spotify & Last.fm Tracker via ATProto</h1>
			<div class="nav">
				<a href="/">Home</a>`

		if isLoggedIn {
			html += `
				<a href="/current-track">Spotify Current</a>
				<a href="/history">Spotify History</a>
				<a href="/link-lastfm">Link Last.fm</a>` // Link to Last.fm page
			if lastfmUsername != "" {
				html += ` <a href="/lastfm/recent">Last.fm Recent</a>` // Show only if linked
			}
			html += `
				<a href="/api-keys">API Keys</a>
				<a href="/login/spotify">Connect Spotify Account</a>
				<a href="/logout">Logout</a>`
		} else {
			html += `
				<a href="/login/atproto">Login with ATProto</a>`
		}

		html += `
			</div>

			<div class="card">
				<h2>Welcome to Piper</h2>
				<p>Piper is a multi-user application that records what you're listening to on Spotify and Last.fm, saving your listening history.</p>`

		if !isLoggedIn {
			html += `
				<p>Login with ATProto to get started!</p>
				<form action="/login/atproto">
					<label for="handle">handle:</label>
					<input type="text" id="handle" name="handle" >
					<input type="submit" value="submit">
				</form>`
		} else {
			html += `
				<p>You're logged in!</p>
				<ul>
					<li><a href="/login/spotify">Connect your Spotify account</a> to start tracking.</li>
					<li><a href="/link-lastfm">Link your Last.fm account</a> to track scrobbles.</li>
				</ul>
				<p>Once connected, you can check out your:</p>
				<ul>
					<li><a href="/current-track">Spotify current track</a> or <a href="/history">listening history</a>.</li>`
			if lastfmUsername != "" {
				html += `<li><a href="/lastfm/recent">Last.fm recent tracks</a>.</li>`
			}
			html += `
				</ul>
				<p>You can also manage your <a href="/api-keys">API keys</a> for programmatic access.</p>`
			if lastfmUsername != "" {
				html += fmt.Sprintf("<p class='service-status'>Last.fm Username: %s</p>", lastfmUsername)
			} else {
				html += "<p class='service-status'>Last.fm account not linked.</p>"
			}

		}

		html += `
			</div> <!-- Close card div -->
		</body>
		</html>
	`
		pages.execute("home", w, nil)

		//w.Write([]byte(html))
	}
}

func handleLinkLastfmForm(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context())
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
		fmt.Fprintf(w, `
			<html>
			<head><title>Link Last.fm Account</title>
				<style>
					body { font-family: Arial, sans-serif; max-width: 600px; margin: 20px auto; padding: 20px; border: 1px solid #ddd; border-radius: 8px; }
					label, input { display: block; margin-bottom: 10px; }
					input[type='text'] { width: 95%%; padding: 8px; } /* Corrected width */
					input[type='submit'] { padding: 10px 15px; background-color: #d51007; color: white; border: none; border-radius: 4px; cursor: pointer; }
					.nav { margin-bottom: 20px; }
					.nav a { margin-right: 10px; text-decoration: none; color: #1DB954; font-weight: bold; }
					.error { color: red; margin-bottom: 10px; }
				</style>
			</head>
			<body>
				<div class="nav">
					<a href="/">Home</a>
					<a href="/link-lastfm">Link Last.fm</a>
					<a href="/logout">Logout</a>
				</div>
				<h2>Link Your Last.fm Account</h2>
				<p>Enter your Last.fm username to start tracking your scrobbles.</p>
				<form method="post" action="/link-lastfm">
					<label for="lastfm_username">Last.fm Username:</label>
					<input type="text" id="lastfm_username" name="lastfm_username" value="%s" required>
					<input type="submit" value="Save Username">
				</form>
			</body>
			</html>`, currentUsername)
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

func apiCurrentTrack(spotifyService *spotify.SpotifyService) http.HandlerFunc {
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

func apiTrackHistory(spotifyService *spotify.SpotifyService) http.HandlerFunc {
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

func apiMusicBrainzSearch(mbService *musicbrainz.MusicBrainzService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

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

// apiSubmitListensHandler handles ListenBrainz-compatible submissions
func apiSubmitListensHandler(database *db.DB) http.HandlerFunc {
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
			track := listen.ConvertToTrack(userID)

			// For 'playing_now' type, we might want to handle differently
			// For now, treat all the same but could add temporary storage later
			if submission.ListenType == "playing_now" {
				log.Printf("Received playing_now listen for user %d: %s - %s", userID, track.Artist[0].Name, track.Name)
				// Could store in a separate playing_now table or just log
				continue
			}

			// Store the track
			if _, err := database.SaveTrack(userID, &track); err != nil {
				log.Printf("apiSubmitListensHandler: Error saving track for user %d: %v", userID, err)
				errors = append(errors, fmt.Sprintf("payload[%d]: failed to save track", i))
				continue
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

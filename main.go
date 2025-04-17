package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/viper"
	"github.com/teal-fm/piper/config"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/oauth"
	"github.com/teal-fm/piper/oauth/atproto"
	apikeyService "github.com/teal-fm/piper/service/apikey"
	"github.com/teal-fm/piper/service/lastfm"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

func home(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/html")

		userID, authenticated := session.GetUserID(r.Context())
		isLoggedIn := authenticated
		lastfmUsername := ""

		if isLoggedIn {
			user, err := database.GetUserByID(userID)
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
				<p><a href="/login/atproto">Login with ATProto</a> to get started!</p>`
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

		w.Write([]byte(html))
	}
}

func handleLinkLastfmForm(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := session.GetUserID(r.Context()) // Auth middleware ensures this exists

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

// JSON API handlers

func jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		json.NewEncoder(w).Encode(data)
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

func main() {
	config.Load()

	database, err := db.New(viper.GetString("db.path"))
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	if err := database.Initialize(); err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}

	// --- Service Initializations ---
	jwksBytes, err := os.ReadFile("./jwks.json")
	if err != nil {
		log.Fatalf("Error reading JWK file: %v", err)
	}
	jwks, err := atproto.LoadJwks(jwksBytes)
	if err != nil {
		log.Fatalf("Error loading JWK: %v", err)
	}
	atprotoService, err := atproto.NewATprotoAuthService(
		database,
		jwks,
		viper.GetString("atproto.client_id"),
		viper.GetString("atproto.callback_url"),
	)
	if err != nil {
		log.Fatalf("Error creating ATproto auth service: %v", err)
	}

	mbService := musicbrainz.NewMusicBrainzService(database)
	spotifyService := spotify.NewSpotifyService(database, atprotoService, mbService)
	lastfmService := lastfm.NewLastFMService(database, viper.GetString("lastfm.api_key"))

	sessionManager := session.NewSessionManager()
	oauthManager := oauth.NewOAuthServiceManager()

	spotifyOAuth := oauth.NewOAuth2Service(
		viper.GetString("spotify.client_id"),
		viper.GetString("spotify.client_secret"),
		viper.GetString("callback.spotify"),
		viper.GetStringSlice("spotify.scopes"),
		"spotify",
		spotifyService,
	)
	oauthManager.RegisterService("spotify", spotifyOAuth)
	oauthManager.RegisterService("atproto", atprotoService)

	apiKeyService := apikeyService.NewAPIKeyService(database, sessionManager)

	http.HandleFunc("/", session.WithPossibleAuth(home(database), sessionManager))

	// OAuth Routes
	http.HandleFunc("/login/spotify", oauthManager.HandleLogin("spotify"))
	http.HandleFunc("/callback/spotify", session.WithPossibleAuth(oauthManager.HandleCallback("spotify"), sessionManager)) // Use possible auth
	http.HandleFunc("/login/atproto", oauthManager.HandleLogin("atproto"))
	http.HandleFunc("/callback/atproto", session.WithPossibleAuth(oauthManager.HandleCallback("atproto"), sessionManager)) // Use possible auth

	// Authenticated Web Routes
	http.HandleFunc("/current-track", session.WithAuth(spotifyService.HandleCurrentTrack, sessionManager))
	http.HandleFunc("/history", session.WithAuth(spotifyService.HandleTrackHistory, sessionManager))
	http.HandleFunc("/api-keys", session.WithAuth(apiKeyService.HandleAPIKeyManagement, sessionManager))
	http.HandleFunc("/link-lastfm", session.WithAuth(handleLinkLastfmForm(database), sessionManager))          // GET form
	http.HandleFunc("/link-lastfm/submit", session.WithAuth(handleLinkLastfmSubmit(database), sessionManager)) // POST submit - Changed route slightly
	http.HandleFunc("/logout", sessionManager.HandleLogout)                                                    // Logout doesn't strictly need auth middleware, but handles session deletion

	http.HandleFunc("/api/v1/current-track", session.WithAPIAuth(apiCurrentTrack(spotifyService), sessionManager)) // Spotify Current
	http.HandleFunc("/api/v1/history", session.WithAPIAuth(apiTrackHistory(spotifyService), sessionManager))       // Spotify History
	http.HandleFunc("/api/v1/musicbrainz/search", apiMusicBrainzSearch(mbService))                                 // MusicBrainz (public?)

	serverUrlRoot := viper.GetString("server.root_url")
	atpClientId := viper.GetString("atproto.client_id")
	atpCallbackUrl := viper.GetString("atproto.callback_url")
	http.HandleFunc("/.well-known/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		atprotoService.HandleClientMetadata(w, r, serverUrlRoot, atpClientId, atpCallbackUrl)
	})
	http.HandleFunc("/oauth/jwks.json", atprotoService.HandleJwks)
	trackerInterval := time.Duration(viper.GetInt("tracker.interval")) * time.Second
	lastfmInterval := time.Duration(viper.GetInt("lastfm.interval_seconds")) * time.Second // Add config for Last.fm interval
	if lastfmInterval <= 0 {
		lastfmInterval = 1 * time.Minute
	}

	if err := spotifyService.LoadAllUsers(); err != nil {
		log.Printf("Warning: Failed to preload Spotify users: %v", err)
	}
	go spotifyService.StartListeningTracker(trackerInterval)

	go lastfmService.StartListeningTracker(lastfmInterval)

	serverAddr := fmt.Sprintf("%s:%s", viper.GetString("server.host"), viper.GetString("server.port"))
	fmt.Printf("Server running at: http://%s\n", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}

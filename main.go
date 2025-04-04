package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/teal-fm/piper/config"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/oauth"
	apikeyService "github.com/teal-fm/piper/service/apikey"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Check if user is logged in
	cookie, err := r.Cookie("session")
	isLoggedIn := err == nil && cookie != nil

	html := `
		<html>
		<head>
			<title>Piper - Spotify Tracker</title>
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
					margin-bottom: 20px;
				}
				.nav a {
					margin-right: 15px;
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
			</style>
		</head>
		<body>
			<h1>Piper - Multi-User Spotify Tracker</h1>
			<div class="nav">
				<a href="/">Home</a>`

	if isLoggedIn {
		html += `
				<a href="/current-track">Current Track</a>
				<a href="/history">Track History</a>
				<a href="/api-keys">API Keys</a>
				<a href="/logout">Logout</a>`
	} else {
		html += `
				<a href="/login/spotify">Login with Spotify</a>`
	}

	html += `
			</div>

			<div class="card">
				<h2>Welcome to Piper</h2>
				<p>Piper is a multi-user Spotify tracking application that records what you're listening to and saves your listening history.</p>`

	if !isLoggedIn {
		html += `
				<p><a href="/login/spotify">Login with Spotify</a> to get started!</p>`
	} else {
		html += `
				<p>You're logged in! Check out your <a href="/current-track">current track</a> or view your <a href="/history">listening history</a>.</p>
				<p>You can also manage your <a href="/api-keys">API keys</a> for programmatic access.</p>`
	}

	html += `
		</body>
		</html>
	`

	w.Write([]byte(html))
}

// JSON API handlers

// jsonResponse returns a JSON response
func jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// API endpoint for current track
func apiCurrentTrack(spotifyService *spotify.SpotifyService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := session.GetUserID(r.Context())
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		track, err := spotifyService.DB.GetRecentTracks(userID, 1)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		jsonResponse(w, http.StatusOK, track)
	}
}

// API endpoint for history
func apiTrackHistory(spotifyService *spotify.SpotifyService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := session.GetUserID(r.Context())
		if !ok {
			jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		limit := 50 // Default limit
		tracks, err := spotifyService.DB.GetRecentTracks(userID, limit)
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		jsonResponse(w, http.StatusOK, tracks)
	}
}

func main() {
	config.Load()

	// create data folder if not exists with proper perms
	os.Mkdir("./data", 755)

	database, err := db.New(viper.GetString("db.path"))
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	if err := database.Initialize(); err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}

	oauthManager := oauth.NewOAuthServiceManager()

	spotifyOAuth := oauth.NewOAuth2Service(
		viper.GetString("spotify.client_id"),
		viper.GetString("spotify.client_secret"),
		viper.GetString("callback.spotify"),
		viper.GetStringSlice("spotify.scopes"),
		"spotify",
	)
	oauthManager.RegisterOAuth2Service("spotify", spotifyOAuth)

	spotifyService := spotify.NewSpotifyService(database)
	sessionManager := session.NewSessionManager()
	apiKeyService := apikeyService.NewAPIKeyService(database, sessionManager)

	// Web browser routes
	http.HandleFunc("/", home)
	http.HandleFunc("/login/spotify", oauthManager.HandleLogin("spotify"))
	http.HandleFunc("/callback/spotify", oauthManager.HandleCallback("spotify", spotifyService))
	http.HandleFunc("/current-track", session.WithAuth(spotifyService.HandleCurrentTrack, sessionManager))
	http.HandleFunc("/history", session.WithAuth(spotifyService.HandleTrackHistory, sessionManager))
	http.HandleFunc("/api-keys", session.WithAuth(apiKeyService.HandleAPIKeyManagement, sessionManager))
	http.HandleFunc("/logout", sessionManager.HandleLogout)

	// API routes
	http.HandleFunc("/api/v1/current-track", session.WithAPIAuth(apiCurrentTrack(spotifyService), sessionManager))
	http.HandleFunc("/api/v1/history", session.WithAPIAuth(apiTrackHistory(spotifyService), sessionManager))

	trackerInterval := time.Duration(viper.GetInt("tracker.interval")) * time.Second

	if err := spotifyService.LoadAllUsers(); err != nil {
		log.Printf("Warning: Failed to preload users: %v", err)
	}

	go spotifyService.StartListeningTracker(trackerInterval)

	serverAddr := fmt.Sprintf("%s:%s", viper.GetString("server.host"), viper.GetString("server.port"))
	fmt.Printf("Server running at: http://%s\n", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}

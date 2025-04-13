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
	"github.com/teal-fm/piper/oauth/atproto"
	apikeyService "github.com/teal-fm/piper/service/apikey"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Check if user has an active session cookie
	cookie, err := r.Cookie("session")
	isLoggedIn := err == nil && cookie != nil
	// TODO: Add logic here to fetch user details from DB using session ID
	// to check if Spotify is already connected, if desired for finer control.
	// For now, we'll just check if *any* session exists.

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
			<h1>Piper - Multi-User Spotify Tracker via ATProto</h1>
			<div class="nav">
				<a href="/">Home</a>`

	if isLoggedIn {
		html += `
				<a href="/current-track">Current Track</a>
				<a href="/history">Track History</a>
				<a href="/api-keys">API Keys</a>
				<a href="/login/spotify">Connect Spotify Account</a> <!-- Link to connect Spotify -->
				<a href="/logout">Logout</a>`
	} else {
		html += `
				<a href="/login/atproto">Login with ATProto</a>` // Primary login is ATProto
	}

	html += `
			</div>

			<div class="card">
				<h2>Welcome to Piper</h2>
				<p>Piper is a multi-user Spotify tracking application that records what you're listening to and saves your listening history.</p>`

	if !isLoggedIn {
		html += `
				<p><a href="/login/atproto">Login with ATProto</a> to get started!</p>` // Prompt to login via ATProto
	} else {
		html += `
				<p>You're logged in! <a href="/login/spotify">Connect your Spotify account</a> to start tracking.</p>
				<p>Once connected, you can check out your <a href="/current-track">current track</a> or view your <a href="/history">listening history</a>.</p>
				<p>You can also manage your <a href="/api-keys">API keys</a> for programmatic access.</p>`
	}

	html += `
			</div> <!-- Close card div -->
		</body>
		</html>
	` // Added closing div tag

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

	database, err := db.New(viper.GetString("db.path"))
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	if err := database.Initialize(); err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}

	spotifyService := spotify.NewSpotifyService(database)
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
	apiKeyService := apikeyService.NewAPIKeyService(database, sessionManager)

	// init atproto svc
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

	oauthManager.RegisterService("atproto", atprotoService)

	// Web browser routes
	http.HandleFunc("/", home)

	// oauth (scraper) logins
	http.HandleFunc("/login/spotify", oauthManager.HandleLogin("spotify"))
	http.HandleFunc("/callback/spotify", session.WithPossibleAuth(oauthManager.HandleCallback("spotify"), sessionManager))

	// atproto login
	http.HandleFunc("/login/atproto", oauthManager.HandleLogin("atproto"))
	http.HandleFunc("/callback/atproto", oauthManager.HandleCallback("atproto"))

	http.HandleFunc("/current-track", session.WithAuth(spotifyService.HandleCurrentTrack, sessionManager))
	http.HandleFunc("/history", session.WithAuth(spotifyService.HandleTrackHistory, sessionManager))
	http.HandleFunc("/api-keys", session.WithAuth(apiKeyService.HandleAPIKeyManagement, sessionManager))
	http.HandleFunc("/logout", sessionManager.HandleLogout)

	// API routes
	http.HandleFunc("/api/v1/current-track", session.WithAPIAuth(apiCurrentTrack(spotifyService), sessionManager))
	http.HandleFunc("/api/v1/history", session.WithAPIAuth(apiTrackHistory(spotifyService), sessionManager))

	serverUrlRoot := viper.GetString("server.root_url")
	atpClientId := viper.GetString("atproto.client_id")
	atpCallbackUrl := viper.GetString("atproto.callback_url")

	http.HandleFunc("/.well-known/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		atprotoService.HandleClientMetadata(w, r, serverUrlRoot, atpClientId, atpCallbackUrl)
	})

	http.HandleFunc("/oauth/jwks.json", atprotoService.HandleJwks)

	trackerInterval := time.Duration(viper.GetInt("tracker.interval")) * time.Second

	if err := spotifyService.LoadAllUsers(); err != nil {
		log.Printf("Warning: Failed to preload users: %v", err)
	}

	go spotifyService.StartListeningTracker(trackerInterval)

	serverAddr := fmt.Sprintf("%s:%s", viper.GetString("server.host"), viper.GetString("server.port"))
	fmt.Printf("Server running at: http://%s\n", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}

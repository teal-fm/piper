package main

import (
	"encoding/json"
	"fmt"
	"github.com/teal-fm/piper/service/lastfm"
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
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

type application struct {
	database       *db.DB
	sessionManager *session.SessionManager
	oauthManager   *oauth.OAuthServiceManager
	spotifyService *spotify.SpotifyService
	apiKeyService  *apikeyService.Service
	mbService      *musicbrainz.MusicBrainzService
	atprotoService *atproto.ATprotoAuthService
}

// JSON API handlers

func jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		json.NewEncoder(w).Encode(data)
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
		// run `make jwtgen`
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
	lastfmService := lastfm.NewLastFMService(database, viper.GetString("lastfm.api_key"), mbService, atprotoService)

	sessionManager := session.NewSessionManager(database)
	oauthManager := oauth.NewOAuthServiceManager(sessionManager)

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

	app := &application{
		database:       database,
		sessionManager: sessionManager,
		oauthManager:   oauthManager,
		apiKeyService:  apiKeyService,
		mbService:      mbService,
		spotifyService: spotifyService,
		atprotoService: atprotoService,
	}

	trackerInterval := time.Duration(viper.GetInt("tracker.interval")) * time.Second
	lastfmInterval := time.Duration(viper.GetInt("lastfm.interval_seconds")) * time.Second // Add config for Last.fm interval
	if lastfmInterval <= 0 {
		lastfmInterval = 30 * time.Second
	}

	//if err := spotifyService.LoadAllUsers(); err != nil {
	//	log.Printf("Warning: Failed to preload Spotify users: %v", err)
	//}
	go spotifyService.StartListeningTracker(trackerInterval)

	go lastfmService.StartListeningTracker(lastfmInterval)

	serverAddr := fmt.Sprintf("%s:%s", viper.GetString("server.host"), viper.GetString("server.port"))
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	fmt.Printf("Server running at: http://%s\n", serverAddr)
	log.Fatal(server.ListenAndServe())
}

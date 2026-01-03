package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/teal-fm/piper/service/applemusic"
	"github.com/teal-fm/piper/service/lastfm"
	"github.com/teal-fm/piper/service/playingnow"

	"github.com/spf13/viper"
	"github.com/teal-fm/piper/config"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/oauth"
	"github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/pages"
	apikeyService "github.com/teal-fm/piper/service/apikey"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/spotify"
	"github.com/teal-fm/piper/session"
)

type application struct {
	database          *db.DB
	sessionManager    *session.Manager
	oauthManager      *oauth.ServiceManager
	spotifyService    *spotify.Service
	apiKeyService     *apikeyService.Service
	mbService         *musicbrainz.Service
	atprotoService    *atproto.AuthService
	playingNowService *playingnow.Service
	appleMusicService *applemusic.Service
	pages             *pages.Pages
}

// JSON API handlers

func jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		err := json.NewEncoder(w).Encode(data)
		if err != nil {
			log.Printf("Error encoding JSON response: %v", err)
			return
		}
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

	sessionManager := session.NewSessionManager(database)

	// --- Service Initializations ---

	var newJwkPrivateKey = viper.GetString("atproto.client_secret_key")
	if newJwkPrivateKey == "" {
		fmt.Printf("You now have to set the ATPROTO_CLIENT_SECRET_KEY env var to a private key. This can be done via goat key generate -t P-256")
		return
	}
	var clientSecretKeyId = viper.GetString("atproto.client_secret_key_id")
	if clientSecretKeyId == "" {
		fmt.Printf("You also now have to set the ATPROTO_CLIENT_SECRET_KEY_ID env var to a key ID. This needs to be persistent and unique. Here's one for you: %d", time.Now().Unix())
		return
	}

	var allowedDids = viper.GetStringSlice("allowed_dids")
	if len(allowedDids) > 0 {
		log.Printf("Allowed DIDs provided. Only allowing %s\n", strings.Join(allowedDids, ", "))
	}

	atprotoService, err := atproto.NewATprotoAuthService(
		database,
		sessionManager,
		newJwkPrivateKey,
		viper.GetString("atproto.client_id"),
		viper.GetString("atproto.callback_url"),
		clientSecretKeyId,
		allowedDids,
	)
	if err != nil {
		log.Fatalf("Error creating ATproto auth service: %v", err)
	}

	mbService := musicbrainz.NewMusicBrainzService(database)
	playingNowService := playingnow.NewPlayingNowService(database, atprotoService)

	// Apple Music Initialization

	// Read Apple Music settings with env fallbacks
	teamID := viper.GetString("applemusic.team_id")
	if teamID == "" {
		teamID = viper.GetString("APPLE_MUSIC_TEAM_ID")
	}
	keyID := viper.GetString("applemusic.key_id")
	if keyID == "" {
		keyID = viper.GetString("APPLE_MUSIC_KEY_ID")
	}
	keyPath := viper.GetString("applemusic.private_key_path")
	if keyPath == "" {
		keyPath = viper.GetString("APPLE_MUSIC_PRIVATE_KEY_PATH")
	}

	var appleMusicService *applemusic.Service
	// Only initialize Apple Music service if all required credentials are present
	if teamID != "" && keyID != "" && keyPath != "" {
		log.Println("Apple Music variables provided, enabling Apple Music service")
		appleMusicService = applemusic.NewService(
			teamID,
			keyID,
			keyPath,
		).WithPersistence(
			func() (string, time.Time, bool, error) {
				return database.GetAppleMusicDeveloperToken()
			},
			func(token string, exp time.Time) error {
				return database.SaveAppleMusicDeveloperToken(token, exp)
			},
		).WithDeps(database, atprotoService, mbService, playingNowService)
	} else {
		log.Println("Apple Music credentials not configured (missing team_id, key_id, or private_key_path). Apple Music features will be disabled.")
	}

	oauthManager := oauth.NewOAuthServiceManager()

	// Spotify Initialization
	var spotifyClientID = viper.GetString("spotify.client_id")
	var spotifyClientSecret = viper.GetString("spotify.client_secret")
	var spotifyCallbackURL = viper.GetString("callback.spotify")
	var spotifyScopes = viper.GetStringSlice("spotify.scopes")

	var spotifyService *spotify.Service
	// Only initialize Spotify service if all required credentials are present
	if spotifyClientID != "" && spotifyClientSecret != "" && spotifyCallbackURL != "" && len(spotifyScopes) > 0 {
		log.Println("Spotify variables provided, enabling Spotify service")
		spotifyService = spotify.NewSpotifyService(database, atprotoService, mbService, playingNowService)

		spotifyOAuth := oauth.NewOAuth2Service(
			spotifyClientID,
			spotifyClientSecret,
			spotifyCallbackURL,
			spotifyScopes,
			"spotify",
			spotifyService,
		)
		oauthManager.RegisterService("spotify", spotifyOAuth)
	} else {
		log.Println("Spotify credentials not configured (missing client_id, client_secret, callback, or scopes). Spotify features will be disabled.")
	}

	// Last.fm Initialization
	var lastfmInterval = time.Duration(viper.GetInt("lastfm.interval_seconds")) * time.Second

	var lastfmService *lastfm.Service
	if viper.GetString("lastfm.api_key") != "" {
		log.Println("Last.fm API key provided, enabling Last.fm service")
		lastfmService = lastfm.NewLastFMService(database, viper.GetString("lastfm.api_key"), mbService, atprotoService, playingNowService)
		if lastfmInterval <= 0 {
			lastfmInterval = 30 * time.Second
		}
	} else {
		log.Println("Last.fm API key not configured. Last.fm features will be disabled.")
	}

	oauthManager.RegisterService("atproto", atprotoService)

	apiKeyService := apikeyService.NewAPIKeyService(database, sessionManager)

	app := &application{
		database:          database,
		sessionManager:    sessionManager,
		oauthManager:      oauthManager,
		apiKeyService:     apiKeyService,
		mbService:         mbService,
		spotifyService:    spotifyService,
		atprotoService:    atprotoService,
		playingNowService: playingNowService,
		appleMusicService: appleMusicService,
		pages:             pages.NewPages(),
	}

	trackerInterval := time.Duration(viper.GetInt("tracker.interval")) * time.Second
	if spotifyService != nil {
		go spotifyService.StartListeningTracker(trackerInterval)
	}

	if lastfmService != nil {
		go lastfmService.StartListeningTracker(lastfmInterval)
	}

	// Apple Music tracker uses same tracker.interval as Spotify for now
	// Only start if Apple Music service is configured
	if appleMusicService != nil {
		go appleMusicService.StartListeningTracker(trackerInterval)
	}

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

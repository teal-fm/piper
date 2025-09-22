package main

import (
	"net/http"

	"github.com/justinas/alice"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/session"
)

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", session.WithPossibleAuth(home(app.database), app.sessionManager))

	// OAuth Routes
	mux.HandleFunc("/login/spotify", app.oauthManager.HandleLogin("spotify"))
	mux.HandleFunc("/callback/spotify", session.WithPossibleAuth(app.oauthManager.HandleCallback("spotify"), app.sessionManager)) // Use possible auth
	mux.HandleFunc("/login/atproto", app.oauthManager.HandleLogin("atproto"))
	mux.HandleFunc("/callback/atproto", session.WithPossibleAuth(app.oauthManager.HandleCallback("atproto"), app.sessionManager)) // Use possible auth

	// Authenticated Web Routes
	mux.HandleFunc("/current-track", session.WithAuth(app.spotifyService.HandleCurrentTrack, app.sessionManager))
	mux.HandleFunc("/history", session.WithAuth(app.spotifyService.HandleTrackHistory, app.sessionManager))
	mux.HandleFunc("/api-keys", session.WithAuth(app.apiKeyService.HandleAPIKeyManagement, app.sessionManager))
	mux.HandleFunc("/link-lastfm", session.WithAuth(handleLinkLastfmForm(app.database), app.sessionManager))          // GET form
	mux.HandleFunc("/link-lastfm/submit", session.WithAuth(handleLinkLastfmSubmit(app.database), app.sessionManager)) // POST submit - Changed route slightly
	mux.HandleFunc("/logout", app.sessionManager.HandleLogout)
	mux.HandleFunc("/debug/", session.WithAuth(app.sessionManager.HandleDebug, app.sessionManager))

	mux.HandleFunc("/api/v1/me", session.WithAPIAuth(apiMeHandler(app.database), app.sessionManager))
	mux.HandleFunc("/api/v1/lastfm", session.WithAPIAuth(apiGetLastfmUserHandler(app.database), app.sessionManager))
	mux.HandleFunc("/api/v1/lastfm/set", session.WithAPIAuth(apiLinkLastfmHandler(app.database), app.sessionManager))
	mux.HandleFunc("/api/v1/lastfm/unset", session.WithAPIAuth(apiUnlinkLastfmHandler(app.database), app.sessionManager))
	mux.HandleFunc("/api/v1/current-track", session.WithAPIAuth(apiCurrentTrack(app.spotifyService), app.sessionManager)) // Spotify Current
	mux.HandleFunc("/api/v1/history", session.WithAPIAuth(apiTrackHistory(app.spotifyService), app.sessionManager))       // Spotify History
	mux.HandleFunc("/api/v1/musicbrainz/search", apiMusicBrainzSearch(app.mbService))                                     // MusicBrainz (public?)

	// ListenBrainz-compatible endpoint
	mux.HandleFunc("/1/submit-listens", session.WithAPIAuth(apiSubmitListensHandler(app.database), app.sessionManager))

	serverUrlRoot := viper.GetString("server.root_url")
	atpClientId := viper.GetString("atproto.client_id")
	atpCallbackUrl := viper.GetString("atproto.callback_url")
	mux.HandleFunc("/.well-known/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		app.atprotoService.HandleClientMetadata(w, r, serverUrlRoot, atpClientId, atpCallbackUrl)
	})
	mux.HandleFunc("/oauth/jwks.json", app.atprotoService.HandleJwks)

	standard := alice.New()
	return standard.Then(mux)
}

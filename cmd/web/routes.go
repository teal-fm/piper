package main

import (
	"net/http"

	"github.com/teal-fm/piper/services/oauth"

  "github.com/justinas/alice"
)

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	oauthService := oauth.NewOAuthService(app.logger)

	fileServer := http.FileServer(http.Dir("./ui/static/"))
	mux.Handle("GET /static/", http.StripPrefix("/static", fileServer))

  dynamic := alice.New(app.sessionManager.LoadAndSave)

	mux.Handle("GET /{$}", dynamic.ThenFunc(app.home))
	mux.Handle("/login", dynamic.ThenFunc(oauthService.HandleLogin))
	mux.Handle("/callback", dynamic.ThenFunc(oauthService.HandleCallback))

  standard := alice.New(app.recoverPanic, app.logRequest, commonHeaders)

	return standard.Then(mux)
}

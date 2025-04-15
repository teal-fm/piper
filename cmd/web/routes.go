package main

import (
	"net/http"

	"github.com/justinas/alice"
	"github.com/teal-fm/piper/ui"
)

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.FileServerFS(ui.Files))

	dynamic := alice.New(app.sessionManager.LoadAndSave, app.authenticate)

	mux.Handle("GET /{$}", dynamic.ThenFunc(app.home))
	mux.Handle("/login", dynamic.ThenFunc(app.oauthService.HandleLogin))
	mux.Handle("/callback", dynamic.ThenFunc(app.oauthService.HandleCallback))
	mux.Handle("/logout", dynamic.ThenFunc(app.logout))

	//protected := dynamic.Append(app.requireAuthentication)

	standard := alice.New(app.recoverPanic, app.logRequest, commonHeaders)

	return standard.Then(mux)
}

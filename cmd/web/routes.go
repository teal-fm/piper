package main

import (
	"net/http"

	"github.com/justinas/alice"
)

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir("./ui/static/"))
	mux.Handle("GET /static/", http.StripPrefix("/static", fileServer))

	dynamic := alice.New(app.sessionManager.LoadAndSave, app.authenticate)

	mux.Handle("GET /{$}", dynamic.ThenFunc(app.home))
	mux.Handle("/login", dynamic.ThenFunc(app.oauthService.HandleLogin))
	mux.Handle("/logout", dynamic.ThenFunc(app.logout))
	mux.Handle("/callback", dynamic.ThenFunc(app.oauthService.HandleCallback))

  //protected := dynamic.Append(app.requireAuthentication)

	standard := alice.New(app.recoverPanic, app.logRequest, commonHeaders)

	return standard.Then(mux)
}

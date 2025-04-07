package main

import (
  "net/http"

	"github.com/teal-fm/piper/services/oauth"
)

func (app *application) routes() *http.ServeMux {
  mux := http.NewServeMux()

	oauthService := oauth.NewOAuthService(app.logger)

  fileServer := http.FileServer(http.Dir("./ui/static/"))
  mux.Handle("GET /static/", http.StripPrefix("/static", fileServer))

  mux.HandleFunc("GET /{$}", app.home)
	mux.HandleFunc("/login", oauthService.HandleLogin)
	mux.HandleFunc("/callback", oauthService.HandleCallback)

  return mux
}

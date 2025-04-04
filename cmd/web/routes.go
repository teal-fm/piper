package main

import (
  "net/http"
)

func (app *application) routes() *http.ServeMux {
  mux := http.NewServeMux)_

  fileServer := http.FileServer(http.Dir("./ui/static/"))
  mux.Handle("GET /static/", http.StripPrefix("/static", fileServer))

  mux.HandleFunc("GET /{$}", app.home)
	mux.HandleFunc("/login", oauthService.HandleLogin)
	mux.HandleFunc("/callback", oauthService.HandleCallback)

  return mux
}

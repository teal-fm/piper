package main

import (
  "net/http"
)


func (app *application) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("visit <a href='/login'>/login</a> to get started"))
}


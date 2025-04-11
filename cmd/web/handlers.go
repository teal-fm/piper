package main

import (
	"net/http"
)

func (app *application) home(w http.ResponseWriter, r *http.Request) {
  app.sessionManager.Put(r.Context(), "flash", "hi!")

  data := app.newTemplateData(r)

  app.render(w, r, http.StatusOK, "home.tmpl", data)
}

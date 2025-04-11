package main

import (
	"net/http"
)

func (app *application) home(w http.ResponseWriter, r *http.Request) {
  data := app.newTemplateData(r)

  app.sessionManager.Put(r.Context(), "flash", "hi!")

  app.render(w, r, http.StatusOK, "home.tmpl", data)
}

package main

import (
    "encoding/json"
	"net/http"

    "golang.org/x/oauth2"
)

func (app *application) home(w http.ResponseWriter, r *http.Request) {
  data := app.newTemplateData(r)


  if app.sessionManager.Exists(r.Context(), "token") {
    token := app.sessionManager.PopString(r.Context(), "token")
    var tok oauth2.Token
    err := json.Unmarshal([]byte(token), &tok)
    if err != nil {
        app.logger.Error(err.Error())
        return
    }
  }

  app.render(w, r, http.StatusOK, "home.tmpl", data)
}

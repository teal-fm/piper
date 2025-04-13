package main

import (
	"context"
	"encoding/json"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/teal-fm/piper/providers/spotify"
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
		client := app.oauthService.Cfg.Client(context.Background(), &tok)
		userInfo, err := spotify.GetUserInfo(client, app.logger)
		if err != nil {
			http.Error(w, "failed to get user info", http.StatusInternalServerError)
			app.logger.Error("user info error: ", err)
			return
		}
		app.logger.Info("user", "name", userInfo.Name)
	}

	app.render(w, r, http.StatusOK, "home.tmpl", data)
}

func (app *application) logout(w http.ResponseWriter, r *http.Request) {
	err := app.sessionManager.RenewToken(r.Context())
	if err != nil {
		app.serverError(w, r, err)
		return
	}

	app.sessionManager.Remove(r.Context(), "token")

	app.sessionManager.Put(r.Context(), "flash", "you've been logged out!")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

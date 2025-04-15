package oauth

import (
	"context"
	"encoding/json"
	"net/http"

	"golang.org/x/oauth2"
)

func (o *OAuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	verifier := oauth2.GenerateVerifier()
	o.sessionManager.Put(r.Context(), "verifier", verifier)

	url := o.Cfg.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (o *OAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	verifier := o.sessionManager.PopString(r.Context(), "verifier")

	if state != "state" {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	token, err := o.Cfg.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		return
	}

	err = o.sessionManager.RenewToken(r.Context())
	if err != nil {
		http.Error(w, "failed to renew token", http.StatusInternalServerError)
		return
	}

	tok, err := json.Marshal(token)
	if err != nil {
		http.Error(w, "failed to marshal user token", http.StatusInternalServerError)
		return
	}
	o.sessionManager.Put(r.Context(), "token", string(tok))

	http.Redirect(w, r, "/", http.StatusSeeOther)

	//playlistsInfo, err := spotify.GetUserPlaylists(client, o.logger)
	//if err != nil {
	//	http.Error(w, "failed to get user playlists", http.StatusInternalServerError)
	//	o.logger.Error("user playlist info error: ", err)
	//	return
	//}
	//fmt.Fprintf(w, "playlistResponse: %v\n", playlistsInfo)
}

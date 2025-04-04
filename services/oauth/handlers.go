package oauth

import (
  "context"
  "fmt"
  "net/http"

  "github.com/teal-fm/piper/providers/spotify"
  
  "golang.org/x/oauth2"
)

func (o *OAuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	url := o.cfg.AuthCodeURL("state", oauth2.AccessTypeOffline/*, oauth2.S256ChallengeOption(o.verifier)*/)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (o *OAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state != "state" {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	token, err := o.cfg.Exchange(context.Background(), code/*, oauth2.VerifierOption(o.verifier)*/)
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusInternalServerError)
		o.logger.Error("token exchange error:", err)
		return
	}

	client := o.cfg.Client(context.Background(), token)
	userInfo, err := spotify.GetUserInfo(client, o.logger)
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		o.logger.Error("user info error: ", err)
		return
	}

	playlistsInfo, err := spotify.GetUserPlaylists(client, o.logger)
	if err != nil {
		http.Error(w, "failed to get user playlists", http.StatusInternalServerError)
		o.logger.Error("user playlist info error: ", err)
		return
	}
	fmt.Fprintf(w, "logged in successfully!\nuser: %s; id: %s\n", userInfo.Name, userInfo.ID)
	fmt.Fprintf(w, "playlistResponse: %v\n", playlistsInfo)
}


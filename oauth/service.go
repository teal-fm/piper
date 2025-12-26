package oauth

import (
	"net/http"
)

type AuthService interface {
	// HandleLogin inits the login flow for the service
	HandleLogin(w http.ResponseWriter, r *http.Request)
	// HandleCallback handles the callback for the provider. is responsible for inserting
	// sessions in the db
	HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error)

	HandleLogout(w http.ResponseWriter, r *http.Request)
}

// TokenReceiver optional but recommended
type TokenReceiver interface {
	// SetAccessToken stores the access token in the db
	// if there is a session, will associate the token with the session
	SetAccessToken(token string, refreshToken string, currentId int64, hasSession bool) (int64, error)
}

package oauth

import (
	"net/http"
)

type AuthService interface {
	// inits the login flow for the service
	HandleLogin(w http.ResponseWriter, r *http.Request)
	// handles the callback for the provider. is responsible for inserting
	// sessions in the db
	HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error)
}

// optional but recommended
type TokenReceiver interface {
	// stores the access token in the db
	// if there is a session, will associate the token with the session
	SetAccessToken(token string, currentId int64, hasSession bool) (int64, error)
}

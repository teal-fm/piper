package oauth

import (
	"log/slog"
	"os"

	"github.com/alexedwards/scs/v2"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

type OAuthService struct {
	Cfg            *oauth2.Config
	logger         *slog.Logger
	sessionManager *scs.SessionManager
}

func NewOAuthService(
	logger *slog.Logger,
	sessionManager *scs.SessionManager,
) *OAuthService {
	return &OAuthService{
		Cfg: &oauth2.Config{
			ClientID:     os.Getenv("CLIENT_ID"),
			ClientSecret: os.Getenv("CLIENT_SECRET"),
			Endpoint:     spotify.Endpoint,
			RedirectURL:  os.Getenv("REDIRECT_URL"),
			Scopes:       []string{
        "user-read-private", "user-read-email", "user-library-read", 
        "user-read-currently-playing",
      },
		},
		logger:         logger,
		sessionManager: sessionManager,
	}
}

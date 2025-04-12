package main

import (
	"flag"
	"fmt"
  "html/template"
	"log/slog"
	"net/http"
	"os"
  "time"

	"github.com/joho/godotenv"
  "github.com/alexedwards/scs/v2/memstore"
  "github.com/alexedwards/scs/v2"

  "github.com/teal-fm/piper/services/oauth"
)

type application struct {
	logger          *slog.Logger
    oauthService    *oauth.OAuthService
  sessionManager  *scs.SessionManager
  templateCache   map[string]*template.Template
}

func main() {
	port := flag.String("addr", ":8080", "HTTP network port")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := godotenv.Load()
	if err != nil {
		logger.Error("Error loading .env file")
	}

  templateCache, err := newTemplateCache()
  if err != nil {
    logger.Error(err.Error())
    os.Exit(1)
  }

  sessionManager := scs.New()
  sessionManager.Store = memstore.New()
  sessionManager.Lifetime = 12 * time.Hour

    oauthService := oauth.NewOAuthService(logger, sessionManager)


	app := &application{
		logger:         logger,
        oauthService:   oauthService,
    sessionManager: sessionManager,
    templateCache:  templateCache,
	}

	logger.Info(fmt.Sprintf("starting server at: http://localhost%s", *port))

	err = http.ListenAndServe(*port, app.routes())
	logger.Error(err.Error())
	os.Exit(1)
}

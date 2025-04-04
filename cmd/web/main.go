package main

import (
  "flag"
  "fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/teal-fm/piper/services/oauth"

	"github.com/joho/godotenv"
)

type application struct {
	logger *slog.Logger
}

func main() {
  port := flag.String("addr", ":8080", "HTTP network port")

  flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := godotenv.Load()
	if err != nil {
		logger.Error("Error loading .env file")
	}

	oauthService := oauth.NewOAuthService(logger)

	app := &application{
		logger: logger,
	}

	logger.Info(fmt.Sprintf("starting server at: http://localhost%s", *port))

	err = http.ListenAndServe(*port, app.routes())
	logger.Error(err.Error())
	os.Exit(1)
}

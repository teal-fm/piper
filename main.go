package main

import (
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := godotenv.Load()
	if err != nil {
		logger.Error("Error loading .env file")
	}

	oauthService := oauth.NewOAuthService(logger)

	app := &application{
		logger: logger,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", app.home)
	mux.HandleFunc("/login", oauthService.HandleLogin)
	mux.HandleFunc("/callback", oauthService.HandleCallback)

	logger.Info("starting server at: http://localhost:8080")

	err = http.ListenAndServe(":8080", mux)
	logger.Error(err.Error())
	os.Exit(1)
}

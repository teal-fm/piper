package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
  "time"

	"github.com/joho/godotenv"
  "github.com/alexedwards/scs/v2/memstore"
  "github.com/alexedwards/scs/v2"
)

type application struct {
	logger          *slog.Logger
  sessionManager  *scs.SessionManager
}

func main() {
	port := flag.String("addr", ":8080", "HTTP network port")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := godotenv.Load()
	if err != nil {
		logger.Error("Error loading .env file")
	}

  sessionManager := scs.New()
  sessionManager.Store = memstore.New()
  sessionManager.Lifetime = 12 * time.Hour


	app := &application{
		logger:         logger,
    sessionManager: sessionManager,
	}

	logger.Info(fmt.Sprintf("starting server at: http://localhost%s", *port))

	err = http.ListenAndServe(*port, app.routes())
	logger.Error(err.Error())
	os.Exit(1)
}

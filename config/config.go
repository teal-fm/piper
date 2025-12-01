package config

import (
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Load initializes the configuration with viper
func Load() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it. Using default values and environment variables.")
	}

	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.host", "localhost")
	viper.SetDefault("callback.spotify", "http://localhost:8080/callback/spotify")
	viper.SetDefault("spotify.auth_url", "https://accounts.spotify.com/authorize")
	viper.SetDefault("spotify.token_url", "https://accounts.spotify.com/api/token")
	viper.SetDefault("spotify.scopes", "user-read-currently-playing user-read-email")
	viper.SetDefault("tracker.interval", 30)
	viper.SetDefault("db.path", "./data/piper.db")

	// plyr.fm defaults
	viper.SetDefault("plyrfm.api_url", "https://api.plyr.fm")
	viper.SetDefault("plyrfm.interval_seconds", 30)

	// server metadata
	viper.SetDefault("server.root_url", "http://localhost:8080")
	viper.SetDefault("atproto.metadata_url", "http://localhost:8080/metadata")
	viper.SetDefault("atproto.callback_url", "/metadata")

	viper.AutomaticEnv()

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Fatalf("Error reading config file: %v", err)
		}
		log.Println("Config file not found, using default values and environment variables")
	} else {
		log.Println("Using config file:", viper.ConfigFileUsed())
	}

	// check for required settings
	requiredVars := []string{"spotify.client_id", "spotify.client_secret"}
	missingVars := []string{}

	for _, v := range requiredVars {
		if !viper.IsSet(v) {
			missingVars = append(missingVars, v)
		}
	}

	if len(missingVars) > 0 {
		log.Fatalf("Required configuration variables not set: %s", strings.Join(missingVars, ", "))
	}
}

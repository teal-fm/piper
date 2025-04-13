package config

import (
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Load initializes the configuration with viper
func Load() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it. Using default values and environment variables.")
	}

	// Set default configurations
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.host", "localhost")
	viper.SetDefault("callback.spotify", "http://localhost:8080/callback/spotify")
	viper.SetDefault("spotify.auth_url", "https://accounts.spotify.com/authorize")
	viper.SetDefault("spotify.token_url", "https://accounts.spotify.com/api/token")
	viper.SetDefault("spotify.scopes", "user-read-currently-playing user-read-email")
	viper.SetDefault("tracker.interval", 30)
	viper.SetDefault("db.path", "./data/piper.db")

	// server metadata
	viper.SetDefault("server.root_url", "http://localhost:8080")
	viper.SetDefault("atproto.metadata_url", "http://localhost:8080/metadata")
	viper.SetDefault("atproto.callback_url", "/metadata")

	// Configure Viper to read environment variables
	viper.AutomaticEnv()

	// Replace dots with underscores for environment variables
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set the config name and paths
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	// Try to read the config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// It's not a "file not found" error, so it's a real error
			log.Fatalf("Error reading config file: %v", err)
		}
		// Config file not found, using defaults and environment variables
		log.Println("Config file not found, using default values and environment variables")
	} else {
		log.Println("Using config file:", viper.ConfigFileUsed())
	}

	// Check if required values are present
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

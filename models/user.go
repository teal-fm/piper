package models

import "time"

// User represents a user of the application
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	SpotifyID    string    `json:"spotify_id"`
	AccessToken  string    `json:"-"` // Not exposed in JSON
	RefreshToken string    `json:"-"` // Not exposed in JSON
	TokenExpiry  time.Time `json:"-"` // Not exposed in JSON
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
package models

import "time"

// User represents a user of the application
type User struct {
	ID                  int64
	Username            string
	Email               *string    // Use pointer for nullable fields
	SpotifyID           *string    // Use pointer for nullable fields
	AccessToken         *string    // Spotify Access Token
	RefreshToken        *string    // Spotify Refresh Token
	TokenExpiry         *time.Time // Spotify Token Expiry
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ATProtoDID          *string    // ATProto DID
	ATProtoAccessToken  *string    // ATProto Access Token
	ATProtoRefreshToken *string    // ATProto Refresh Token
	ATProtoTokenExpiry  *time.Time // ATProto Token Expiry
}

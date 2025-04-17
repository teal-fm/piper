package models

import "time"

// an end user of piper
type User struct {
	ID       int64
	Username string
	Email    *string

	// spotify information
	SpotifyID    *string
	AccessToken  *string
	RefreshToken *string
	TokenExpiry  *time.Time

	// lfm information
	LastFMUsername *string

	// atp info
	ATProtoDID          *string
	ATProtoAccessToken  *string
	ATProtoRefreshToken *string
	ATProtoTokenExpiry  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

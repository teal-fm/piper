package models

import "time"

// an end user of piper
type User struct {
	ID       int64
	Username *string
	Email    *string

	// spotify information
	SpotifyID    *string
	AccessToken  *string
	RefreshToken *string
	TokenExpiry  *time.Time

	// lfm information
	LastFMUsername *string

	// plyr.fm information
	PlyrFMHandle *string

	// atp info
	ATProtoDID *string
	//This is meant to only be used by the automated music stamping service. If the user ever does an
	//atproto action from the web ui use the atproto session id for the logged-in session
	MostRecentAtProtoSessionID *string
	//ATProtoAccessToken  *string
	//ATProtoRefreshToken *string
	//ATProtoTokenExpiry  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

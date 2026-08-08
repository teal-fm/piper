package models

import "time"

// User an end user of piper
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

	// Apple Music
	AppleMusicUserToken *string

	// atp info
	ATProtoDID *string
	//This is meant to only be used by the automated music stamping service. If the user ever does an
	//atproto action from the web ui use the atproto session id for the logged-in session
	MostRecentAtProtoSessionID *string

	// Public profile, fetched from the Bluesky AppView and cached here so we
	// don't hit the network on every page render
	Handle           *string
	DisplayName      *string
	AvatarURL        *string
	ProfileFetchedAt *time.Time
	//ATProtoAccessToken  *string
	//ATProtoRefreshToken *string
	//ATProtoTokenExpiry  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

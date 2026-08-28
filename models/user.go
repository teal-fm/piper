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
	// An empty (but non-nil) value means the account has no avatar.
	LastFMAvatarURL *string

	// Apple Music
	AppleMusicUserToken *string

	// atp info
	ATProtoDID *string
	//This is meant to only be used by the automated music stamping service. If the user ever does an
	//atproto action from the web ui use the atproto session id for the logged-in session
	MostRecentAtProtoSessionID *string

	// Public profile, cached from the Bluesky AppView.
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

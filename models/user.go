package models

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	ServiceSpotify    = "spotify"
	ServiceAppleMusic = "applemusic"
	ServiceLastFM     = "lastfm"

	DefaultServicePriority = ServiceSpotify + "," + ServiceAppleMusic + "," + ServiceLastFM
)

var SupportedServicePriority = []string{ServiceSpotify, ServiceAppleMusic, ServiceLastFM}

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
	ServicePriority     string

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

func NormalizeServicePriority(raw string) (string, error) {
	items := strings.Split(strings.ToLower(strings.TrimSpace(raw)), ",")
	seen := make(map[string]struct{}, len(SupportedServicePriority))
	normalized := make([]string, 0, len(SupportedServicePriority))

	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !slices.Contains(SupportedServicePriority, item) {
			return "", fmt.Errorf("unsupported service %q", item)
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}

	for _, service := range SupportedServicePriority {
		if _, exists := seen[service]; !exists {
			normalized = append(normalized, service)
		}
	}

	if len(normalized) == 0 {
		return DefaultServicePriority, nil
	}

	return strings.Join(normalized, ","), nil
}

func ParseServicePriority(raw string) []string {
	normalized, err := NormalizeServicePriority(raw)
	if err != nil {
		normalized = DefaultServicePriority
	}
	return strings.Split(normalized, ",")
}

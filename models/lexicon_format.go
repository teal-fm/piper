package models

import (
	"net/url"
	"strings"
)

const mbidURIPrefix = "mbid:"

var serviceDomainAliases = map[string]string{
	"apple":        "music.apple.com",
	"applemusic":   "music.apple.com",
	"lastfm":       "last.fm",
	"listenbrainz": "listenbrainz.org",
	"spotify":      "spotify.com",
}

func FormatMBIDURI(id *string) *string {
	if id == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*id)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, mbidURIPrefix) {
		return &trimmed
	}

	formatted := mbidURIPrefix + trimmed
	return &formatted
}

func FormatMusicServiceURI(service string) *string {
	trimmed := strings.TrimSpace(service)
	if trimmed == "" {
		return nil
	}

	normalized := strings.ToLower(trimmed)
	if alias, ok := serviceDomainAliases[normalized]; ok {
		normalized = alias
	}

	if strings.Contains(normalized, "://") {
		parsed, err := url.Parse(normalized)
		if err == nil && parsed.Hostname() != "" {
			normalized = parsed.Hostname()
		}
	}

	if strings.Contains(normalized, "/") {
		normalized = strings.SplitN(normalized, "/", 2)[0]
	}

	uri := "https://" + normalized
	return &uri
}

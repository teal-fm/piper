package models

import "time"

// Track represents a Spotify track
type Track struct {
	PlayID         int64     `json:"playId"`
	Name           string    `json:"name"`
	Artist         []Artist  `json:"artist"`
	Album          string    `json:"album"`
	URL            string    `json:"url"`
	Timestamp      time.Time `json:"timestamp"`
	DurationMs     int64     `json:"durationMs"`
	ProgressMs     int64     `json:"progressMs"`
	ServiceBaseUrl string    `json:"serviceBaseUrl"`
	ISRC           string    `json:"isrc"`
	HasStamped     bool      `json:"hasStamped"`
}

type Artist struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

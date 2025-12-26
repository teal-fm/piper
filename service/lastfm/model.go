package lastfm

import (
	"encoding/json"
	"strconv"
	"time"
)

// RecentTracksResponse Structs to represent the Last.fm API response for user.getrecenttracks
type RecentTracksResponse struct {
	RecentTracks RecentTracks `json:"recenttracks"`
}

type RecentTracks struct {
	Tracks []Track      `json:"track"`
	Attr   TrackXMLAttr `json:"@attr"`
}

type Track struct {
	Artist     Artist     `json:"artist"`
	Streamable string     `json:"streamable"` // Typically "0" or "1"
	Image      []Image    `json:"image"`
	MBID       string     `json:"mbid"` // MusicBrainz ID for the track
	Album      Album      `json:"album"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Date       *TrackDate `json:"date,omitempty"` // Use pointer for optional fields
	Attr       *struct {  // Custom handling for @attr.nowplaying
		NowPlaying string `json:"nowplaying"` // Field name corrected to match struct tag
	} `json:"@attr,omitempty"` // This captures the @attr object within the track
}

type Artist struct {
	MBID string `json:"mbid"` // MusicBrainz ID for the artist
	Text string `json:"#text"`
}

type Image struct {
	Size string `json:"size"`  // "small", "medium", "large", "extralarge"
	Text string `json:"#text"` // URL of the image
}

type Album struct {
	MBID string `json:"mbid"`  // MusicBrainz ID for the album
	Text string `json:"#text"` // Album name
}

// ApiTrackDate This is the real structure returned from lastFM.
// Represents a date associated with a track, including both a Unix timestamp and a human-readable string.
// UTS is a Unix timestamp stored as a string.
// Text contains the human-readable date string.
type ApiTrackDate struct {
	UTS  string `json:"uts"`   // Unix timestamp string
	Text string `json:"#text"` // Human-readable date string
}

// TrackDate This is the struct we use to represent a date associated with a track.
// It is a wrapper around time.Time that implements json.Unmarshaler.
type TrackDate struct {
	time.Time
}

// UnmarshalJSON Implements json.Unmarshaler.
// Parses the UTS field from the API response and converts it to a time.Time.
// The time.Time is stored in the Time field.
// The Text field is ignored since it can be parsed from the Time field if needed.
func (t *TrackDate) UnmarshalJSON(b []byte) (err error) {
	var apiTrackDate ApiTrackDate
	if err := json.Unmarshal(b, &apiTrackDate); err != nil {
		return err
	}
	uts, err := strconv.ParseInt(apiTrackDate.UTS, 10, 64)
	if err != nil {
		return err
	}
	date := time.Unix(uts, 0).UTC()
	t.Time = date
	return
}

type TrackXMLAttr struct {
	User       string `json:"user"`
	TotalPages string `json:"totalPages"`
	Page       string `json:"page"`
	PerPage    string `json:"perPage"`
	Total      string `json:"total"`
}

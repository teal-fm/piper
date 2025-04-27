package lastfm

// Structs to represent the Last.fm API response for user.getrecenttracks
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

type TrackDate struct {
	UTS  string `json:"uts"`   // Unix timestamp string
	Text string `json:"#text"` // Human-readable date string
}

type TrackXMLAttr struct {
	User       string `json:"user"`
	TotalPages string `json:"totalPages"`
	Page       string `json:"page"`
	PerPage    string `json:"perPage"`
	Total      string `json:"total"`
}

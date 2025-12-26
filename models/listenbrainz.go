package models

import "time"

// ListenBrainzSubmission represents the top-level submission format
type ListenBrainzSubmission struct {
	ListenType string                `json:"listen_type"`
	Payload    []ListenBrainzPayload `json:"payload"`
}

// ListenBrainzPayload represents individual listen data
type ListenBrainzPayload struct {
	ListenedAt    *int64                    `json:"listened_at,omitempty"`
	TrackMetadata ListenBrainzTrackMetadata `json:"track_metadata"`
}

// ListenBrainzTrackMetadata contains the track information
type ListenBrainzTrackMetadata struct {
	ArtistName     string                      `json:"artist_name"`
	TrackName      string                      `json:"track_name"`
	ReleaseName    *string                     `json:"release_name,omitempty"`
	AdditionalInfo *ListenBrainzAdditionalInfo `json:"additional_info,omitempty"`
}

// ListenBrainzAdditionalInfo contains optional metadata
type ListenBrainzAdditionalInfo struct {
	MediaPlayer             *string  `json:"media_player,omitempty"`
	SubmissionClient        *string  `json:"submission_client,omitempty"`
	SubmissionClientVersion *string  `json:"submission_client_version,omitempty"`
	RecordingMBID           *string  `json:"recording_mbid,omitempty"`
	ArtistMBIDs             []string `json:"artist_mbids,omitempty"`
	ReleaseMBID             *string  `json:"release_mbid,omitempty"`
	ReleaseGroupMBID        *string  `json:"release_group_mbid,omitempty"`
	TrackMBID               *string  `json:"track_mbid,omitempty"`
	WorkMBIDs               []string `json:"work_mbids,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	DurationMs              *int64   `json:"duration_ms,omitempty"`
	SpotifyID               *string  `json:"spotify_id,omitempty"`
	ISRC                    *string  `json:"isrc,omitempty"`
	TrackNumber             *int     `json:"tracknumber,omitempty"`
	DiscNumber              *int     `json:"discnumber,omitempty"`
	MusicService            *string  `json:"music_service,omitempty"`
	MusicServiceName        *string  `json:"music_service_name,omitempty"`
	OriginURL               *string  `json:"origin_url,omitempty"`
	LastFMTrackURL          *string  `json:"lastfm_track_url,omitempty"`
	YoutubeID               *string  `json:"youtube_id,omitempty"`
}

// ConvertToTrack converts ListenBrainz format to internal Track format
func (lbp *ListenBrainzPayload) ConvertToTrack() Track {
	track := Track{
		Name:   lbp.TrackMetadata.TrackName,
		Artist: []Artist{{Name: lbp.TrackMetadata.ArtistName}},
	}

	// Set timestamp
	if lbp.ListenedAt != nil {
		track.Timestamp = time.Unix(*lbp.ListenedAt, 0)
	} else {
		track.Timestamp = time.Now()
	}

	// Set album/release name
	if lbp.TrackMetadata.ReleaseName != nil {
		track.Album = *lbp.TrackMetadata.ReleaseName
	}

	// Handle additional info if present
	if info := lbp.TrackMetadata.AdditionalInfo; info != nil {
		// Set MBIDs
		if info.RecordingMBID != nil {
			track.RecordingMBID = info.RecordingMBID
		}
		if info.ReleaseMBID != nil {
			track.ReleaseMBID = info.ReleaseMBID
		}

		// Set duration
		if info.DurationMs != nil {
			track.DurationMs = *info.DurationMs
		}

		// Set ISRC
		if info.ISRC != nil {
			track.ISRC = *info.ISRC
		}

		// Handle multiple artists from MBIDs
		if len(info.ArtistMBIDs) > 0 {
			artists := make([]Artist, len(info.ArtistMBIDs))
			for i, mbid := range info.ArtistMBIDs {
				artists[i] = Artist{
					Name: lbp.TrackMetadata.ArtistName, // Use main artist name
					MBID: &mbid,
				}
			}
			track.Artist = artists
		}

		// Set service information
		if info.MusicService != nil {
			track.ServiceBaseUrl = *info.MusicService
		}
		if info.OriginURL != nil {
			track.URL = *info.OriginURL
		}
		if info.SpotifyID != nil {
			track.URL = "https://open.spotify.com/track/" + *info.SpotifyID
			track.ServiceBaseUrl = "spotify"
		}
	}

	// Default service if not set
	if track.ServiceBaseUrl == "" {
		track.ServiceBaseUrl = "listenbrainz"
	}

	// Mark as stamped since it came from external submission
	track.HasStamped = true

	return track
}

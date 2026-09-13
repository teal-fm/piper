package models

import (
	"math"
	"net/url"
	"strings"
	"time"
)

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
	MBIDMapping    *ListenBrainzMBIDMapping    `json:"mbid_mapping,omitempty"`
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
	Duration                *int64   `json:"duration,omitempty"`
	SpotifyID               *string  `json:"spotify_id,omitempty"`
	ISRC                    *string  `json:"isrc,omitempty"`
	// Clients send these fields as either strings or numbers.
	TrackNumber      any     `json:"tracknumber,omitempty"`
	DiscNumber       any     `json:"discnumber,omitempty"`
	MusicService     *string `json:"music_service,omitempty"`
	MusicServiceName *string `json:"music_service_name,omitempty"`
	OriginURL        *string `json:"origin_url,omitempty"`
	RecordingMSID    *string `json:"recording_msid,omitempty"`
	LastFMTrackURL   *string `json:"lastfm_track_url,omitempty"`
	YoutubeID        *string `json:"youtube_id,omitempty"`
}

// ListenBrainzMBIDMapping is metadata resolved by ListenBrainz. Unlike the
// client-supplied values in additional_info, ListenBrainz has checked these
// identifiers against MusicBrainz.
type ListenBrainzMBIDMapping struct {
	RecordingMBID    string                     `json:"recording_mbid"`
	RecordingName    string                     `json:"recording_name"`
	ReleaseMBID      string                     `json:"release_mbid"`
	ReleaseGroupMBID string                     `json:"release_group_mbid"`
	ArtistMBIDs      []string                   `json:"artist_mbids"`
	Artists          []ListenBrainzMappedArtist `json:"artists"`
	CAAID            int64                      `json:"caa_id"`
	CAAReleaseMBID   string                     `json:"caa_release_mbid"`
	URLRels          []ListenBrainzURLRelation  `json:"url_rels"`
}

type ListenBrainzMappedArtist struct {
	ArtistMBID       string `json:"artist_mbid"`
	ArtistCreditName string `json:"artist_credit_name"`
	JoinPhrase       string `json:"join_phrase"`
}

type ListenBrainzURLRelation struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ConvertToTrack converts ListenBrainz format to internal Track format
func (lbp *ListenBrainzPayload) ConvertToTrack() Track {
	track := Track{
		Name:   lbp.TrackMetadata.TrackName,
		Artist: []Artist{{Name: lbp.TrackMetadata.ArtistName}},
	}

	// Set timestamp
	if lbp.ListenedAt != nil {
		track.Timestamp = time.Unix(*lbp.ListenedAt, 0).UTC()
	} else {
		track.Timestamp = time.Now().UTC()
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
		if info.DurationMs != nil && *info.DurationMs > 0 {
			track.DurationMs = *info.DurationMs
		} else if info.Duration != nil && *info.Duration > 0 && *info.Duration <= math.MaxInt64/1000 {
			track.DurationMs = *info.Duration * 1000
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
			if track.ServiceBaseUrl == "" {
				track.ServiceBaseUrl = serviceFromURL(track.URL)
			}
		}
		if info.SpotifyID != nil {
			if strings.Contains(*info.SpotifyID, "://") {
				track.URL = *info.SpotifyID
			} else {
				track.URL = "https://open.spotify.com/track/" + *info.SpotifyID
			}
			track.ServiceBaseUrl = "spotify"
		}
		if track.URL == "" && info.LastFMTrackURL != nil {
			track.URL = *info.LastFMTrackURL
			track.ServiceBaseUrl = "last.fm"
		}
		if track.URL == "" && info.YoutubeID != nil {
			track.URL = "https://www.youtube.com/watch?v=" + *info.YoutubeID
			track.ServiceBaseUrl = "youtube.com"
		}
	}

	// Prefer ListenBrainz's server-resolved mapping over unverified values
	// supplied by the original scrobbling client.
	if mapping := lbp.TrackMetadata.MBIDMapping; mapping != nil {
		if mapping.RecordingMBID != "" {
			track.RecordingMBID = stringPointer(mapping.RecordingMBID)
		}
		if mapping.ReleaseMBID != "" {
			track.ReleaseMBID = stringPointer(mapping.ReleaseMBID)
		}

		if len(mapping.Artists) > 0 {
			track.Artist = make([]Artist, 0, len(mapping.Artists))
			for _, mappedArtist := range mapping.Artists {
				name := mappedArtist.ArtistCreditName
				if name == "" {
					name = lbp.TrackMetadata.ArtistName
				}
				artist := Artist{Name: name, ID: mappedArtist.ArtistMBID}
				if mappedArtist.ArtistMBID != "" {
					artist.MBID = stringPointer(mappedArtist.ArtistMBID)
				}
				track.Artist = append(track.Artist, artist)
			}
		} else if len(mapping.ArtistMBIDs) > 0 {
			track.Artist = make([]Artist, 0, len(mapping.ArtistMBIDs))
			for _, mbid := range mapping.ArtistMBIDs {
				track.Artist = append(track.Artist, Artist{
					Name: lbp.TrackMetadata.ArtistName,
					ID:   mbid,
					MBID: stringPointer(mbid),
				})
			}
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

func stringPointer(value string) *string {
	return &value
}

func serviceFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

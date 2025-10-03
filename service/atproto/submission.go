package atproto

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
)

// SubmitPlayToPDS submits a track play to the ATProto PDS as a feed.play record
func SubmitPlayToPDS(ctx context.Context, did string, track *models.Track, atprotoService *atprotoauth.ATprotoAuthService) error {
	if did == "" {
		return fmt.Errorf("DID cannot be empty")
	}

	// Get ATProto client
	client, err := atprotoService.GetATProtoClient()
	if err != nil || client == nil {
		return fmt.Errorf("failed to get ATProto client: %w", err)
	}

	xrpcClient := atprotoService.GetXrpcClient()
	if xrpcClient == nil {
		return fmt.Errorf("xrpc client is not available")
	}

	// Get user session
	sess, err := atprotoService.DB.GetAtprotoSession(did, ctx, *client)
	if err != nil {
		return fmt.Errorf("couldn't get Atproto session for DID %s: %w", did, err)
	}

	// Convert track to feed.play record
	playRecord, err := TrackToPlayRecord(track)
	if err != nil {
		return fmt.Errorf("failed to convert track to play record: %w", err)
	}

	// Create the record
	input := atproto.RepoCreateRecord_Input{
		Collection: "fm.teal.alpha.feed.play",
		Repo:       sess.DID,
		Record:     &lexutil.LexiconTypeDecoder{Val: playRecord},
	}

	authArgs := db.AtpSessionToAuthArgs(sess)
	var out atproto.RepoCreateRecord_Output
	if err := xrpcClient.Do(ctx, authArgs, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return fmt.Errorf("failed to create play record for DID %s: %w", did, err)
	}

	log.Printf("Successfully submitted play to PDS for DID %s: %s - %s", did, track.Artist[0].Name, track.Name)
	return nil
}

// TrackToPlayRecord converts a models.Track to teal.AlphaFeedPlay
func TrackToPlayRecord(track *models.Track) (*teal.AlphaFeedPlay, error) {
	if track.Name == "" {
		return nil, fmt.Errorf("track name cannot be empty")
	}

	// Convert artists
	artists := make([]*teal.AlphaFeedDefs_Artist, 0, len(track.Artist))
	for _, a := range track.Artist {
		artist := &teal.AlphaFeedDefs_Artist{
			ArtistName: a.Name,
			ArtistMbId: a.MBID,
		}
		artists = append(artists, artist)
	}

	// Prepare optional fields
	var durationPtr *int64
	if track.DurationMs > 0 {
		durationSeconds := track.DurationMs / 1000
		durationPtr = &durationSeconds
	}

	var playedTimeStr *string
	if !track.Timestamp.IsZero() {
		timeStr := track.Timestamp.Format(time.RFC3339)
		playedTimeStr = &timeStr
	}

	var isrcPtr *string
	if track.ISRC != "" {
		isrcPtr = &track.ISRC
	}

	var originUrlPtr *string
	if track.URL != "" {
		originUrlPtr = &track.URL
	}

	var servicePtr *string
	if track.ServiceBaseUrl != "" {
		servicePtr = &track.ServiceBaseUrl
	}

	var releaseNamePtr *string
	if track.Album != "" {
		releaseNamePtr = &track.Album
	}

	// Get submission client agent
	submissionAgent := viper.GetString("app.submission_agent")
	if submissionAgent == "" {
		submissionAgent = "piper/v0.0.1"
	}

	playRecord := &teal.AlphaFeedPlay{
		LexiconTypeID:          "fm.teal.alpha.feed.play",
		TrackName:              track.Name,
		Artists:                artists,
		Duration:               durationPtr,
		PlayedTime:             playedTimeStr,
		RecordingMbId:          track.RecordingMBID,
		ReleaseMbId:            track.ReleaseMBID,
		ReleaseName:            releaseNamePtr,
		Isrc:                   isrcPtr,
		OriginUrl:              originUrlPtr,
		MusicServiceBaseDomain: servicePtr,
		SubmissionClientAgent:  &submissionAgent,
	}

	return playRecord, nil
}

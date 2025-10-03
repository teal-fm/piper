package playingnow

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
)

// PlayingNowService handles publishing current playing status to ATProto
type PlayingNowService struct {
	db             *db.DB
	atprotoService *atprotoauth.ATprotoAuthService
	logger         *log.Logger
}

// NewPlayingNowService creates a new playing now service
func NewPlayingNowService(database *db.DB, atprotoService *atprotoauth.ATprotoAuthService) *PlayingNowService {
	logger := log.New(os.Stdout, "playingnow: ", log.LstdFlags|log.Lmsgprefix)

	return &PlayingNowService{
		db:             database,
		atprotoService: atprotoService,
		logger:         logger,
	}
}

// PublishPlayingNow publishes a currently playing track as actor status
func (p *PlayingNowService) PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error {
	// Get user information to find their DID
	user, err := p.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if user.ATProtoDID == nil {
		p.logger.Printf("User %d has no ATProto DID, skipping playing now", userID)
		return nil
	}

	did := *user.ATProtoDID

	// Get ATProto client
	client, err := p.atprotoService.GetATProtoClient()
	if err != nil || client == nil {
		return fmt.Errorf("failed to get ATProto client: %w", err)
	}

	xrpcClient := p.atprotoService.GetXrpcClient()
	if xrpcClient == nil {
		return fmt.Errorf("xrpc client is not available")
	}

	// Get user session
	sess, err := p.db.GetAtprotoSession(did, ctx, *client)
	if err != nil {
		return fmt.Errorf("couldn't get Atproto session for DID %s: %w", did, err)
	}

	// Convert track to PlayView format
	playView, err := p.trackToPlayView(track)
	if err != nil {
		return fmt.Errorf("failed to convert track to PlayView: %w", err)
	}

	// Create actor status record
	now := time.Now()
	expiry := now.Add(10 * time.Minute) // Default 10 minutes as mentioned in schema

	status := &teal.AlphaActorStatus{
		LexiconTypeID: "fm.teal.alpha.actor.status",
		Time:          strconv.FormatInt(now.Unix(), 10),
		Expiry:        func() *string { s := strconv.FormatInt(expiry.Unix(), 10); return &s }(),
		Item:          playView,
	}

	authArgs := db.AtpSessionToAuthArgs(sess)
	var swapRecord *string
	swapRecord, err = p.getStatusSwapRecord(ctx, xrpcClient, sess, authArgs)
	if err != nil {
		return err
	}

	// Create the record input
	input := atproto.RepoPutRecord_Input{
		Collection: "fm.teal.alpha.actor.status",
		Repo:       sess.DID,
		Rkey:       "self", // Use "self" as the record key for current status
		Record:     &lexutil.LexiconTypeDecoder{Val: status},
		SwapRecord: swapRecord,
	}

	// Submit to PDS
	var out atproto.RepoPutRecord_Output
	if err := xrpcClient.Do(ctx, authArgs, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		p.logger.Printf("Error creating playing now status for DID %s: %v", did, err)
		return fmt.Errorf("failed to create playing now status for DID %s: %w", did, err)
	}

	p.logger.Printf("Successfully published playing now status for user %d (DID: %s): %s - %s",
		userID, did, track.Artist[0].Name, track.Name)

	return nil
}

// ClearPlayingNow removes the current playing status by setting an expired status
func (p *PlayingNowService) ClearPlayingNow(ctx context.Context, userID int64) error {
	// Get user information
	user, err := p.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if user.ATProtoDID == nil {
		p.logger.Printf("User %d has no ATProto DID, skipping clear playing now", userID)
		return nil
	}

	did := *user.ATProtoDID

	// Get ATProto clients
	client, err := p.atprotoService.GetATProtoClient()
	if err != nil || client == nil {
		return fmt.Errorf("failed to get ATProto client: %w", err)
	}

	xrpcClient := p.atprotoService.GetXrpcClient()
	if xrpcClient == nil {
		return fmt.Errorf("xrpc client is not available")
	}

	// Get user session
	sess, err := p.db.GetAtprotoSession(did, ctx, *client)
	if err != nil {
		return fmt.Errorf("couldn't get Atproto session for DID %s: %w", did, err)
	}

	// Create an expired status (essentially clearing it)
	now := time.Now()
	expiredTime := now.Add(-1 * time.Minute) // Set expiry to 1 minute ago

	// Create empty play view
	emptyPlayView := &teal.AlphaFeedDefs_PlayView{
		TrackName: "", // Empty track indicates no current playing
		Artists:   []*teal.AlphaFeedDefs_Artist{},
	}

	status := &teal.AlphaActorStatus{
		LexiconTypeID: "fm.teal.alpha.actor.status",
		Time:          strconv.FormatInt(now.Unix(), 10),
		Expiry:        func() *string { s := strconv.FormatInt(expiredTime.Unix(), 10); return &s }(),
		Item:          emptyPlayView,
	}

	authArgs := db.AtpSessionToAuthArgs(sess)
	var swapRecord *string
	swapRecord, err = p.getStatusSwapRecord(ctx, xrpcClient, sess, authArgs)
	if err != nil {
		return err
	}

	// Update the record
	input := atproto.RepoPutRecord_Input{
		Collection: "fm.teal.alpha.actor.status",
		Repo:       sess.DID,
		Rkey:       "self",
		Record:     &lexutil.LexiconTypeDecoder{Val: status},
		SwapRecord: swapRecord,
	}

	var out atproto.RepoPutRecord_Output
	if err := xrpcClient.Do(ctx, authArgs, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		p.logger.Printf("Error clearing playing now status for DID %s: %v", did, err)
		return fmt.Errorf("failed to clear playing now status for DID %s: %w", did, err)
	}

	p.logger.Printf("Successfully cleared playing now status for user %d (DID: %s)", userID, did)
	return nil
}

// trackToPlayView converts a models.Track to teal.AlphaFeedDefs_PlayView
func (p *PlayingNowService) trackToPlayView(track *models.Track) (*teal.AlphaFeedDefs_PlayView, error) {
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

	playView := &teal.AlphaFeedDefs_PlayView{
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

	return playView, nil
}

// getStatusSwapRecord retrieves the current swap record (CID) for the actor status record.
// Returns (nil, nil) if the record does not exist yet.
func (p *PlayingNowService) getStatusSwapRecord(ctx context.Context, xrpcClient *oauth.XrpcClient, sess *models.ATprotoAuthSession, authArgs *oauth.XrpcAuthedRequestArgs) (*string, error) {
	getOutput := atproto.RepoGetRecord_Output{}
	if err := xrpcClient.Do(ctx, authArgs, xrpc.Query, "application/json", "com.atproto.repo.getRecord", map[string]any{
		"repo":       sess.DID,
		"collection": "fm.teal.alpha.actor.status",
		"rkey":       "self",
	}, nil, &getOutput); err != nil {
		xErr, ok := err.(*xrpc.Error)
		if !ok {
			return nil, fmt.Errorf("could not get record: %w", err)
		}
		if xErr.StatusCode != 400 { // 400 means not found in this API
			return nil, fmt.Errorf("could not get record: %w", err)
		}
		return nil, nil
	}
	return getOutput.Cid, nil
}

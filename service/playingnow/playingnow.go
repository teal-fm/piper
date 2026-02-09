package playingnow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/client"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/spf13/viper"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
)

// Service handles publishing current playing status to ATProto
type Service struct {
	db             *db.DB
	atprotoService *atprotoauth.AuthService
	logger         *log.Logger
	mu             sync.RWMutex
	clearedStatus  map[int64]bool // tracks if a user's status has been cleared on their repo
}

// NewPlayingNowService creates a new playing now service
func NewPlayingNowService(database *db.DB, atprotoService *atprotoauth.AuthService) *Service {
	logger := log.New(os.Stdout, "playingnow: ", log.LstdFlags|log.Lmsgprefix)

	return &Service{
		db:             database,
		atprotoService: atprotoService,
		logger:         logger,
		clearedStatus:  make(map[int64]bool),
	}
}

// PublishPlayingNow publishes a currently playing track as actor status
func (p *Service) PublishPlayingNow(ctx context.Context, userID int64, track *models.Track) error {
	// Get user information to find their DID
	user, err := p.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found user ID: %d", userID)
	}

	if user.ATProtoDID == nil {
		p.logger.Printf("User %d has no ATProto DID, skipping playing now", userID)
		return nil
	}

	did := *user.ATProtoDID

	// Get ATProto atProtoClient
	atProtoClient, err := p.atprotoService.GetATProtoClient(did, *user.MostRecentAtProtoSessionID, ctx)
	if err != nil || atProtoClient == nil {
		return fmt.Errorf("failed to get ATProto atProtoClient: %w", err)
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
		Time:          now.Format(time.RFC3339),
		Expiry:        func() *string { s := expiry.Format(time.RFC3339); return &s }(),
		Item:          playView,
	}

	var swapRecord *comatproto.RepoGetRecord_Output
	swapRecord, err = p.getStatusSwapRecord(ctx, atProtoClient)
	if err != nil {
		return err
	}

	var swapCid *string
	if swapRecord != nil {
		swapCid = swapRecord.Cid
	}

	p.logger.Printf("Publishing playing now status for user %d (DID: %s): %s - %s", userID, did, track.Artist[0].Name, track.Name)

	// Create the record input
	input := comatproto.RepoPutRecord_Input{
		Collection: "fm.teal.alpha.actor.status",
		Repo:       atProtoClient.AccountDID.String(),
		Rkey:       "self", // Use "self" as the record key for current status
		Record:     &lexutil.LexiconTypeDecoder{Val: status},
		SwapRecord: swapCid,
	}

	// Submit to PDS
	if _, err := comatproto.RepoPutRecord(ctx, atProtoClient, &input); err != nil {
		p.logger.Printf("Error creating playing now status for DID %s: %v", did, err)
		return fmt.Errorf("failed to create playing now status for DID %s: %w", did, err)
	}

	// Resets clear to false since there is a song playing. The publish playing state is kept in the services from
	// if a song has changed/stamped
	p.mu.Lock()
	p.clearedStatus[userID] = false
	p.mu.Unlock()

	return nil
}

// ClearPlayingNow removes the current playing status by setting an expired status
func (p *Service) ClearPlayingNow(ctx context.Context, userID int64) error {
	// Check if status is already cleared to avoid clearing on the users repo over and over
	p.mu.RLock()
	alreadyCleared := p.clearedStatus[userID]
	p.mu.RUnlock()

	if alreadyCleared {
		return nil
	}

	// Get user information
	user, err := p.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		return fmt.Errorf("user not found user ID: %d", userID)
	}

	if user.ATProtoDID == nil {
		p.logger.Printf("User %d has no ATProto DID, skipping clear playing now", userID)
		return nil
	}

	did := *user.ATProtoDID

	// Get ATProto clients
	atProtoClient, err := p.atprotoService.GetATProtoClient(did, *user.MostRecentAtProtoSessionID, ctx)
	if err != nil || atProtoClient == nil {
		return fmt.Errorf("failed to get ATProto atProtoClient: %w", err)
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
		Time:          now.Format(time.RFC3339),
		Expiry:        func() *string { s := expiredTime.Format(time.RFC3339); return &s }(),
		Item:          emptyPlayView,
	}

	var swapRecord *comatproto.RepoGetRecord_Output
	swapRecord, err = p.getStatusSwapRecord(ctx, atProtoClient)

	if err != nil {
		return err
	}

	var swapCid *string
	if swapRecord != nil {
		swapCid = swapRecord.Cid
	}

	// Update the record
	input := comatproto.RepoPutRecord_Input{
		Collection: "fm.teal.alpha.actor.status",
		Repo:       atProtoClient.AccountDID.String(),
		Rkey:       "self",
		Record:     &lexutil.LexiconTypeDecoder{Val: status},
		SwapRecord: swapCid,
	}

	if _, err := comatproto.RepoPutRecord(ctx, atProtoClient, &input); err != nil {
		p.logger.Printf("Error clearing playing now status for DID %s: %v", did, err)
		return fmt.Errorf("failed to clear playing now status for DID %s: %w", did, err)
	}

	p.logger.Printf("Successfully cleared playing now status for user %d (DID: %s)", userID, did)

	// Mark status as cleared so we don't clear again until user starts playing a song again
	p.mu.Lock()
	p.clearedStatus[userID] = true
	p.mu.Unlock()

	return nil
}

// trackToPlayView converts a models.Track to teal.AlphaFeedDefs_PlayView
func (p *Service) trackToPlayView(track *models.Track) (*teal.AlphaFeedDefs_PlayView, error) {
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
		submissionAgent = models.SubmissionAgent
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
func (p *Service) getStatusSwapRecord(ctx context.Context, atApiClient *client.APIClient) (*comatproto.RepoGetRecord_Output, error) {
	result, err := comatproto.RepoGetRecord(ctx, atApiClient, "", "fm.teal.alpha.actor.status", atApiClient.AccountDID.String(), "self")

	if err != nil {
		var xErr *client.APIError
		ok := errors.As(err, &xErr)
		if !ok {
			return nil, fmt.Errorf("error getting the record: %w", err)
		}
		if xErr.StatusCode == 400 { // 400 means not found in this API, which would be the case if the record does not exist yet
			return nil, nil
		}

		return nil, fmt.Errorf("error getting the record: %w", err)

	}

	return result, nil
}

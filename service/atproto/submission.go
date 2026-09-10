package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/client"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/spf13/viper"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
)

// PublishStoredPlay submits a saved, eligible play using the user's current session.
// The durable claim is shared by ingestion and manual retries.
func PublishStoredPlay(ctx context.Context, database *db.DB, userID, trackID int64, auth *atprotoauth.AuthService) error {
	return publishStoredPlay(ctx, database, userID, trackID, func(ctx context.Context, user *models.User, key string, record *teal.FeedPlay) error {
		if auth == nil || user.ATProtoDID == nil || user.MostRecentAtProtoSessionID == nil || *user.MostRecentAtProtoSessionID == "" {
			return errors.New("Sign in to Piper again to reconnect publishing.")
		}
		client, err := auth.GetATProtoClient(*user.ATProtoDID, *user.MostRecentAtProtoSessionID, ctx)
		if err != nil {
			return fmt.Errorf("opening OAuth session: %s", submissionError(err))
		}
		if client == nil {
			return errors.New("OAuth session returned no client. Sign in to Piper again.")
		}
		return createPlayRecord(ctx, client, *user.ATProtoDID, key, record)
	})
}

type playPublisher func(context.Context, *models.User, string, *teal.FeedPlay) error

func publishStoredPlay(ctx context.Context, database *db.DB, userID, trackID int64, publish playPublisher) error {
	p, err := database.ClaimSubmission(userID, trackID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	err = func() error {
		user, err := database.GetUserByID(userID)
		if err != nil {
			return errors.New("Could not load the publishing account.")
		}
		if user == nil {
			return errors.New("Publishing account was not found.")
		}
		var record *teal.FeedPlay
		if p.RecordJSON.Valid {
			if err := json.Unmarshal([]byte(p.RecordJSON.String), &record); err != nil {
				return errors.New("Could not read saved play record.")
			}
		} else {
			track, err := database.GetTrackForUser(userID, trackID)
			if err != nil {
				return errors.New("Could not load saved play.")
			}
			record, err = TrackToPlayRecord(track)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				return errors.New("Could not encode saved play.")
			}
			if err := database.SaveSubmissionRecord(p, string(encoded)); err != nil {
				return errors.New("Could not save publishing record.")
			}
		}
		return publish(ctx, user, p.RKey, record)
	}()
	message := ""
	if err != nil {
		message = err.Error()
	}
	if saveErr := database.FinishSubmission(p, message); saveErr != nil {
		log.Printf("play_submission user_id=%d play_id=%d attempt=%d outcome=persistence_failed error=%q", userID, trackID, p.Attempts, saveErr)
		return fmt.Errorf("saving publishing outcome: %w", saveErr)
	}
	outcome := "published"
	if err != nil {
		outcome = "failed"
	}
	log.Printf("play_submission user_id=%d play_id=%d rkey=%s attempt=%d outcome=%s error=%q", userID, trackID, p.RKey, p.Attempts, outcome, message)
	return err
}

// createRecord retains create-only OAuth permissions. If the write succeeded
// but its response was lost, verify the existing record at the same key.
func createPlayRecord(ctx context.Context, client lexutil.LexClient, did, key string, record *teal.FeedPlay) error {
	input := comatproto.RepoCreateRecord_Input{Collection: "fm.teal.feed.play", Repo: did, Rkey: &key, Record: &lexutil.LexiconTypeDecoder{Val: record}}
	if _, err := comatproto.RepoCreateRecord(ctx, client, &input); err != nil {
		existing, readErr := comatproto.RepoGetRecord(ctx, client, "", "fm.teal.feed.play", did, key)
		if readErr == nil && existing != nil && existing.Value != nil {
			// Compare decoded JSON so field order does not affect equality.
			a, _ := json.Marshal(existing.Value)
			b, _ := json.Marshal(record)
			var av, bv any
			if json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv) {
				return nil
			}
		}
		return fmt.Errorf("publishing play: %s", submissionError(err))
	}
	return nil
}

var httpStatusPattern = regexp.MustCompile(`HTTP[ )]*(\d{3})`)
var errorNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// Never persist arbitrary response bodies, URLs, or token-bearing OAuth errors.
// Keep the actionable code, HTTP status, and operation instead.
func submissionError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Request timed out. Retry this play."
	}
	if errors.Is(err, context.Canceled) {
		return "Request was interrupted. Retry this play."
	}
	detail := "Request failed. Retry this play."
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		detail = fmt.Sprintf("HTTP %d", apiErr.StatusCode)
		if errorNamePattern.MatchString(apiErr.Name) {
			detail += ": " + apiErr.Name
		}
	} else if match := httpStatusPattern.FindStringSubmatch(err.Error()); len(match) > 1 {
		detail = "HTTP " + match[1]
	}
	if strings.Contains(err.Error(), "invalid_grant") {
		detail += ": invalid_grant. Sign in to Piper again, then retry."
	}
	if strings.Contains(err.Error(), "failed to refresh OAuth tokens") || strings.Contains(err.Error(), "token refresh failed") {
		detail = "Failed to refresh OAuth tokens. " + detail
	}
	return detail
}

// TrackToPlayRecord converts a models.Track to teal.FeedPlay
func TrackToPlayRecord(track *models.Track) (*teal.FeedPlay, error) {
	if track.Name == "" {
		return nil, fmt.Errorf("track name cannot be empty")
	}

	// Convert artists
	artists := make([]*teal.FeedDefs_Artist, 0, len(track.Artist))
	for _, a := range track.Artist {
		artist := &teal.FeedDefs_Artist{
			ArtistName: a.Name,
			ArtistMbId: models.FormatMBIDURI(a.MBID),
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

	originURI := models.FormatOriginURI(track.URL)

	serviceURI := models.FormatMusicServiceURI(track.ServiceBaseUrl)

	var releaseNamePtr *string
	if track.Album != "" {
		releaseNamePtr = &track.Album
	}

	// Get submission client agent
	submissionAgent := viper.GetString("app.submission_agent")
	if submissionAgent == "" {
		submissionAgent = models.SubmissionAgent
	}

	playRecord := &teal.FeedPlay{
		LexiconTypeID:         "fm.teal.feed.play",
		TrackName:             track.Name,
		Artists:               artists,
		Duration:              durationPtr,
		PlayedTime:            playedTimeStr,
		RecordingMbId:         models.FormatMBIDURI(track.RecordingMBID),
		ReleaseMbId:           models.FormatMBIDURI(track.ReleaseMBID),
		ReleaseName:           releaseNamePtr,
		Isrc:                  isrcPtr,
		OriginUri:             originURI,
		MusicServiceUri:       serviceURI,
		SubmissionClientAgent: &submissionAgent,
	}

	return playRecord, nil
}

package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teal-fm/piper/api/teal"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
)

func TestFailedSubmissionSurvivesRestartAndRetriesOriginalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piper.db")
	database, err := db.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	uid, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	playedAt := time.Date(2026, 9, 3, 18, 3, 0, 0, time.UTC)
	track := &models.Track{Name: "Original title", Timestamp: playedAt, HasStamped: true}
	id, err := database.SaveTrack(uid, db.SourceAppleMusic, track)
	if err != nil {
		t.Fatal(err)
	}
	var originalKey string
	err = publishStoredPlay(context.Background(), database, uid, id, func(ctx context.Context, u *models.User, key string, r *teal.FeedPlay) error {
		originalKey = key
		return errors.New("publishing play: HTTP 400: invalid_grant. Sign in to Piper again, then retry.")
	})
	if err == nil {
		t.Fatal("expected publishing failure")
	}
	plays, err := database.ListUnpublishedPlays(uid, 20, 0)
	if err != nil || len(plays) != 1 || plays[0].Attempts != 1 || !strings.Contains(plays[0].LastError, "invalid_grant") {
		t.Fatalf("failure not saved: %+v %v", plays, err)
	}
	database.Close()
	database, err = db.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	// Later hydration cannot change the record sent by a retry.
	if _, err := database.Exec(`UPDATE tracks SET name='Changed title' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	err = publishStoredPlay(context.Background(), database, uid, id, func(ctx context.Context, u *models.User, key string, r *teal.FeedPlay) error {
		if key != originalKey || r.TrackName != "Original title" || r.PlayedTime == nil || *r.PlayedTime != playedAt.Format(time.RFC3339) {
			t.Fatalf("retry changed original record: %s %+v", key, r)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	plays, err = database.ListUnpublishedPlays(uid, 20, 0)
	if err != nil || len(plays) != 0 {
		t.Fatalf("successful retry still listed: %+v %v", plays, err)
	}
}

func TestCreatePlayRecordVerifiesAmbiguousWrite(t *testing.T) {
	for _, matches := range []bool{true, false} {
		t.Run(fmt.Sprint(matches), func(t *testing.T) {
			record := &teal.FeedPlay{LexiconTypeID: "fm.teal.feed.play", TrackName: "Track"}
			key := "3jzfcijpj2z2a"
			creates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					var input struct {
						RKey string `json:"rkey"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					if input.RKey != key {
						t.Errorf("unstable key %q", input.RKey)
					}
					creates++
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, `{"error":"InternalServerError"}`)
					return
				}
				if r.URL.Query().Get("rkey") != key {
					t.Errorf("wrong verification key")
				}
				existing := *record
				if !matches {
					existing.TrackName = "Different play"
				}
				json.NewEncoder(w).Encode(map[string]any{"uri": "at://did:plc:test/fm.teal.feed.play/" + key, "value": existing})
			}))
			defer server.Close()
			err := createPlayRecord(context.Background(), &xrpc.Client{Host: server.URL, Client: server.Client()}, "did:plc:test", key, record)
			if matches && err != nil {
				t.Fatal(err)
			}
			if !matches && err == nil {
				t.Fatal("different remote record counted as successful")
			}
			if creates != 1 {
				t.Fatalf("unexpected create count %d", creates)
			}
		})
	}
}

func TestSubmissionErrorsKeepActionableDetailsWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("failed to refresh OAuth tokens: token refresh failed (HTTP 400): invalid_grant secret-token"), "HTTP 400: invalid_grant"},
		{&client.APIError{StatusCode: 429, Name: "RateLimitExceeded", Message: "secret-token"}, "HTTP 429: RateLimitExceeded"},
		{errors.New("https://secret-token@host/?refresh_token=secret-token"), "Request failed"},
		{context.DeadlineExceeded, "timed out"},
	} {
		got := submissionError(tc.err)
		if !strings.Contains(got, tc.want) || strings.Contains(got, "secret-token") {
			t.Errorf("unsafe or unhelpful error %q", got)
		}
	}
}

func TestOverlappingRetriesPublishOnce(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "piper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	uid, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := database.SaveTrack(uid, db.SourceAppleMusic, &models.Track{Name: "One play", HasStamped: true, Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- publishStoredPlay(context.Background(), database, uid, id, func(ctx context.Context, u *models.User, key string, r *teal.FeedPlay) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("first retry exited before publishing: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first retry did not start")
	}
	err = publishStoredPlay(context.Background(), database, uid, id, func(ctx context.Context, u *models.User, key string, r *teal.FeedPlay) error {
		t.Error("overlapping retry attempted a second publish")
		return nil
	})
	close(release)
	if !errors.Is(err, db.ErrSubmissionUnavailable) {
		t.Errorf("overlapping retry = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

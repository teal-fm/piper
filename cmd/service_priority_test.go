package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/session"
)

type stubResolver struct {
	track *models.Track
	err   error
}

func (s stubResolver) ResolveCurrentTrack(userID int64) (*models.Track, string, error) {
	if s.track == nil {
		return nil, "", s.err
	}
	return s.track, s.track.ServiceBaseUrl, s.err
}

func setupCmdTestDB(t *testing.T) (*db.DB, int64) {
	t.Helper()

	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return database, userID
}

func TestAPIUpdateServicePriorityHandler(t *testing.T) {
	database, userID := setupCmdTestDB(t)

	body, _ := json.Marshal(map[string]string{"service_priority": "applemusic,spotify"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/preferences/service-priority", bytes.NewReader(body))
	req = req.WithContext(session.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()

	apiUpdateServicePriorityHandler(database).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if user.ServicePriority != "applemusic,spotify,lastfm" {
		t.Fatalf("expected service priority to be updated, got %q", user.ServicePriority)
	}
}

func TestAPIUpdateServicePriorityHandlerRejectsInvalidPriority(t *testing.T) {
	database, userID := setupCmdTestDB(t)

	body, _ := json.Marshal(map[string]string{"service_priority": "tidal,spotify"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/preferences/service-priority", bytes.NewReader(body))
	req = req.WithContext(session.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()

	apiUpdateServicePriorityHandler(database).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}

func TestAPIResolvedCurrentTrackUsesCoordinator(t *testing.T) {
	database, userID := setupCmdTestDB(t)
	track := &models.Track{
		Name:           "Resolver Song",
		Artist:         []models.Artist{{Name: "Resolver Artist"}},
		Album:          "Resolver Album",
		URL:            "https://music.apple.com/track/1",
		ServiceBaseUrl: "music.apple.com",
		Timestamp:      time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/current-track", nil)
	req = req.WithContext(session.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()

	apiResolvedCurrentTrack(database, stubResolver{track: track}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var decoded models.Track
	if err := json.NewDecoder(rr.Body).Decode(&decoded); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if decoded.Name != track.Name {
		t.Fatalf("expected track %q, got %q", track.Name, decoded.Name)
	}
}

func TestAPIResolvedCurrentTrackReturnsNilWhenNoTrack(t *testing.T) {
	database, userID := setupCmdTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/current-track", nil)
	req = req.WithContext(session.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()

	apiResolvedCurrentTrack(database, stubResolver{}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "" && strings.TrimSpace(rr.Body.String()) != "null" {
		t.Fatalf("expected empty or null body, got %q", rr.Body.String())
	}
}

func TestServicePriorityOptionsIncludesLastFMOrders(t *testing.T) {
	options := servicePriorityOptions("lastfm,spotify,applemusic", []string{
		models.ServiceSpotify,
		models.ServiceAppleMusic,
		models.ServiceLastFM,
	})

	if len(options) != 6 {
		t.Fatalf("expected 6 permutations for 3 linked services, got %d", len(options))
	}

	found := false
	for _, option := range options {
		if option.Value == "lastfm,spotify,applemusic" && option.Selected {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected selected option for lastfm,spotify,applemusic")
	}
}

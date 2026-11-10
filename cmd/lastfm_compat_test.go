package main

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/session"
)

func lastfmCompatTestHandler(t *testing.T, database *db.DB) http.HandlerFunc {
	t.Helper()
	sessionManager := session.NewSessionManager(database)
	return apiLastfmCompatHandler(database, sessionManager, nil, nil, nil)
}

func lastfmCompatRequest(method string, values url.Values) *http.Request {
	if values == nil {
		values = url.Values{}
	}
	if method != "" {
		values.Set("method", method)
	}
	request := httptest.NewRequest(http.MethodPost, "/2.0/", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func decodeJSONResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("failed to decode JSON response %q: %v", recorder.Body.String(), err)
	}
}

func TestLastfmCompatAuthTokenAndSession(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)
	if err := database.AddLastFMUsername(userID, "test-listener"); err != nil {
		t.Fatalf("failed to link Last.fm username: %v", err)
	}

	handler := lastfmCompatTestHandler(t, database)

	tokenRequest := lastfmCompatRequest("auth.getToken", url.Values{
		"api_key": {apiKey},
		"format":  {"json"},
	})
	tokenRecorder := httptest.NewRecorder()
	handler(tokenRecorder, tokenRequest)

	if tokenRecorder.Code != http.StatusOK {
		t.Fatalf("expected token status 200, got %d: %s", tokenRecorder.Code, tokenRecorder.Body.String())
	}
	var tokenResponse lastfmJSONTokenResponse
	decodeJSONResponse(t, tokenRecorder, &tokenResponse)
	if tokenResponse.Token != apiKey {
		t.Fatalf("expected token %q, got %q", apiKey, tokenResponse.Token)
	}

	sessionRequest := lastfmCompatRequest("auth.getSession", url.Values{
		"api_key": {apiKey},
		"token":   {tokenResponse.Token},
		"format":  {"json"},
	})
	sessionRecorder := httptest.NewRecorder()
	handler(sessionRecorder, sessionRequest)

	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("expected session status 200, got %d: %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	var sessionResponse lastfmJSONSessionResponse
	decodeJSONResponse(t, sessionRecorder, &sessionResponse)
	if sessionResponse.Session.Key != apiKey {
		t.Fatalf("expected session key %q, got %q", apiKey, sessionResponse.Session.Key)
	}
	if sessionResponse.Session.Name != "test-listener" {
		t.Fatalf("expected session name %q, got %q", "test-listener", sessionResponse.Session.Name)
	}
}

func TestLastfmCompatScrobbleJSONBatch(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	request := lastfmCompatRequest("track.scrobble", url.Values{
		"sk":             {apiKey},
		"format":         {"json"},
		"artist[0]":      {"Daft Punk"},
		"track[0]":       {"One More Time"},
		"album[0]":       {"Discovery"},
		"timestamp[0]":   {"1704067200"},
		"duration[0]":    {"320"},
		"mbid[0]":        {"98255a8c-017a-4bc7-8dd6-1fa36124572b"},
		"artist[1]":      {"The Beatles"},
		"track[1]":       {"Hey Jude"},
		"timestamp[1]":   {"1704067300"},
		"albumArtist[1]": {"The Beatles"},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Scrobbles struct {
			Attr struct {
				Accepted int `json:"accepted"`
				Ignored  int `json:"ignored"`
			} `json:"@attr"`
			Scrobble []lastfmJSONScrobble `json:"scrobble"`
		} `json:"scrobbles"`
	}
	decodeJSONResponse(t, recorder, &response)

	if response.Scrobbles.Attr.Accepted != 2 {
		t.Fatalf("expected 2 accepted scrobbles, got %d", response.Scrobbles.Attr.Accepted)
	}
	if response.Scrobbles.Attr.Ignored != 0 {
		t.Fatalf("expected 0 ignored scrobbles, got %d", response.Scrobbles.Attr.Ignored)
	}
	if len(response.Scrobbles.Scrobble) != 2 {
		t.Fatalf("expected 2 scrobble results, got %d", len(response.Scrobbles.Scrobble))
	}
	if response.Scrobbles.Scrobble[0].Track.Text != "One More Time" {
		t.Fatalf("unexpected first track %q", response.Scrobbles.Scrobble[0].Track.Text)
	}
	if response.Scrobbles.Scrobble[0].IgnoredMessage.Code != "0" {
		t.Fatalf("expected accepted ignored message code 0, got %q", response.Scrobbles.Scrobble[0].IgnoredMessage.Code)
	}

	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("failed to load saved tracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 saved tracks, got %d", len(tracks))
	}
	if tracks[1].Name != "One More Time" {
		t.Fatalf("expected first saved track to be One More Time, got %q", tracks[1].Name)
	}
	if tracks[1].DurationMs != 320000 {
		t.Fatalf("expected duration 320000ms, got %d", tracks[1].DurationMs)
	}
	if tracks[1].RecordingMBID == nil || *tracks[1].RecordingMBID != "98255a8c-017a-4bc7-8dd6-1fa36124572b" {
		t.Fatalf("expected recording MBID to be saved")
	}
}

func TestLastfmCompatScrobbleXMLByDefault(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	request := lastfmCompatRequest("track.scrobble", url.Values{
		"api_key":      {apiKey},
		"artist[0]":    {"Aphex Twin"},
		"track[0]":     {"Xtal"},
		"timestamp[0]": {"1704067200"},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "xml") {
		t.Fatalf("expected XML content type, got %q", contentType)
	}

	var response struct {
		XMLName   xml.Name `xml:"lfm"`
		Status    string   `xml:"status,attr"`
		Scrobbles struct {
			Accepted int `xml:"accepted,attr"`
			Ignored  int `xml:"ignored,attr"`
			Scrobble []struct {
				Track struct {
					Text string `xml:",chardata"`
				} `xml:"track"`
				Timestamp int64 `xml:"timestamp"`
			} `xml:"scrobble"`
		} `xml:"scrobbles"`
	}
	if err := xml.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode XML response %q: %v", recorder.Body.String(), err)
	}
	if response.Status != "ok" {
		t.Fatalf("expected status ok, got %q", response.Status)
	}
	if response.Scrobbles.Accepted != 1 {
		t.Fatalf("expected 1 accepted scrobble, got %d", response.Scrobbles.Accepted)
	}
	if len(response.Scrobbles.Scrobble) != 1 || response.Scrobbles.Scrobble[0].Track.Text != "Xtal" {
		t.Fatalf("unexpected XML scrobbles: %+v", response.Scrobbles.Scrobble)
	}
	if response.Scrobbles.Scrobble[0].Timestamp != 1704067200 {
		t.Fatalf("unexpected XML timestamp %d", response.Scrobbles.Scrobble[0].Timestamp)
	}

	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("failed to load saved tracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 saved track, got %d", len(tracks))
	}
}

func TestLastfmCompatScrobbleRetryIsIdempotent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	userID, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	values := url.Values{
		"sk":           {apiKey},
		"format":       {"json"},
		"artist[0]":    {"Burial"},
		"track[0]":     {"Archangel"},
		"timestamp[0]": {"1704067200"},
	}

	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		handler(recorder, lastfmCompatRequest("track.scrobble", values))
		if recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected status 200, got %d: %s", attempt, recorder.Code, recorder.Body.String())
		}
		var response struct {
			Scrobbles struct {
				Attr struct {
					Accepted int `json:"accepted"`
				} `json:"@attr"`
			} `json:"scrobbles"`
		}
		decodeJSONResponse(t, recorder, &response)
		if response.Scrobbles.Attr.Accepted != 1 {
			t.Fatalf("attempt %d: expected duplicate to be accepted, got %d", attempt, response.Scrobbles.Attr.Accepted)
		}
	}

	tracks, err := database.GetRecentTracks(userID, 10)
	if err != nil {
		t.Fatalf("failed to load saved tracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected retry to store one track, got %d", len(tracks))
	}
}

func TestLastfmCompatScrobbleIgnoresIncompleteScrobbles(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	_, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	request := lastfmCompatRequest("track.scrobble", url.Values{
		"sk":           {apiKey},
		"format":       {"json"},
		"artist[0]":    {"Missing Timestamp"},
		"track[0]":     {"No Date"},
		"track[1]":     {"No Artist"},
		"timestamp[1]": {"1704067200"},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Scrobbles struct {
			Attr struct {
				Accepted int `json:"accepted"`
				Ignored  int `json:"ignored"`
			} `json:"@attr"`
			Scrobble []lastfmJSONScrobble `json:"scrobble"`
		} `json:"scrobbles"`
	}
	decodeJSONResponse(t, recorder, &response)
	if response.Scrobbles.Attr.Accepted != 0 || response.Scrobbles.Attr.Ignored != 2 {
		t.Fatalf("expected 0 accepted and 2 ignored, got %+v", response.Scrobbles.Attr)
	}
	if response.Scrobbles.Scrobble[0].IgnoredMessage.Code != "3" {
		t.Fatalf("expected missing timestamp to be ignored with code 3, got %q", response.Scrobbles.Scrobble[0].IgnoredMessage.Code)
	}
	if response.Scrobbles.Scrobble[1].IgnoredMessage.Code != "1" {
		t.Fatalf("expected missing artist to be ignored with code 1, got %q", response.Scrobbles.Scrobble[1].IgnoredMessage.Code)
	}
}

func TestLastfmCompatRejectsInvalidCredentials(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	_, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	tests := []struct {
		name         string
		values       url.Values
		expectedCode int
	}{
		{
			name: "missing key",
			values: url.Values{
				"artist[0]":    {"A"},
				"track[0]":     {"B"},
				"timestamp[0]": {"1704067200"},
			},
			expectedCode: lastfmErrInvalidAPIKey,
		},
		{
			name: "invalid session key",
			values: url.Values{
				"sk":           {"not-a-key"},
				"artist[0]":    {"A"},
				"track[0]":     {"B"},
				"timestamp[0]": {"1704067200"},
			},
			expectedCode: lastfmErrInvalidSession,
		},
		{
			name: "invalid api key",
			values: url.Values{
				"api_key":      {"not-a-key"},
				"artist[0]":    {"A"},
				"track[0]":     {"B"},
				"timestamp[0]": {"1704067200"},
			},
			expectedCode: lastfmErrInvalidAPIKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.values.Set("method", "track.scrobble")
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/2.0/", strings.NewReader(test.values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handler(recorder, request)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("expected status 403, got %d: %s", recorder.Code, recorder.Body.String())
			}
			var xmlResponse lastfmXMLErrorResponse
			if err := xml.Unmarshal(recorder.Body.Bytes(), &xmlResponse); err != nil {
				t.Fatalf("failed to decode error response %q: %v", recorder.Body.String(), err)
			}
			if xmlResponse.Error.Code != test.expectedCode {
				t.Fatalf("expected error code %d, got %d", test.expectedCode, xmlResponse.Error.Code)
			}
		})
	}

	// JSON errors follow Last.fm's {"error": code, "message": ...} shape.
	jsonRequest := lastfmCompatRequest("track.scrobble", url.Values{
		"format":       {"json"},
		"artist[0]":    {"A"},
		"track[0]":     {"B"},
		"timestamp[0]": {"1704067200"},
	})
	jsonRecorder := httptest.NewRecorder()
	handler(jsonRecorder, jsonRequest)
	if jsonRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected JSON error status 403, got %d: %s", jsonRecorder.Code, jsonRecorder.Body.String())
	}
	var jsonError lastfmJSONErrorResponse
	decodeJSONResponse(t, jsonRecorder, &jsonError)
	if jsonError.Error != lastfmErrInvalidAPIKey {
		t.Fatalf("expected JSON error code %d, got %d", lastfmErrInvalidAPIKey, jsonError.Error)
	}

	// A valid key supplied through the Authorization header is accepted.
	request := lastfmCompatRequest("track.updateNowPlaying", url.Values{
		"artist": {"A"},
		"track":  {"B"},
	})
	request.Header.Set("Authorization", "Bearer "+apiKey)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected Authorization auth to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestLastfmCompatUpdateNowPlaying(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	_, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	request := lastfmCompatRequest("track.updateNowPlaying", url.Values{
		"sk":       {apiKey},
		"format":   {"json"},
		"artist":   {"Floating Points"},
		"track":    {"Promises"},
		"album":    {"Promises"},
		"duration": {"285"},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response lastfmJSONNowPlayingResponse
	decodeJSONResponse(t, recorder, &response)
	if response.NowPlaying.Track.Text != "Promises" {
		t.Fatalf("unexpected now playing track %q", response.NowPlaying.Track.Text)
	}
	if response.NowPlaying.Artist.Text != "Floating Points" {
		t.Fatalf("unexpected now playing artist %q", response.NowPlaying.Artist.Text)
	}
	if response.NowPlaying.Album.Text != "Promises" {
		t.Fatalf("unexpected now playing album %q", response.NowPlaying.Album.Text)
	}
}

func TestLastfmCompatUnknownMethod(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	_, apiKey := createTestUser(t, database)
	handler := lastfmCompatTestHandler(t, database)

	request := lastfmCompatRequest("user.getInfo", url.Values{
		"api_key": {apiKey},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response lastfmXMLErrorResponse
	if err := xml.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Error.Code != lastfmErrInvalidMethod {
		t.Fatalf("expected error code %d, got %d", lastfmErrInvalidMethod, response.Error.Code)
	}
}

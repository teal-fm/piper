package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/service/listenbrainz"
)

func TestListenBrainzAccountAPI(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	userID, _ := createTestUser(t, database)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token lb-secret" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "valid": true, "user_name": "listener",
		})
	}))
	defer server.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	service := listenbrainz.NewService(database, server.URL, "piper/test (test@example.com)", nil, nil, nil)

	setRequest := httptest.NewRequest(http.MethodPost, "/api/v1/listenbrainz/set", bytes.NewBufferString(`{"token":"lb-secret"}`))
	setRequest = setRequest.WithContext(withUserContext(setRequest.Context(), userID))
	setResponse := httptest.NewRecorder()
	apiLinkListenBrainzHandler(database, service).ServeHTTP(setResponse, setRequest)
	if setResponse.Code != http.StatusOK {
		t.Fatalf("set status = %d, body = %s", setResponse.Code, setResponse.Body.String())
	}
	if strings.Contains(setResponse.Body.String(), "lb-secret") {
		t.Fatal("set response leaked the ListenBrainz token")
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/listenbrainz", nil)
	getRequest = getRequest.WithContext(withUserContext(getRequest.Context(), userID))
	getResponse := httptest.NewRecorder()
	apiGetListenBrainzHandler(database).ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"listenbrainz_username":"listener"`) {
		t.Fatalf("get status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	if strings.Contains(getResponse.Body.String(), "lb-secret") {
		t.Fatal("get response leaked the ListenBrainz token")
	}

	unsetRequest := httptest.NewRequest(http.MethodPost, "/api/v1/listenbrainz/unset", nil)
	unsetRequest = unsetRequest.WithContext(withUserContext(unsetRequest.Context(), userID))
	unsetResponse := httptest.NewRecorder()
	apiUnlinkListenBrainzHandler(database, service).ServeHTTP(unsetResponse, unsetRequest)
	if unsetResponse.Code != http.StatusOK {
		t.Fatalf("unset status = %d, body = %s", unsetResponse.Code, unsetResponse.Body.String())
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.ListenBrainzUsername != nil || user.ListenBrainzToken != nil {
		t.Fatalf("account was not unlinked: %+v", user)
	}
}

func TestListenBrainzAccountAPIRejectsInvalidToken(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	userID, _ := createTestUser(t, database)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "valid": false})
	}))
	defer server.Close()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	service := listenbrainz.NewService(database, server.URL, "piper/test (test@example.com)", nil, nil, nil)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/listenbrainz/set", bytes.NewBufferString(`{"token":"bad"}`))
	request = request.WithContext(withUserContext(request.Context(), userID))
	response := httptest.NewRecorder()
	apiLinkListenBrainzHandler(database, service).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.ListenBrainzUsername != nil || user.ListenBrainzToken != nil {
		t.Fatal("invalid token was stored")
	}
}

func TestCompatibleSubmissionIgnoresOutputOnlyMBIDMapping(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	userID, _ := createTestUser(t, database)

	listenedAt := time.Now().Add(-time.Minute).Unix()
	unverified := "client-recording"
	submission := models.ListenBrainzSubmission{
		ListenType: "single",
		Payload: []models.ListenBrainzPayload{{
			ListenedAt: &listenedAt,
			TrackMetadata: models.ListenBrainzTrackMetadata{
				ArtistName: "Artist",
				TrackName:  "Song",
				AdditionalInfo: &models.ListenBrainzAdditionalInfo{
					RecordingMBID: &unverified,
				},
				MBIDMapping: &models.ListenBrainzMBIDMapping{RecordingMBID: "spoofed-server-mapping"},
			},
		}},
	}
	body, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/1/submit-listens", bytes.NewReader(body))
	request = request.WithContext(withUserContext(request.Context(), userID))
	response := httptest.NewRecorder()
	apiSubmitListensHandler(database, nil, nil, nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	tracks, err := database.GetRecentTracks(userID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].RecordingMBID == nil || *tracks[0].RecordingMBID != unverified {
		t.Fatalf("output-only mapping was trusted: %+v", tracks)
	}
}

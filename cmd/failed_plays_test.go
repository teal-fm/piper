package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	"github.com/teal-fm/piper/pages"
	"github.com/teal-fm/piper/session"
)

func TestFailedPlaysPageAndRetryAuthorization(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
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
	other, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	save := func(userID int64, name string) int64 {
		t.Helper()
		id, err := database.SaveTrack(userID, db.SourceAppleMusic, &models.Track{Name: name, HasStamped: true, Timestamp: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	own := save(uid, "<script>alert(1)</script>")
	foreign := save(other, "Another user's private play")
	handler := failedPlays(database, pages.NewPages(), nil, "http://localhost")
	ctx := session.WithUserID(context.Background(), uid)
	get := httptest.NewRequestWithContext(ctx, http.MethodGet, "/failed-plays", nil)
	response := httptest.NewRecorder()
	handler(response, get)
	if response.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "Another user's private play") || strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("unsafe rendered page: %s", body)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing CSRF cookie")
	}
	csrf := cookies[0]
	for _, tc := range []struct {
		name   string
		ids    []int64
		token  string
		origin string
		code   int
	}{
		{"missing CSRF", []int64{own}, "", "", http.StatusForbidden},
		{"foreign play", []int64{foreign}, csrf.Value, "", http.StatusNotFound},
		{"mixed ownership", []int64{own, foreign}, csrf.Value, "", http.StatusNotFound},
		{"cross origin", []int64{own}, csrf.Value, "https://evil.example", http.StatusForbidden},
		{"retry own", []int64{own}, csrf.Value, "", http.StatusSeeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf": {tc.token}}
			for _, id := range tc.ids {
				form.Add("play_id", strconv.FormatInt(id, 10))
			}
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/failed-plays", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", tc.origin)
			request.AddCookie(csrf)
			result := httptest.NewRecorder()
			handler(result, request)
			if result.Code != tc.code {
				t.Fatalf("POST = %d: %s", result.Code, result.Body.String())
			}
		})
	}
	plays, err := database.ListUnpublishedPlays(uid, 20, 0)
	if err != nil || len(plays) != 1 || plays[0].Attempts != 1 || !strings.Contains(plays[0].LastError, "Sign in") {
		t.Fatalf("retry failure not visible: %+v %v", plays, err)
	}
	result := httptest.NewRecorder()
	handler(result, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/failed-plays", nil))
	if result.Code != http.StatusUnauthorized {
		t.Fatal("anonymous access allowed")
	}
}

func TestRetryCookieSecureBehindHTTPSProxy(t *testing.T) {
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
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
	for _, tc := range []struct {
		name, publicURL, requestURL string
		secure                      bool
	}{
		{"HTTPS proxy", "https://piper.example", "http://localhost/failed-plays", true},
		{"direct TLS", "http://localhost", "https://piper.example/failed-plays", true},
		{"local HTTP", "http://localhost", "http://localhost/failed-plays", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := failedPlays(database, pages.NewPages(), nil, tc.publicURL)
			request := httptest.NewRequestWithContext(session.WithUserID(t.Context(), uid), http.MethodGet, tc.requestURL, nil)
			// Forwarded headers are not trusted to determine the transport policy.
			request.Header.Set("X-Forwarded-Proto", "https")
			token := strings.Repeat("a", 64)
			request.AddCookie(&http.Cookie{Name: "piper_retry_csrf", Value: token})
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET: %d", response.Code)
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("expected existing cookie to be reissued")
			}
			cookie := cookies[0]
			if cookie.Secure != tc.secure || cookie.Value != token || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("unexpected cookie: %+v", cookie)
			}
		})
	}
}

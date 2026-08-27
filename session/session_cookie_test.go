package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionCookieSecurityAttributes(t *testing.T) {
	manager := &Manager{secureCookies: true}
	recorder := httptest.NewRecorder()
	manager.SetSessionCookie(recorder, &Session{ID: "session-id", ExpiresAt: time.Now().Add(time.Hour)})
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie attributes: %#v", cookie)
	}
}

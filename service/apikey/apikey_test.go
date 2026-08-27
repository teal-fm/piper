package apikey

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewKeyFlashIsScopedAndConsumedOnce(t *testing.T) {
	service := &Service{newKeys: make(map[string]string)}
	request := httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: "session-a"})

	if !service.storeNewKey(request, "secret-key") {
		t.Fatal("storeNewKey returned false")
	}
	if got := service.takeNewKey(request); got != "secret-key" {
		t.Fatalf("first take got %q, want secret-key", got)
	}
	if got := service.takeNewKey(request); got != "" {
		t.Fatalf("second take got %q, want empty", got)
	}
}

func TestNewKeyFlashRequiresSessionCookie(t *testing.T) {
	service := &Service{newKeys: make(map[string]string)}
	request := httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	if service.storeNewKey(request, "secret-key") {
		t.Fatal("storeNewKey succeeded without a session cookie")
	}
}

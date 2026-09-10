package atproto

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestInvalidLoginReturnsToForm(t *testing.T) {
	service := &AuthService{logger: log.New(io.Discard, "", 0)}
	for _, tc := range []struct{ handle, code string }{{"asdf./asdf", "invalid_handle"}, {"", "missing_handle"}} {
		t.Run(tc.code, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/login/atproto?"+url.Values{"handle": {tc.handle}}.Encode(), nil)
			response := httptest.NewRecorder()
			service.HandleLogin(response, req)
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d", response.Code)
			}
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if location.Path != "/" || location.Query().Get("login_error") != tc.code || location.Query().Get("handle") != tc.handle {
				t.Fatalf("unexpected redirect: %s", location)
			}
		})
	}
}

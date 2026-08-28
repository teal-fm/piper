package bsky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns a client whose requests all land on srv.
func newTestClient(srv *httptest.Server) *http.Client {
	client := srv.Client()
	client.Transport = rewriteHost{base: srv.URL, rt: client.Transport}
	return client
}

type rewriteHost struct {
	base string
	rt   http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := http.NewRequestWithContext(req.Context(), req.Method, r.base+req.URL.RequestURI(), req.Body)
	if err != nil {
		return nil, err
	}
	return r.rt.RoundTrip(target)
}

func TestFetchProfile(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantErr     bool
		wantHandle  string
		wantDisplay string
		wantAvatar  string
	}{
		{
			name:   "full profile",
			status: http.StatusOK,
			body: `{
				"did": "did:plc:abc123",
				"handle": "charles.harries.me",
				"displayName": "Charles",
				"avatar": "https://cdn.bsky.app/img/avatar/plain/did:plc:abc123/bafy@jpeg"
			}`,
			wantHandle:  "charles.harries.me",
			wantDisplay: "Charles",
			wantAvatar:  "https://cdn.bsky.app/img/avatar/plain/did:plc:abc123/bafy@jpeg",
		},
		{
			name:       "no display name or avatar",
			status:     http.StatusOK,
			body:       `{"did": "did:plc:abc123", "handle": "charles.harries.me"}`,
			wantHandle: "charles.harries.me",
		},
		{
			name:    "actor not found",
			status:  http.StatusNotFound,
			body:    `{"error": "InvalidRequest", "message": "Profile not found"}`,
			wantErr: true,
		},
		{
			name:    "appview error",
			status:  http.StatusInternalServerError,
			body:    `upstream is sad`,
			wantErr: true,
		},
		{
			name:    "malformed body",
			status:  http.StatusOK,
			body:    `{"handle": `,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("actor"); got != "did:plc:abc123" {
					t.Errorf("actor = %q, want did:plc:abc123", got)
				}
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			profile, err := FetchProfile(context.Background(), newTestClient(srv), "did:plc:abc123")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if profile.Handle != tt.wantHandle {
				t.Errorf("Handle = %q, want %q", profile.Handle, tt.wantHandle)
			}
			if profile.DisplayName != tt.wantDisplay {
				t.Errorf("DisplayName = %q, want %q", profile.DisplayName, tt.wantDisplay)
			}
			if profile.Avatar != tt.wantAvatar {
				t.Errorf("Avatar = %q, want %q", profile.Avatar, tt.wantAvatar)
			}
		})
	}
}

func TestFetchProfileEmptyActor(t *testing.T) {
	if _, err := FetchProfile(context.Background(), nil, ""); err == nil {
		t.Fatal("expected an error for an empty actor, got nil")
	}
}

// A hanging AppView must error rather than block the login redirect forever.
func TestFetchProfileTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := FetchProfile(ctx, newTestClient(srv), "did:plc:abc123"); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

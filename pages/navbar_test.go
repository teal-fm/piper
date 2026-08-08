package pages

import (
	"bytes"
	"strings"
	"testing"

	"github.com/teal-fm/piper/models"
)

func ptr(s string) *string { return &s }

func TestNewNavBar(t *testing.T) {
	t.Run("nil user", func(t *testing.T) {
		nav := NewNavBar(nil, false)
		if nav.IsLoggedIn || nav.Handle != "" || nav.AvatarURL != "" {
			t.Errorf("expected an empty nav bar, got %+v", nav)
		}
	})

	t.Run("user with a profile", func(t *testing.T) {
		nav := NewNavBar(&models.User{
			Handle:         ptr("charles.harries.me"),
			DisplayName:    ptr("Charles"),
			AvatarURL:      ptr("https://cdn.bsky.app/img/avatar/plain/did:plc:x/bafy@jpeg"),
			LastFMUsername: ptr("charles"),
		}, true)

		if !nav.IsLoggedIn {
			t.Error("IsLoggedIn = false, want true")
		}
		if nav.Handle != "charles.harries.me" {
			t.Errorf("Handle = %q", nav.Handle)
		}
		if nav.DisplayName != "Charles" {
			t.Errorf("DisplayName = %q", nav.DisplayName)
		}
		if nav.LastFMUsername != "charles" {
			t.Errorf("LastFMUsername = %q", nav.LastFMUsername)
		}
	})

	t.Run("user without a profile", func(t *testing.T) {
		nav := NewNavBar(&models.User{ATProtoDID: ptr("did:plc:x")}, true)
		if nav.Handle != "" || nav.DisplayName != "" || nav.AvatarURL != "" {
			t.Errorf("expected empty profile fields, got %+v", nav)
		}
	})
}

// The nav bar has to stay readable whether or not the AppView answered, so the
// chip degrades from avatar, to an initial, to nothing at all.
func TestNavBarChipRendering(t *testing.T) {
	tests := []struct {
		name   string
		nav    NavBar
		want   []string
		absent []string
	}{
		{
			name: "handle and avatar",
			nav: NavBar{
				IsLoggedIn:  true,
				Handle:      "charles.harries.me",
				DisplayName: "Charles",
				AvatarURL:   "https://cdn.bsky.app/img/avatar/plain/did:plc:x/bafy@jpeg",
			},
			want: []string{"@charles.harries.me", "cdn.bsky.app"},
		},
		{
			name:   "handle without an avatar falls back to an initial",
			nav:    NavBar{IsLoggedIn: true, Handle: "charles.harries.me"},
			want:   []string{"@charles.harries.me", ">c</span"},
			absent: []string{"<img"},
		},
		{
			name:   "no profile means no chip",
			nav:    NavBar{IsLoggedIn: true},
			absent: []string{"@", "<img"},
		},
		{
			name:   "logged out",
			nav:    NavBar{},
			want:   []string{"Login with ATProto"},
			absent: []string{"@", "<img"},
		},
	}

	pages := NewPages()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			params := struct{ NavBar NavBar }{NavBar: tt.nav}
			if err := pages.Execute("home", &buf, params); err != nil {
				t.Fatalf("failed to render home: %v", err)
			}

			out := buf.String()
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in rendered output", want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out, absent) {
					t.Errorf("unexpected %q in rendered output", absent)
				}
			}
		})
	}
}

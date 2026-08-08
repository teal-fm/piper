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

// The connect-your-accounts list marks linked services with a green bullet and
// unlinked ones with grey. The bullet itself has to survive: styling the list
// item as a flex container silently drops the marker.
func TestHomeServiceBullets(t *testing.T) {
	const (
		green = `marker:text-[#1DB954]`
		grey  = `marker:text-gray-400`
	)

	pages := NewPages()

	render := func(t *testing.T, nav NavBar) string {
		t.Helper()
		var buf bytes.Buffer
		params := struct{ NavBar NavBar }{NavBar: nav}
		if err := pages.Execute("home", &buf, params); err != nil {
			t.Fatalf("failed to render home: %v", err)
		}
		return buf.String()
	}

	t.Run("linked service is green", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:      true,
			LastFMEnabled:   true,
			LastFMConnected: true,
			LastFMUsername:  "charles",
		})
		if !strings.Contains(out, `<li class="`+green+`">`) {
			t.Error("expected a green bullet for the linked Last.fm account")
		}
	})

	t.Run("unlinked service is grey", func(t *testing.T) {
		out := render(t, NavBar{IsLoggedIn: true, LastFMEnabled: true})
		if !strings.Contains(out, `<li class="`+grey+`">`) {
			t.Error("expected a grey bullet for the unlinked Last.fm account")
		}
		if strings.Contains(out, green) {
			t.Error("did not expect a green bullet with nothing connected")
		}
	})

	t.Run("service disabled on the server is grey", func(t *testing.T) {
		out := render(t, NavBar{IsLoggedIn: true})
		if strings.Contains(out, green) {
			t.Error("did not expect a green bullet when every service is disabled")
		}
		if strings.Count(out, grey) != 3 {
			t.Errorf("expected 3 grey bullets, got %d", strings.Count(out, grey))
		}
	})

	// display:flex on the list item removes the disc marker entirely.
	t.Run("list items keep their marker", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:      true,
			LastFMEnabled:   true,
			LastFMConnected: true,
			LastFMUsername:  "charles",
			LastFMAvatarURL: "https://lastfm-img.freetls.fastly.net/i/u/174s/x.png",
		})
		if strings.Contains(out, `<li class="flex`) {
			t.Error("a flex list item has no bullet marker")
		}
		if !strings.Contains(out, "inline-flex items-center") {
			t.Error("expected the avatar row to be laid out inside the list item")
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

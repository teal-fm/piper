package pages

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func ptr(s string) *string { return &s }

// homeParams mirrors cmd.HomeParams, which this package can't import.
type homeParams struct {
	NavBar    NavBar
	BuildTime time.Time
	Agent     string
}

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

	// users.username only holds Spotify's display name once the callback has
	// run, so the ID is what says the name is really Spotify's.
	t.Run("user with a linked Spotify account", func(t *testing.T) {
		nav := NewNavBar(&models.User{
			Username:  ptr("Charles"),
			SpotifyID: ptr("spotify:user:x"),
		}, true)

		if nav.SpotifyUsername != "Charles" {
			t.Errorf("SpotifyUsername = %q, want %q", nav.SpotifyUsername, "Charles")
		}
		if !nav.SpotifyConnected {
			t.Error("SpotifyConnected = false, want true")
		}
	})

	t.Run("username without a Spotify ID is not Spotify's", func(t *testing.T) {
		nav := NewNavBar(&models.User{Username: ptr("Charles")}, true)

		if nav.SpotifyUsername != "" {
			t.Errorf("SpotifyUsername = %q, want empty", nav.SpotifyUsername)
		}
		if nav.SpotifyConnected {
			t.Error("SpotifyConnected = true, want false")
		}
	})

	// apiUnlinkLastfmHandler writes an empty username rather than NULL, so a
	// non-nil pointer isn't enough to say the account is still linked.
	t.Run("an empty Last.fm username is not linked", func(t *testing.T) {
		nav := NewNavBar(&models.User{LastFMUsername: ptr("")}, true)

		if nav.LastFMUsername != "" {
			t.Errorf("LastFMUsername = %q, want empty", nav.LastFMUsername)
		}
		if nav.LastFMConnected {
			t.Error("LastFMConnected = true, want false")
		}
	})

	t.Run("user without a profile", func(t *testing.T) {
		nav := NewNavBar(&models.User{ATProtoDID: ptr("did:plc:x")}, true)
		if nav.Handle != "" || nav.DisplayName != "" || nav.AvatarURL != "" {
			t.Errorf("expected empty profile fields, got %+v", nav)
		}
	})
}

func TestServices(t *testing.T) {
	nav := NavBar{
		IsLoggedIn:        true,
		SpotifyEnabled:    true,
		SpotifyConnected:  true,
		SpotifyUsername:   "charles",
		LastFMEnabled:     true,
		LastFMUsername:    "charles-lfm",
		LastFMConnected:   true,
		LastFMAvatarURL:   "https://lastfm-img.freetls.fastly.net/i/u/174s/x.png",
		AppleMusicEnabled: false,
	}

	services := nav.Services()
	if len(services) != 3 {
		t.Fatalf("Services() returned %d cards, want 3", len(services))
	}

	spotify, lastfm, applemusic := services[0], services[1], services[2]

	if spotify.Name != "Spotify" || spotify.Account != "charles" || spotify.UnlinkURL != "/unlink-spotify" {
		t.Errorf("Spotify card = %+v", spotify)
	}
	if lastfm.Name != "Last.fm" || lastfm.Account != "charles-lfm" || lastfm.AvatarURL == "" {
		t.Errorf("Last.fm card = %+v", lastfm)
	}
	// Every service unlinks from its own card, so each needs a POST target.
	for _, svc := range services {
		if svc.UnlinkURL == "" {
			t.Errorf("%s card has no unlink URL", svc.Name)
		}
		if svc.Icon == "" {
			t.Errorf("%s card has no icon slug", svc.Name)
		}
	}
	// MusicKit hands us a user token and nothing to call the account by.
	if applemusic.Name != "Apple Music" || applemusic.Account != "" {
		t.Errorf("Apple Music card = %+v", applemusic)
	}
	if applemusic.Enabled {
		t.Error("Apple Music Enabled = true, want false")
	}
}

// Each service tile has three looks: greyed out when unavailable, plain grey
// when available but unlinked, and accent teal once linked.
func TestHomeServiceCards(t *testing.T) {
	const (
		connected = `border-accent bg-accent/10`
		disabled  = `opacity-50`
	)

	pages := NewPages()

	render := func(t *testing.T, nav NavBar) string {
		t.Helper()
		var buf bytes.Buffer
		params := homeParams{NavBar: nav}
		if err := pages.Execute("home", &buf, params); err != nil {
			t.Fatalf("failed to render home: %v", err)
		}
		return buf.String()
	}

	// An unknown icon slug silently renders nothing, so check the paths land.
	t.Run("every card renders its brand icon", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn: true, SpotifyEnabled: true, LastFMEnabled: true, AppleMusicEnabled: true,
		})
		if got := strings.Count(out, "<svg"); got != 3 {
			t.Errorf("rendered %d icons, want 3", got)
		}
		if strings.Count(out, `viewBox="0 0 24 24"`) != 3 {
			t.Error("expected every icon to keep its viewBox")
		}
		// Decorative: the service name sits right beside the mark.
		if strings.Count(out, `aria-hidden="true"`) < 3 {
			t.Error("expected the brand marks to be hidden from assistive tech")
		}
	})

	t.Run("linked service is accent teal", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:      true,
			LastFMEnabled:   true,
			LastFMConnected: true,
			LastFMUsername:  "charles",
		})
		if !strings.Contains(out, connected) {
			t.Error("expected an accent card for the linked Last.fm account")
		}
		if !strings.Contains(out, "charles") {
			t.Error("expected the Last.fm username on the card")
		}
	})

	t.Run("linked Spotify account shows its username and an unlink form", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:       true,
			SpotifyEnabled:   true,
			SpotifyConnected: true,
			SpotifyUsername:  "charles",
		})
		if !strings.Contains(out, connected) {
			t.Error("expected an accent card for the linked Spotify account")
		}
		if !strings.Contains(out, "charles") {
			t.Error("expected the Spotify username")
		}
		if strings.Contains(out, "Not connected") {
			t.Error("did not expect the unlinked state for a linked account")
		}
		// Unlinking is destructive, so it needs a POST form, not a link.
		if !strings.Contains(out, `method="post" action="/unlink-spotify"`) {
			t.Error("expected the unlink form to POST to /unlink-spotify")
		}
	})

	// Once linked, the only control is Unlink — misclicking must not re-run OAuth.
	t.Run("linked card is inert apart from unlinking", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:       true,
			SpotifyEnabled:   true,
			SpotifyConnected: true,
			SpotifyUsername:  "charles",
		})
		if strings.Contains(out, `href="/login/spotify"`) {
			t.Error("a linked card must not link back into the connect flow")
		}
		if strings.Contains(out, "absolute inset-0") {
			t.Error("a linked card must not carry a stretched link")
		}
		if !strings.Contains(out, `method="post" action="/unlink-spotify"`) {
			t.Error("expected the unlink form")
		}
	})

	t.Run("every linked service offers an unlink", func(t *testing.T) {
		out := render(t, NavBar{
			IsLoggedIn:          true,
			SpotifyEnabled:      true,
			SpotifyConnected:    true,
			LastFMEnabled:       true,
			LastFMConnected:     true,
			LastFMUsername:      "charles",
			AppleMusicEnabled:   true,
			AppleMusicConnected: true,
		})
		for _, route := range []string{"/unlink-spotify", "/unlink-lastfm", "/unlink-applemusic"} {
			if !strings.Contains(out, `method="post" action="`+route+`"`) {
				t.Errorf("missing unlink form for %s", route)
			}
		}
		if strings.Contains(out, "absolute inset-0") {
			t.Error("no linked card should be clickable")
		}
	})

	t.Run("unlinked service links to its connect flow", func(t *testing.T) {
		out := render(t, NavBar{IsLoggedIn: true, SpotifyEnabled: true})
		if !strings.Contains(out, `href="/login/spotify"`) {
			t.Error("expected the card to link to the Spotify connect flow")
		}
		if !strings.Contains(out, "Not connected") {
			t.Error("expected the unlinked state")
		}
		if strings.Contains(out, "/unlink-spotify") {
			t.Error("did not expect an unlink form with nothing connected")
		}
		if strings.Contains(out, connected) {
			t.Error("did not expect an accent card with nothing connected")
		}
	})

	// A service the server can't offer must not hand out a link that 404s or 503s.
	t.Run("service disabled on the server is greyed out and inert", func(t *testing.T) {
		out := render(t, NavBar{IsLoggedIn: true})
		if strings.Count(out, disabled) != 3 {
			t.Errorf("expected 3 greyed-out cards, got %d", strings.Count(out, disabled))
		}
		if strings.Contains(out, connected) {
			t.Error("did not expect an accent card when every service is disabled")
		}
		for _, route := range []string{"/login/spotify", "/link-lastfm", "/link-applemusic"} {
			if strings.Contains(out, route) {
				t.Errorf("did not expect a link to %s for a disabled service", route)
			}
		}
		if strings.Count(out, "Unavailable on this server") != 3 {
			t.Error("expected every disabled card to say so")
		}
	})
}

// The chip degrades from avatar, to an initial, to nothing, so the header stays
// readable whether or not the AppView answered.
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
			absent: []string{`title="@`, "<img"},
		},
		{
			// Logged out, the login form is the only call to action.
			name:   "logged out",
			nav:    NavBar{},
			want:   []string{"Log in", `name="handle"`},
			absent: []string{`title="@`, "<img", "/logout", "/api-keys"},
		},
	}

	pages := NewPages()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			params := homeParams{NavBar: tt.nav}
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

func TestHomeBuildTime(t *testing.T) {
	pages := NewPages()

	render := func(t *testing.T, params homeParams) string {
		t.Helper()
		var buf bytes.Buffer
		if err := pages.Execute("home", &buf, params); err != nil {
			t.Fatalf("failed to render home: %v", err)
		}
		return buf.String()
	}

	t.Run("shows the build time", func(t *testing.T) {
		built := time.Date(2026, time.August, 10, 21, 25, 0, 0, time.UTC)
		out := render(t, homeParams{BuildTime: built})
		if !strings.Contains(out, "Built Aug 10, 2026 21:25 UTC") {
			t.Error("expected the formatted build time in the footer")
		}
	})

	// The agent is piper's own identity string, not the visitor's browser.
	t.Run("shows the agent beside the build time", func(t *testing.T) {
		built := time.Date(2026, time.August, 10, 21, 25, 0, 0, time.UTC)
		out := render(t, homeParams{BuildTime: built, Agent: "piper/v0.0.8"})
		if !strings.Contains(out, "Built Aug 10, 2026 21:25 UTC &middot; piper/v0.0.8") {
			t.Error("expected the agent next to the build time")
		}
	})

	// Nothing sets an empty agent in practice, but a bare separator would look
	// like a rendering bug.
	t.Run("no dangling separator without an agent", func(t *testing.T) {
		out := render(t, homeParams{Agent: ""})
		if strings.Contains(out, "&middot;") {
			t.Error("did not expect a separator with no agent")
		}
	})

	// "unknown" beats the "N/A UTC" that formatTime would produce on its own.
	t.Run("unknown build time", func(t *testing.T) {
		out := render(t, homeParams{})
		if !strings.Contains(out, "Built unknown") {
			t.Error("expected the footer to degrade to 'unknown'")
		}
		if strings.Contains(out, "UTC") {
			t.Error("did not expect a timezone with no build time")
		}
	})
}

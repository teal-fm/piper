package pages

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every page is parsed into one namespace, so an empty block definition
// silently inherits whatever another page defined for it. Check nothing bleeds across.
func TestPagesRenderIndependently(t *testing.T) {
	type apiKeysParams struct {
		Keys     []struct{}
		NewKeyID string
		NavBar   NavBar
	}
	type lastFMParams struct {
		NavBar          NavBar
		CurrentUsername string
	}
	type appleMusicParams struct {
		NavBar   NavBar
		DevToken string
	}

	nav := NavBar{IsLoggedIn: true, Handle: "charles.harries.me"}
	// Handlers attach the breadcrumb; here we supply it the same way they do.
	crumb := func(name string) NavBar { return nav.WithBreadcrumb(name) }

	tests := []struct {
		name       string
		params     any
		title      string
		breadcrumb string // empty on the home page, which is the brand itself
		absent     []string
	}{
		{
			name:       "home",
			breadcrumb: "",
			params:     homeParams{NavBar: nav},
			title:      "piper",
			absent:     []string{"musickit.js", "API keys allow", "Last.fm username"},
		},
		{
			name:       "apiKeys",
			breadcrumb: "API keys",
			params:     apiKeysParams{NavBar: crumb("API keys")},
			title:      "API keys · piper",
			absent:     []string{"musickit.js", "Your services"},
		},
		{
			name:       "lastFMForm",
			breadcrumb: "Last.fm",
			params:     lastFMParams{NavBar: crumb("Last.fm"), CurrentUsername: "charles"},
			title:      "Link Last.fm · piper",
			absent:     []string{"musickit.js", "Your services"},
		},
		{
			name:       "applemusic_link",
			breadcrumb: "Apple Music",
			params:     appleMusicParams{NavBar: crumb("Apple Music"), DevToken: "dev-token"},
			title:      "Link Apple Music · piper",
			absent:     []string{"Your services", "API keys allow"},
		},
	}

	pages := NewPages()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			if err := pages.Execute(tt.name, &buf, tt.params); err != nil {
				t.Fatalf("failed to render %s: %v", tt.name, err)
			}
			out := buf.String()

			if !strings.HasPrefix(strings.TrimSpace(out), "<!DOCTYPE html>") {
				t.Error("expected a doctype")
			}
			if want := "<title>" + tt.title + "</title>"; !strings.Contains(out, want) {
				t.Errorf("missing %q", want)
			}
			// Every page shares the header, the font, and the theme.
			for _, want := range []string{"DM+Sans", "/static/main.css", "dark:from-[#111827]", ">piper<"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in rendered output", want)
				}
			}
			// The header reads "piper › <page>" everywhere but home.
			if tt.breadcrumb == "" {
				if strings.Contains(out, `aria-current="page"`) {
					t.Error("home is the brand itself and should carry no breadcrumb")
				}
			} else if !strings.Contains(out, `aria-current="page"
      >`+tt.breadcrumb+`<`) {
				t.Errorf("missing breadcrumb %q", tt.breadcrumb)
			}
			for _, absent := range tt.absent {
				if strings.Contains(out, absent) {
					t.Errorf("%q bled in from another page template", absent)
				}
			}
		})
	}

	// The key's ID *is* the secret, so the page has to actually print it.
	t.Run("a new API key is shown in full", func(t *testing.T) {
		const key = "n0tAr3alK3y_bUt-l00ks-l1ke-0ne="
		var buf strings.Builder
		err := pages.Execute("apiKeys", &buf, apiKeysParams{
			NavBar: crumb("API keys"), NewKeyID: key,
		})
		if err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, ">"+key+"</pre>") {
			t.Error("expected the key itself inside a <pre>")
		}
		if !strings.Contains(out, "font-mono") {
			t.Error("expected the key set in the mono face")
		}
		// A 44-char base64 secret must not blow out the layout.
		if !strings.Contains(out, "break-all") {
			t.Error("expected the key to wrap rather than overflow")
		}
	})

	t.Run("no callout without a new key", func(t *testing.T) {
		var buf strings.Builder
		if err := pages.Execute("apiKeys", &buf, apiKeysParams{NavBar: crumb("API keys")}); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		if strings.Contains(buf.String(), "Your new API key") {
			t.Error("did not expect the new-key callout on a plain visit")
		}
	})

	// MusicKit is a third-party script; it belongs only on the page that uses it.
	t.Run("applemusic loads MusicKit", func(t *testing.T) {
		var buf strings.Builder
		if err := pages.Execute("applemusic_link", &buf, appleMusicParams{NavBar: crumb("Apple Music"), DevToken: "dev-token"}); err != nil {
			t.Fatalf("failed to render: %v", err)
		}
		if !strings.Contains(buf.String(), "musickit.js") {
			t.Error("expected the MusicKit loader on the Apple Music page")
		}
	})
}

// Cache lets a CDN hold /static/main.css for a day, so the fingerprint has to
// track the file's actual content.
func TestStylesheetIsFingerprinted(t *testing.T) {
	css, err := Files.ReadFile("static/main.css")
	if err != nil {
		t.Fatalf("reading the embedded stylesheet: %v", err)
	}
	sum := sha256.Sum256(css)
	want := "/static/main.css?v=" + hex.EncodeToString(sum[:])[:8]

	var sb strings.Builder
	if err := NewPages().Execute("home", &sb, homeParams{NavBar: NavBar{}}); err != nil {
		t.Fatalf("rendering home: %v", err)
	}
	if !strings.Contains(sb.String(), want) {
		t.Errorf("stylesheet link is not fingerprinted, want %q", want)
	}
}

// The fingerprint is only worth anything if the URL carrying it still serves.
func TestFingerprintedStylesheetServes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, staticURL("main.css"), nil)
	rr := httptest.NewRecorder()
	NewPages().Static().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "tailwindcss") {
		t.Error("expected the stylesheet body")
	}
}

// An asset with no embedded copy still has to render a usable URL.
func TestStaticURLWithoutAnEmbeddedFile(t *testing.T) {
	if got, want := staticURL("nothing.css"), "/static/nothing.css"; got != want {
		t.Errorf("staticURL() = %q, want %q", got, want)
	}
}

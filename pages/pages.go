package pages

// Helpers to load gohtml templates and render them
// forked and inspired from tangled's implementation
//https://tangled.org/@tangled.org/core/blob/master/appview/pages/pages.go

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/teal-fm/piper/models"
)

//go:embed templates/* static/*
var Files embed.FS

// staticVersions fingerprints each embedded static file by its content.
var staticVersions = hashStatic()

func hashStatic() map[string]string {
	versions := map[string]string{}
	entries, err := fs.ReadDir(Files, "static")
	if err != nil {
		return versions
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := fs.ReadFile(Files, "static/"+entry.Name())
		if err != nil {
			continue
		}
		sum := sha256.Sum256(content)
		versions[entry.Name()] = hex.EncodeToString(sum[:])[:8]
	}
	return versions
}

// staticURL addresses an embedded asset by its content, so changing the file changes the URL.
func staticURL(name string) string {
	if version, ok := staticVersions[name]; ok {
		return "/static/" + name + "?v=" + version
	}
	return "/static/" + name
}

type Pages struct {
	cache       *TmplCache[string, *template.Template]
	templateDir string // Path to templates on disk for dev mode
	embedFS     fs.FS
}

func NewPages() *Pages {
	return &Pages{
		cache:   NewTmplCache[string, *template.Template](),
		embedFS: Files,
	}
}

func (p *Pages) fragmentPaths() ([]string, error) {
	var fragmentPaths []string
	err := fs.WalkDir(p.embedFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".gohtml") {
			return nil
		}
		fragmentPaths = append(fragmentPaths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return fragmentPaths, nil
}

func (p *Pages) pathToName(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "templates/"), ".gohtml")
}

// reverse of pathToName
func (p *Pages) nameToPath(s string) string {
	return "templates/" + s + ".gohtml"
}

// parse without memoization
func (p *Pages) rawParse(stack ...string) (*template.Template, error) {
	paths, err := p.fragmentPaths()
	if err != nil {
		return nil, err
	}
	for _, s := range stack {
		paths = append(paths, p.nameToPath(s))
	}

	funcs := p.funcMap()
	top := stack[len(stack)-1]
	parsed, err := template.New(top).
		Funcs(funcs).
		ParseFS(p.embedFS, paths...)
	if err != nil {
		return nil, err
	}

	return parsed, nil
}

func (p *Pages) parse(stack ...string) (*template.Template, error) {
	key := strings.Join(stack, "|")

	if cached, exists := p.cache.Get(key); exists {
		return cached, nil
	}

	result, err := p.rawParse(stack...)
	if err != nil {
		return nil, err
	}

	p.cache.Set(key, result)
	return result, nil
}

func (p *Pages) funcMap() template.FuncMap {
	return template.FuncMap{
		"static": staticURL,
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "N/A"
			}
			return t.Format("Jan 02, 2006 15:04")
		},
	}
}

func (p *Pages) parseBase(top string) (*template.Template, error) {
	stack := []string{
		"layouts/base",
		top,
	}
	return p.parse(stack...)
}

func (p *Pages) Static() http.Handler {

	sub, err := fs.Sub(Files, "static")
	if err != nil {
		panic(err)
	}

	return Cache(http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
}

func Cache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Split(r.URL.Path, "?")[0]
		// We may want to change these, just took what tangled has and allows browser side caching
		if strings.HasSuffix(path, ".css") {
			// on day for css files
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		h.ServeHTTP(w, r)
	})
}

// Execute What loads and renders the HTML page/
func (p *Pages) Execute(name string, w io.Writer, params any) error {
	tpl, err := p.parseBase(name)
	if err != nil {
		return err
	}

	return tpl.ExecuteTemplate(w, "layouts/base", params)
}

// Shared view/template params

type NavBar struct {
	IsLoggedIn        bool
	Breadcrumb        string
	Handle            string
	DisplayName       string
	AvatarURL         string
	SpotifyUsername   string
	LastFMUsername    string
	LastFMAvatarURL   string
	SpotifyEnabled    bool
	LastFMEnabled     bool
	AppleMusicEnabled bool
	// *Connected report whether this user has linked the service, as opposed to
	// *Enabled, which reports whether the server offers it at all.
	SpotifyConnected    bool
	LastFMConnected     bool
	AppleMusicConnected bool
}

// NewNavBar builds the shared nav params from the current user, which may be
// nil when logged out or when the lookup failed.
func NewNavBar(user *models.User, isLoggedIn bool) NavBar {
	nav := NavBar{
		IsLoggedIn:        isLoggedIn,
		SpotifyEnabled:    viper.GetBool("enable_spotify"),
		LastFMEnabled:     viper.GetBool("enable_lastfm"),
		AppleMusicEnabled: viper.GetBool("enable_applemusic"),
	}

	if user == nil {
		return nav
	}

	if user.Handle != nil {
		nav.Handle = *user.Handle
	}
	if user.DisplayName != nil {
		nav.DisplayName = *user.DisplayName
	}
	if user.AvatarURL != nil {
		nav.AvatarURL = *user.AvatarURL
	}
	if user.SpotifyID != nil && user.Username != nil {
		nav.SpotifyUsername = *user.Username
	}
	if user.LastFMUsername != nil && *user.LastFMUsername != "" {
		nav.LastFMUsername = *user.LastFMUsername
	}
	if user.LastFMAvatarURL != nil {
		nav.LastFMAvatarURL = *user.LastFMAvatarURL
	}

	nav.SpotifyConnected = user.SpotifyID != nil
	nav.LastFMConnected = nav.LastFMUsername != ""
	nav.AppleMusicConnected = user.AppleMusicUserToken != nil

	return nav
}

// WithBreadcrumb adds a breadcrumb if we're on a subpage
func (n NavBar) WithBreadcrumb(name string) NavBar {
	n.Breadcrumb = name
	return n
}

// ServiceCard describes one music service tile on the home page.
type ServiceCard struct {
	Name      string
	Icon      string
	Enabled   bool
	Connected bool
	Account   string
	AvatarURL string
	LinkURL   string
	UnlinkURL string
}

// Services are the available music services that we can scrobble
// from; Apple Music doesn't actually give us a username.
func (n NavBar) Services() []ServiceCard {
	return []ServiceCard{
		{
			Name:      "Spotify",
			Icon:      "spotify",
			Enabled:   n.SpotifyEnabled,
			Connected: n.SpotifyConnected,
			Account:   n.SpotifyUsername,
			LinkURL:   "/login/spotify",
			UnlinkURL: "/unlink-spotify",
		},
		{
			Name:      "Last.fm",
			Icon:      "lastfm",
			Enabled:   n.LastFMEnabled,
			Connected: n.LastFMConnected,
			Account:   n.LastFMUsername,
			AvatarURL: n.LastFMAvatarURL,
			LinkURL:   "/link-lastfm",
			UnlinkURL: "/unlink-lastfm",
		},
		{
			Name:      "Apple Music",
			Icon:      "applemusic",
			Enabled:   n.AppleMusicEnabled,
			Connected: n.AppleMusicConnected,
			LinkURL:   "/link-applemusic",
			UnlinkURL: "/unlink-applemusic",
		},
	}
}

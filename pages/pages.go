package pages

// Helpers to load gohtml templates and render them
// forked and inspired from tangled's implementation
//https://tangled.org/@tangled.org/core/blob/master/appview/pages/pages.go

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/* static/*
var Files embed.FS

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
	//if p.dev {
	//	return http.StripPrefix("/static/", http.FileServer(http.Dir("appview/pages/static")))
	//}

	sub, err := fs.Sub(Files, "static")
	if err != nil {
		//p.logger.Error("no static dir found? that's crazy", "err", err)
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	// Custom handler to apply Cache-Control headers for font files
	//return http.FileServer(http.FS(sub))
	//return http.FileServer(http.FS(sub))
}

func Cache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Split(r.URL.Path, "?")[0]

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
	IsLoggedIn     bool
	LastFMUsername string
}

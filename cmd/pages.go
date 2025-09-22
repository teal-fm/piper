package main

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*
var Files embed.FS

// inspired from tangled's implementation
//https://tangled.org/@tangled.org/core/blob/master/appview/pages/pages.go

type Pages struct {
	cache       *TmplCache[string, *template.Template]
	dev         bool
	templateDir string // Path to templates on disk for dev mode
	embedFS     fs.FS
}

func NewPages(dev bool) *Pages {
	pages := &Pages{
		cache:       NewTmplCache[string, *template.Template](),
		dev:         dev,
		templateDir: "templates",
	}
	if pages.dev {
		pages.embedFS = os.DirFS(pages.templateDir)
	} else {
		//pages.embedFS = Files
	}

	return pages
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
		if !strings.Contains(path, "fragments/") {
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
	return strings.TrimSuffix(strings.TrimPrefix(s, "templates/"), ".html")
}

// reverse of pathToName
func (p *Pages) nameToPath(s string) string {
	return "templates/" + s + ".html"
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

	// never cache in dev mode
	if cached, exists := p.cache.Get(key); !p.dev && exists {
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

func (p *Pages) executePlain(name string, w io.Writer, params any) error {
	tpl, err := p.parse(name)
	if err != nil {
		return err
	}

	return tpl.Execute(w, params)
}

func (p *Pages) execute(name string, w io.Writer, params any) error {
	tpl, err := p.parseBase(name)
	if err != nil {
		return err
	}

	return tpl.ExecuteTemplate(w, "layouts/base", params)
}

/// Cache for pages

type TmplCache[K comparable, V any] struct {
	data  map[K]V
	mutex sync.RWMutex
}

func NewTmplCache[K comparable, V any]() *TmplCache[K, V] {
	return &TmplCache[K, V]{
		data: make(map[K]V),
	}
}

func (c *TmplCache[K, V]) Get(key K) (V, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	val, exists := c.data[key]
	return val, exists
}

func (c *TmplCache[K, V]) Set(key K, value V) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.data[key] = value
}

func (c *TmplCache[K, V]) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.data)
}

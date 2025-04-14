package main

import (
	"html/template"
  "io/fs"
	"path/filepath"
	"time"

  "github.com/teal-fm/piper/ui"
)

type templateData struct {
	CurrentYear     int
	Flash           string
  IsAuthenticated bool
}

func humanDate(t time.Time) string {
	return t.Format("02 Jan 2006 at 15:04")
}

var functions = template.FuncMap{
	"humanDate": humanDate,
}

func newTemplateCache() (map[string]*template.Template, error) {
	cache := map[string]*template.Template{}

	pages, err := fs.Glob(ui.Files, "html/pages/*.tmpl")
	if err != nil {
		return nil, err
	}

	for _, page := range pages {
		name := filepath.Base(page)

    patterns := []string{
      "html/base.tmpl",
      "html/partials/*.tmpl",
      page,
    }

		ts, err := template.New(name).Funcs(functions).ParseFS(ui.Files, patterns)
		if err != nil {
			return nil, err
		}

		cache[name] = ts
	}

	return cache, nil
}

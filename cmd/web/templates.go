package main

import (
  "html/template"
  "path/filepath"
  "time"
)

type templateData struct {
  CurrentYear int
  Flash       string
}

func humanDate(t time.Time) string {
  return t.Format("02 Jan 2006 at 15:04")
}

var functions = template.FuncMap{
  "humanDate":  humanDate,
}

func newTemplateCache() (map[string]*template.Template, error) {
  cache := map[string]*template.Template{}

  pages, err := filepath.Glob("./ui/html/pages/*.tmpl")
  if err != nil {
    return nil, err
  }

  for _, page := range pages {
    name := filepath.Base(page)

    ts, err := template.New(name).Funcs(functions).ParseFiles("./ui/html/base.tmpl")
    if err != nil {
      return nil, err
    }

    ts, err = ts.ParseGlob("./ui/html/partials/*.tmpl")
    if err != nil {
      return nil, err
    }

    ts, err = ts.ParseFiles(page)
    if err != nil {
      return nil, err
    }

    cache[name] = ts
  }

  return cache, nil
}

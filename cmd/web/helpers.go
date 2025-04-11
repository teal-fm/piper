package main

import (
  "bytes"
  "fmt"
	"net/http"
	"runtime/debug"
  "time"
)

// The serverError helper writes a log entry at Error level (including the request
// method and URI as attributes), then sends a generic 500 Internal Server Error
// response to the user.
func (app *application) serverError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		method = r.Method
		uri    = r.URL.RequestURI()
		trace  = string(debug.Stack())
	)

	app.logger.Error(err.Error(), "method", method, "uri", uri, "trace", trace)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// the clientError helper sends a specific status code and corresponding description
// to the user. we'll use this later in the book to send responses like 400 "Bad Request"
// when there's a problem with the requesst that the user sent.
func (app *application) clientError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func (app *application) render(
  w http.ResponseWriter, 
  r *http.Request, 
  status int, 
  page string, 
  data templateData,
) {
  ts, ok := app.templateCache[page]
  if !ok {
    err := fmt.Errorf("the template %s does not exist", page)
    app.serverError(w, r, err)
    return
  }

  buf := new(bytes.Buffer)

  err := ts.ExecuteTemplate(buf, "base", data)
  if err != nil {
    app.serverError(w, r, err)
    return
  }

  w.WriteHeader(status)

  buf.WriteTo(w)
}

func (app *application) newTemplateData(r *http.Request) templateData {
  return templateData{
    CurrentYear:  time.Now().Year(),
    Flash:        app.sessionManager.PopString(r.Context(), "flash"),
  }
}

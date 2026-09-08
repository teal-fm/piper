package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/teal-fm/piper/db"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/pages"
	atprotoservice "github.com/teal-fm/piper/service/atproto"
	"github.com/teal-fm/piper/session"
)

type failedPlaysParams struct {
	NavBar      pages.NavBar
	Plays       []db.PlaySubmission
	CSRF        string
	Notice      string
	Previous    int
	Next        int
	HasPrevious bool
	HasNext     bool
}

func failedPlays(database *db.DB, pg *pages.Pages, auth *atprotoauth.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := session.GetUserID(r.Context())
		if !ok {
			http.Error(w, "Sign in to view failed plays.", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid retry request", http.StatusBadRequest)
				return
			}
			cookie, err := r.Cookie("piper_retry_csrf")
			if err != nil || len(cookie.Value) != 64 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.PostForm.Get("csrf"))) != 1 {
				http.Error(w, "Refresh this page before retrying.", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					http.Error(w, "Invalid request origin", http.StatusForbidden)
					return
				}
			}
			ids := r.PostForm["play_id"]
			if len(ids) == 0 || len(ids) > 20 {
				http.Error(w, "Choose between 1 and 20 plays.", http.StatusBadRequest)
				return
			}
			// Validate the complete request before publishing anything.
			parsedIDs := make([]int64, 0, len(ids))
			seen := map[int64]bool{}
			for _, raw := range ids {
				id, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || id <= 0 {
					http.Error(w, "Invalid play ID", http.StatusBadRequest)
					return
				}
				if _, err := database.GetTrackForUser(userID, id); err != nil {
					http.Error(w, "Play not found", http.StatusNotFound)
					return
				}
				if !seen[id] {
					parsedIDs = append(parsedIDs, id)
					seen[id] = true
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			published, failed, skipped := 0, 0, 0
			for _, id := range parsedIDs {
				err := atprotoservice.PublishStoredPlay(ctx, database, userID, id, auth)
				if errors.Is(err, db.ErrSubmissionUnavailable) {
					skipped++
					continue
				}
				if err != nil {
					failed++
					break
				}
				published++
			}
			notice := fmt.Sprintf("Published %d. Failed %d. Unavailable %d. Not attempted %d.", published, failed, skipped, len(parsedIDs)-published-failed-skipped)
			http.Redirect(w, r, "/failed-plays?notice="+url.QueryEscape(notice), http.StatusSeeOther)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset < 0 || offset > 1000000 {
			offset = 0
		}
		plays, err := database.ListUnpublishedPlays(userID, 21, offset)
		if err != nil {
			log.Printf("failed_plays user_id=%d error=%q", userID, err)
			http.Error(w, "Could not load failed plays. Try again.", http.StatusInternalServerError)
			return
		}
		if offset > 0 && len(plays) == 0 {
			http.Redirect(w, r, "/failed-plays", http.StatusSeeOther)
			return
		}
		user, err := database.GetUserByID(userID)
		if err != nil {
			http.Error(w, "Could not load account", http.StatusInternalServerError)
			return
		}
		token := ""
		if cookie, err := r.Cookie("piper_retry_csrf"); err == nil && len(cookie.Value) == 64 {
			token = cookie.Value
		}
		if token == "" {
			var b [32]byte
			if _, err := rand.Read(b[:]); err != nil {
				http.Error(w, "Could not prepare retry form", http.StatusInternalServerError)
				return
			}
			token = hex.EncodeToString(b[:])
			http.SetCookie(w, &http.Cookie{Name: "piper_retry_csrf", Value: token, Path: "/failed-plays", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
		}
		params := failedPlaysParams{NavBar: pages.NewNavBar(user, true).WithBreadcrumb("Failed plays"), Plays: plays, CSRF: token, Notice: r.URL.Query().Get("notice"), HasPrevious: offset > 0, Previous: max(0, offset-20), HasNext: len(plays) > 20, Next: offset + 20}
		if len(params.Plays) > 20 {
			params.Plays = params.Plays[:20]
		}
		var body bytes.Buffer
		if err := pg.Execute("failedPlays", &body, params); err != nil {
			log.Printf("failed plays template: %v", err)
			http.Error(w, "Could not display failed plays", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body.Bytes())
	}
}

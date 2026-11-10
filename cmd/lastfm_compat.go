package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/db/apikey"
	"github.com/teal-fm/piper/models"
	atprotoauth "github.com/teal-fm/piper/oauth/atproto"
	"github.com/teal-fm/piper/service/musicbrainz"
	"github.com/teal-fm/piper/service/playingnow"
	"github.com/teal-fm/piper/session"
)

// Last.fm API error codes. See
// https://www.last.fm/api/errorcodes for the full list.
const (
	lastfmErrInvalidMethod  = 3
	lastfmErrInvalidParams  = 6
	lastfmErrOperationFail  = 8
	lastfmErrInvalidSession = 9
	lastfmErrInvalidAPIKey  = 10
)

var lastfmIndexedParamPattern = regexp.MustCompile(`^(artist|track|album|albumArtist|mbid|duration|timestamp)\[(\d+)\]$`)

// lastfmMaxFutureSkew tolerates small clock differences while keeping a bad
// future timestamp from becoming the Last.fm watermark for later polling.
const lastfmMaxFutureSkew = time.Hour

type lastfmCompatHandler struct {
	database          *db.DB
	sessionManager    *session.Manager
	atprotoService    *atprotoauth.AuthService
	mbService         *musicbrainz.Service
	playingNowService *playingnow.Service
}

// apiLastfmCompatHandler implements the subset of the Last.fm API that
// scrobbling clients need. Piper API keys stand in for Last.fm session keys,
// so there is no separate application secret or out-of-band auth flow.
func apiLastfmCompatHandler(database *db.DB, sessionManager *session.Manager, atprotoService *atprotoauth.AuthService, mbService *musicbrainz.Service, playingNowService *playingnow.Service) http.HandlerFunc {
	handler := &lastfmCompatHandler{
		database:          database,
		sessionManager:    sessionManager,
		atprotoService:    atprotoService,
		mbService:         mbService,
		playingNowService: playingNowService,
	}
	return handler.handle
}

func (h *lastfmCompatHandler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		h.writeError(w, r, http.StatusMethodNotAllowed, lastfmErrInvalidMethod, "Invalid method - No method with that name in this package")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.writeError(w, r, http.StatusBadRequest, lastfmErrInvalidParams, "Invalid parameters - Your request is missing a required parameter")
		return
	}

	switch r.FormValue("method") {
	case "auth.getToken":
		h.getToken(w, r)
	case "auth.getSession":
		h.getSession(w, r)
	case "track.scrobble":
		h.scrobble(w, r)
	case "track.updateNowPlaying":
		h.updateNowPlaying(w, r)
	case "":
		h.writeError(w, r, http.StatusBadRequest, lastfmErrInvalidParams, "Invalid parameters - Your request is missing a required parameter")
	default:
		h.writeError(w, r, http.StatusBadRequest, lastfmErrInvalidMethod, "Invalid method - No method with that name in this package")
	}
}

// lookupKey resolves a piper API key from the named form/query parameters,
// falling back to an Authorization header. Both Last.fm's api_key and sk are
// accepted because a piper API key is the only credential the server issues.
func (h *lastfmCompatHandler) lookupKey(r *http.Request, params ...string) (key *apikey.ApiKey, value, param string) {
	for _, name := range params {
		candidate := strings.TrimSpace(r.FormValue(name))
		if candidate == "" {
			continue
		}
		apiKey, valid := h.sessionManager.ApiKeyMgr.GetApiKey(candidate)
		if !valid {
			return nil, candidate, name
		}
		return apiKey, candidate, name
	}

	if candidate, err := apikey.ExtractApiKey(r); err == nil {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			apiKey, valid := h.sessionManager.ApiKeyMgr.GetApiKey(candidate)
			if !valid {
				return nil, candidate, "authorization"
			}
			return apiKey, candidate, "authorization"
		}
	}

	return nil, "", ""
}

func (h *lastfmCompatHandler) requireKey(w http.ResponseWriter, r *http.Request, params ...string) (*apikey.ApiKey, string, bool) {
	key, value, param := h.lookupKey(r, params...)
	if key != nil {
		return key, value, true
	}

	code := lastfmErrInvalidAPIKey
	message := "Invalid API key - You must be granted a valid key by last.fm"
	if param == "sk" {
		code = lastfmErrInvalidSession
		message = "Invalid session key - Please re-authenticate"
	}
	h.writeError(w, r, http.StatusForbidden, code, message)
	return nil, "", false
}

// getToken returns the submitted piper API key as a Last.fm auth token. It
// only exists so clients that insist on the full token/session handshake can
// complete it with a key the user already created in the piper web UI.
func (h *lastfmCompatHandler) getToken(w http.ResponseWriter, r *http.Request) {
	_, token, ok := h.requireKey(w, r, "api_key")
	if !ok {
		return
	}

	if wantsLastfmJSON(r) {
		writeLastfmJSON(w, http.StatusOK, lastfmJSONTokenResponse{Token: token})
		return
	}
	writeLastfmXML(w, http.StatusOK, lastfmXMLTokenResponse{Status: "ok", Token: token})
}

func (h *lastfmCompatHandler) getSession(w http.ResponseWriter, r *http.Request) {
	key, value, ok := h.requireKey(w, r, "token", "api_key")
	if !ok {
		return
	}

	user, err := h.database.GetUserByID(key.UserID)
	if err != nil {
		log.Printf("lastfm compat: could not load user %d for auth.getSession: %v", key.UserID, err)
		h.writeError(w, r, http.StatusInternalServerError, lastfmErrOperationFail, "Operation failed - Something else went wrong")
		return
	}
	if user == nil {
		h.writeError(w, r, http.StatusForbidden, lastfmErrInvalidAPIKey, "Invalid API key - You must be granted a valid key by last.fm")
		return
	}

	name := fmt.Sprintf("user-%d", user.ID)
	if user.LastFMUsername != nil && *user.LastFMUsername != "" {
		name = *user.LastFMUsername
	} else if user.Username != nil && *user.Username != "" {
		name = *user.Username
	} else if user.Handle != nil && *user.Handle != "" {
		name = *user.Handle
	}

	if wantsLastfmJSON(r) {
		writeLastfmJSON(w, http.StatusOK, lastfmJSONSessionResponse{Session: lastfmJSONSession{
			Name:       name,
			Key:        value,
			Subscriber: 0,
		}})
		return
	}
	writeLastfmXML(w, http.StatusOK, lastfmXMLSessionResponse{
		Status: "ok",
		Session: lastfmXMLSession{
			Name:       name,
			Key:        value,
			Subscriber: 0,
		},
	})
}

func (h *lastfmCompatHandler) scrobble(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireKey(w, r, "sk", "api_key")
	if !ok {
		return
	}

	inputs := parseLastfmScrobbles(r)
	if len(inputs) == 0 {
		h.writeError(w, r, http.StatusBadRequest, lastfmErrInvalidParams, "Invalid parameters - Your request is missing a required parameter")
		return
	}

	var (
		accepted int
		ignored  int
		saved    []savedListen
		response []lastfmScrobbleResult
	)

	now := time.Now()
	for _, input := range inputs {
		result := lastfmScrobbleResult{
			Artist:         input.Artist,
			Track:          input.Track,
			Album:          input.Album,
			AlbumArtist:    input.AlbumArtist,
			Timestamp:      input.Timestamp,
			IgnoredMessage: lastfmIgnoredAccepted,
		}

		switch {
		case input.Artist == "":
			result.IgnoredCode = 1
			result.IgnoredMessage = "Artist was ignored"
		case input.Track == "":
			result.IgnoredCode = 2
			result.IgnoredMessage = "Track was ignored"
		case input.Timestamp <= 0:
			result.IgnoredCode = 3
			result.IgnoredMessage = "Timestamp was ignored"
		case time.Unix(input.Timestamp, 0).After(now.Add(lastfmMaxFutureSkew)):
			result.IgnoredCode = 4
			result.IgnoredMessage = "Timestamp was ignored"
		}

		if result.IgnoredCode != 0 {
			ignored++
			response = append(response, result)
			continue
		}

		timestamp := time.Unix(input.Timestamp, 0).UTC()
		track := models.Track{
			Name:           input.Track,
			Artist:         []models.Artist{{Name: input.Artist}},
			Album:          input.Album,
			Timestamp:      timestamp,
			DurationMs:     input.DurationSeconds * 1000,
			ServiceBaseUrl: "lastfm",
			HasStamped:     true,
		}
		if input.MBID != "" {
			mbid := input.MBID
			track.RecordingMBID = &mbid
		}

		exists, err := h.database.HasTrackListen(user.UserID, db.SourceLastfm, track.Name, track.Timestamp)
		if err != nil {
			log.Printf("lastfm compat: could not check existing scrobble for user %d: %v", user.UserID, err)
		} else if exists {
			// A retry of an already accepted scrobble is still a success.
			accepted++
			response = append(response, result)
			continue
		}

		trackID, err := h.database.SaveTrack(user.UserID, db.SourceLastfm, &track)
		if err != nil {
			log.Printf("lastfm compat: could not save scrobble for user %d: %v", user.UserID, err)
			h.writeError(w, r, http.StatusInternalServerError, lastfmErrOperationFail, "Operation failed - Something else went wrong")
			return
		}

		accepted++
		response = append(response, result)
		saved = append(saved, savedListen{trackID: trackID, track: track})
	}

	if len(saved) > 0 {
		go hydrateAndSubmitTracks(h.database, h.atprotoService, h.mbService, user.UserID, db.SourceLastfm, saved)
	}

	log.Printf("lastfm compat: accepted %d scrobble(s), ignored %d for user %d", accepted, ignored, user.UserID)

	if wantsLastfmJSON(r) {
		writeLastfmJSON(w, http.StatusOK, lastfmJSONScrobblesResponse{Scrobbles: lastfmJSONScrobbles{
			Attr:     lastfmJSONScrobbleAttr{Accepted: accepted, Ignored: ignored},
			Scrobble: lastfmJSONScrobbleValue(response),
		}})
		return
	}

	writeLastfmXML(w, http.StatusOK, lastfmXMLScrobblesResponse{
		Status:    "ok",
		Scrobbles: lastfmXMLScrobbles{Accepted: accepted, Ignored: ignored, Scrobble: lastfmXMLScrobbleList(response)},
	})
}

func (h *lastfmCompatHandler) updateNowPlaying(w http.ResponseWriter, r *http.Request) {
	user, _, ok := h.requireKey(w, r, "sk", "api_key")
	if !ok {
		return
	}

	artist := strings.TrimSpace(r.FormValue("artist"))
	trackName := strings.TrimSpace(r.FormValue("track"))
	if artist == "" || trackName == "" {
		h.writeError(w, r, http.StatusBadRequest, lastfmErrInvalidParams, "Invalid parameters - Your request is missing a required parameter")
		return
	}

	track := models.Track{
		Name:           trackName,
		Artist:         []models.Artist{{Name: artist}},
		Album:          strings.TrimSpace(r.FormValue("album")),
		Timestamp:      time.Now().UTC(),
		ServiceBaseUrl: "lastfm",
		HasStamped:     false,
	}
	if duration, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("duration")), 10, 64); err == nil && duration > 0 {
		track.DurationMs = duration * 1000
	}
	if mbid := strings.TrimSpace(r.FormValue("mbid")); mbid != "" {
		track.RecordingMBID = &mbid
	}

	if h.playingNowService != nil {
		if err := h.playingNowService.PublishPlayingNow(r.Context(), user.UserID, &track); err != nil {
			// Now-playing is best effort, just like the ListenBrainz endpoint.
			log.Printf("lastfm compat: could not publish now playing for user %d: %v", user.UserID, err)
		}
	}

	nowPlaying := lastfmScrobbleResult{
		Artist:         artist,
		Track:          trackName,
		Album:          track.Album,
		AlbumArtist:    strings.TrimSpace(r.FormValue("albumArtist")),
		IgnoredMessage: lastfmIgnoredAccepted,
	}

	if wantsLastfmJSON(r) {
		writeLastfmJSON(w, http.StatusOK, lastfmJSONNowPlayingResponse{NowPlaying: lastfmJSONScrobbleFromResult(nowPlaying)})
		return
	}
	writeLastfmXML(w, http.StatusOK, lastfmXMLNowPlayingResponse{
		Status:     "ok",
		NowPlaying: lastfmXMLNowPlayingFromResult(nowPlaying),
	})
}

type lastfmScrobbleInput struct {
	Artist          string
	Track           string
	Album           string
	AlbumArtist     string
	MBID            string
	Timestamp       int64
	DurationSeconds int64
}

func parseLastfmScrobbles(r *http.Request) []lastfmScrobbleInput {
	indices := make(map[int]struct{})
	for name := range r.Form {
		match := lastfmIndexedParamPattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		index, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		indices[index] = struct{}{}
	}

	if len(indices) == 0 {
		// Some clients send the single-scrobble variant without indexes.
		if r.FormValue("artist") == "" && r.FormValue("track") == "" && r.FormValue("timestamp") == "" {
			return nil
		}
		return []lastfmScrobbleInput{parseLastfmScrobble(r, "")}
	}

	ordered := make([]int, 0, len(indices))
	for index := range indices {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)

	inputs := make([]lastfmScrobbleInput, 0, len(ordered))
	for _, index := range ordered {
		inputs = append(inputs, parseLastfmScrobble(r, fmt.Sprintf("[%d]", index)))
	}
	return inputs
}

func parseLastfmScrobble(r *http.Request, suffix string) lastfmScrobbleInput {
	input := lastfmScrobbleInput{
		Artist:      strings.TrimSpace(r.FormValue("artist" + suffix)),
		Track:       strings.TrimSpace(r.FormValue("track" + suffix)),
		Album:       strings.TrimSpace(r.FormValue("album" + suffix)),
		AlbumArtist: strings.TrimSpace(r.FormValue("albumArtist" + suffix)),
		MBID:        strings.TrimSpace(r.FormValue("mbid" + suffix)),
	}
	if timestamp, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("timestamp"+suffix)), 10, 64); err == nil {
		input.Timestamp = timestamp
	}
	if duration, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("duration"+suffix)), 10, 64); err == nil && duration > 0 {
		input.DurationSeconds = duration
	}
	return input
}

type lastfmScrobbleResult struct {
	Artist         string
	Track          string
	Album          string
	AlbumArtist    string
	Timestamp      int64
	IgnoredCode    int
	IgnoredMessage string
}

const lastfmIgnoredAccepted = ""

func wantsLastfmJSON(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.FormValue("format")), "json")
}

type lastfmJSONText struct {
	Text      string `json:"#text"`
	Corrected string `json:"corrected"`
}

type lastfmJSONIgnored struct {
	Text string `json:"#text"`
	Code string `json:"code"`
}

type lastfmJSONScrobble struct {
	Artist         lastfmJSONText    `json:"artist"`
	Album          lastfmJSONText    `json:"album"`
	Track          lastfmJSONText    `json:"track"`
	AlbumArtist    lastfmJSONText    `json:"albumArtist"`
	Timestamp      string            `json:"timestamp,omitempty"`
	IgnoredMessage lastfmJSONIgnored `json:"ignoredMessage"`
}

type lastfmJSONScrobbleAttr struct {
	Accepted int `json:"accepted"`
	Ignored  int `json:"ignored"`
}

type lastfmJSONScrobbles struct {
	Attr     lastfmJSONScrobbleAttr `json:"@attr"`
	Scrobble any                    `json:"scrobble"`
}

type lastfmJSONScrobblesResponse struct {
	Scrobbles lastfmJSONScrobbles `json:"scrobbles"`
}

type lastfmJSONNowPlayingResponse struct {
	NowPlaying lastfmJSONScrobble `json:"nowplaying"`
}

type lastfmJSONTokenResponse struct {
	Token string `json:"token"`
}

type lastfmJSONSession struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Subscriber int    `json:"subscriber"`
}

type lastfmJSONSessionResponse struct {
	Session lastfmJSONSession `json:"session"`
}

type lastfmJSONErrorResponse struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func lastfmJSONScrobbleFromResult(result lastfmScrobbleResult) lastfmJSONScrobble {
	scrobble := lastfmJSONScrobble{
		Artist:         lastfmJSONText{Text: result.Artist, Corrected: "0"},
		Album:          lastfmJSONText{Text: result.Album, Corrected: "0"},
		Track:          lastfmJSONText{Text: result.Track, Corrected: "0"},
		AlbumArtist:    lastfmJSONText{Text: result.AlbumArtist, Corrected: "0"},
		IgnoredMessage: lastfmJSONIgnored{Text: result.IgnoredMessage, Code: strconv.Itoa(result.IgnoredCode)},
	}
	if result.Timestamp > 0 {
		scrobble.Timestamp = strconv.FormatInt(result.Timestamp, 10)
	}
	return scrobble
}

func lastfmJSONScrobbleValue(results []lastfmScrobbleResult) any {
	if len(results) == 1 {
		return lastfmJSONScrobbleFromResult(results[0])
	}
	scrobbles := make([]lastfmJSONScrobble, 0, len(results))
	for _, result := range results {
		scrobbles = append(scrobbles, lastfmJSONScrobbleFromResult(result))
	}
	return scrobbles
}

type lastfmXMLText struct {
	Corrected string `xml:"corrected,attr"`
	Text      string `xml:",chardata"`
}

type lastfmXMLIgnored struct {
	Code string `xml:"code,attr"`
	Text string `xml:",chardata"`
}

type lastfmXMLScrobble struct {
	Track          lastfmXMLText    `xml:"track"`
	Artist         lastfmXMLText    `xml:"artist"`
	Album          lastfmXMLText    `xml:"album"`
	AlbumArtist    lastfmXMLText    `xml:"albumArtist"`
	Timestamp      int64            `xml:"timestamp,omitempty"`
	IgnoredMessage lastfmXMLIgnored `xml:"ignoredMessage"`
}

type lastfmXMLNowPlaying struct {
	Track          lastfmXMLText    `xml:"track"`
	Artist         lastfmXMLText    `xml:"artist"`
	Album          lastfmXMLText    `xml:"album"`
	AlbumArtist    lastfmXMLText    `xml:"albumArtist"`
	IgnoredMessage lastfmXMLIgnored `xml:"ignoredMessage"`
}

type lastfmXMLScrobbles struct {
	Accepted int                 `xml:"accepted,attr"`
	Ignored  int                 `xml:"ignored,attr"`
	Scrobble []lastfmXMLScrobble `xml:"scrobble"`
}

type lastfmXMLScrobblesResponse struct {
	XMLName   xml.Name           `xml:"lfm"`
	Status    string             `xml:"status,attr"`
	Scrobbles lastfmXMLScrobbles `xml:"scrobbles"`
}

type lastfmXMLNowPlayingResponse struct {
	XMLName    xml.Name            `xml:"lfm"`
	Status     string              `xml:"status,attr"`
	NowPlaying lastfmXMLNowPlaying `xml:"nowplaying"`
}

type lastfmXMLTokenResponse struct {
	XMLName xml.Name `xml:"lfm"`
	Status  string   `xml:"status,attr"`
	Token   string   `xml:"token"`
}

type lastfmXMLSession struct {
	Name       string `xml:"name"`
	Key        string `xml:"key"`
	Subscriber int    `xml:"subscriber"`
}

type lastfmXMLSessionResponse struct {
	XMLName xml.Name         `xml:"lfm"`
	Status  string           `xml:"status,attr"`
	Session lastfmXMLSession `xml:"session"`
}

type lastfmXMLError struct {
	Code    int    `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type lastfmXMLErrorResponse struct {
	XMLName xml.Name       `xml:"lfm"`
	Status  string         `xml:"status,attr"`
	Error   lastfmXMLError `xml:"error"`
}

func lastfmXMLScrobbleFromResult(result lastfmScrobbleResult) lastfmXMLScrobble {
	return lastfmXMLScrobble{
		Track:          lastfmXMLText{Corrected: "0", Text: result.Track},
		Artist:         lastfmXMLText{Corrected: "0", Text: result.Artist},
		Album:          lastfmXMLText{Corrected: "0", Text: result.Album},
		AlbumArtist:    lastfmXMLText{Corrected: "0", Text: result.AlbumArtist},
		Timestamp:      result.Timestamp,
		IgnoredMessage: lastfmXMLIgnored{Code: strconv.Itoa(result.IgnoredCode), Text: result.IgnoredMessage},
	}
}

func lastfmXMLScrobbleList(results []lastfmScrobbleResult) []lastfmXMLScrobble {
	scrobbles := make([]lastfmXMLScrobble, 0, len(results))
	for _, result := range results {
		scrobbles = append(scrobbles, lastfmXMLScrobbleFromResult(result))
	}
	return scrobbles
}

func lastfmXMLNowPlayingFromResult(result lastfmScrobbleResult) lastfmXMLNowPlaying {
	return lastfmXMLNowPlaying{
		Track:          lastfmXMLText{Corrected: "0", Text: result.Track},
		Artist:         lastfmXMLText{Corrected: "0", Text: result.Artist},
		Album:          lastfmXMLText{Corrected: "0", Text: result.Album},
		AlbumArtist:    lastfmXMLText{Corrected: "0", Text: result.AlbumArtist},
		IgnoredMessage: lastfmXMLIgnored{Code: strconv.Itoa(result.IgnoredCode), Text: result.IgnoredMessage},
	}
}

func (h *lastfmCompatHandler) writeError(w http.ResponseWriter, r *http.Request, status, code int, message string) {
	if wantsLastfmJSON(r) {
		writeLastfmJSON(w, status, lastfmJSONErrorResponse{Error: code, Message: message})
		return
	}
	writeLastfmXML(w, status, lastfmXMLErrorResponse{
		Status: "failed",
		Error:  lastfmXMLError{Code: code, Message: message},
	})
}

func writeLastfmJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("lastfm compat: could not encode JSON response: %v", err)
	}
}

func writeLastfmXML(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		log.Printf("lastfm compat: could not write XML header: %v", err)
		return
	}
	if err := xml.NewEncoder(w).Encode(value); err != nil {
		log.Printf("lastfm compat: could not encode XML response: %v", err)
	}
}

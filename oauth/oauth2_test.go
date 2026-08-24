package oauth

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type mockReceiver struct {
	returnID  int64
	returnErr error
	called    bool
	lastUID   int64
}

func (m *mockReceiver) SetAccessToken(token, refresh string, uid int64) (int64, error) {
	m.called = true
	m.lastUID = uid

	if m.returnErr != nil {
		return 0, m.returnErr
	}

	if m.returnID != 0 {
		return m.returnID, nil
	}

	return 42, nil
}

func noUser(context.Context) int64 { return 0 }

func stateFromLogin(svc *Service) string {
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	svc.HandleLogin(rr, req)
	loc := rr.Result().Header.Get("Location")
	u, _ := url.Parse(loc)
	return u.Query().Get("state")
}

func forgedState(svc *Service) string {
	_ = stateFromLogin(svc)
	return "forged-state"
}

func consumedState(svc *Service) string {
	s := stateFromLogin(svc)
	_, _ = svc.completeLogin(context.Background(), completeLoginParams{State: s, Code: "c"})
	return s
}

func queryMissingCode(svc *Service) string {
	s := stateFromLogin(svc)
	return "state=" + url.QueryEscape(s) + "&error=access_denied"
}

func queryValidCode(svc *Service) string {
	s := stateFromLogin(svc)
	return "state=" + url.QueryEscape(s) + "&code=c"
}

func queryMismatch(_ *Service) string {
	return "state=not-issued&code=c"
}

func okTokenHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"at","token_type":"bearer","refresh_token":"rt"}`))
}

func errorTokenHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(500)
	_, _ = w.Write([]byte(`{"error":"server_error"}`))
}

func wantErr(substr string) func(t *testing.T, gotID int64, err error, r *mockReceiver) {
	return func(t *testing.T, _ int64, err error, _ *mockReceiver) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), substr) {
			t.Fatalf("err = %v, want containing %q", err, substr)
		}
	}
}

func wantID(want int64) func(t *testing.T, gotID int64, err error, r *mockReceiver) {
	return func(t *testing.T, gotID int64, err error, _ *mockReceiver) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected err %v", err)
		}
		if gotID != want {
			t.Fatalf("id = %d, want %d", gotID, want)
		}
	}
}

func wantIDWithSession(wantID, wantUID int64) func(t *testing.T, gotID int64, err error, r *mockReceiver) {
	return func(t *testing.T, gotID int64, err error, r *mockReceiver) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected err %v", err)
		}
		if gotID != wantID {
			t.Fatalf("id = %d, want %d", gotID, wantID)
		}
		if r.lastUID != wantUID {
			t.Fatalf("receiver = %+v, want uid %d", r, wantUID)
		}
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	tests := []struct {
		name     string
		verifier string
		want     string
	}{
		{
			name:     "rfc7636",
			verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			want:     "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		},
		{
			name:     "empty",
			verifier: "",
			want:     "47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU",
		},
		{
			name:     "hello",
			verifier: "hello",
			want:     "LPJNul-wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateCodeChallenge(tt.verifier); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleLogin_Redirect(t *testing.T) {
	svc := NewOAuth2Service(
		oauth2.Config{
			ClientID: "id",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "http://example.com/auth",
				TokenURL: "http://example.com/token",
			},
		},
		nil,
		log.New(io.Discard, "", 0),
		noUser,
	)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/login", nil)
	rr := httptest.NewRecorder()
	svc.HandleLogin(rr, req)

	resp := rr.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("invalid Location: %v", err)
	}
	q := u.Query()

	if q.Get("state") == "" {
		t.Fatalf("missing state")
	}
	if q.Get("code_challenge") == "" {
		t.Fatalf("missing code_challenge")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("method = %q, want S256", q.Get("code_challenge_method"))
	}
}

func TestHandleLogin_ChallengeBindsToStoredVerifier(t *testing.T) {
	var capturedVerifier string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		capturedVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
	}))
	defer ts.Close()

	cfg := oauth2.Config{
		ClientID:     "id",
		ClientSecret: "s",
		RedirectURL:  "http://localhost/cb",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "http://example.com/auth",
			TokenURL: ts.URL,
		},
	}
	svc := NewOAuth2Service(cfg, &mockReceiver{returnID: 1}, log.New(io.Discard, "", 0), noUser)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()
	svc.HandleLogin(rr, req)
	loc := rr.Result().Header.Get("Location")
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	challenge := u.Query().Get("code_challenge")

	reqCB := httptest.NewRequest(http.MethodGet, "/callback?state="+url.QueryEscape(state)+"&code=c", nil)
	rrCB := httptest.NewRecorder()
	if _, err := svc.HandleCallback(rrCB, reqCB); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	if capturedVerifier == "" {
		t.Fatalf("no code_verifier sent to token endpoint")
	}
	if got := generateCodeChallenge(capturedVerifier); got != challenge {
		t.Fatalf("challenge %q != S256(verifier %q) %q", challenge, capturedVerifier, got)
	}
}

func TestCompleteLogin(t *testing.T) {
	tests := []struct {
		name      string
		receiver  *mockReceiver
		tokenFunc http.HandlerFunc
		setup     func(svc *Service) string
		code      string
		uid       int64
		check     func(t *testing.T, gotID int64, err error, receiver *mockReceiver)
	}{
		{
			name:      "state mismatch",
			receiver:  &mockReceiver{},
			tokenFunc: okTokenHandler,
			setup:     forgedState,
			code:      "c",
			check:     wantErr("state mismatch"),
		},
		{
			name:      "no code",
			receiver:  &mockReceiver{},
			tokenFunc: okTokenHandler,
			setup:     stateFromLogin,
			code:      "",
			check:     wantErr("no code provided"),
		},
		{
			name:      "no receiver",
			receiver:  nil,
			tokenFunc: okTokenHandler,
			setup:     stateFromLogin,
			code:      "c",
			check:     wantErr("token receiver not configured"),
		},
		{
			name:      "exchange failure",
			receiver:  &mockReceiver{},
			tokenFunc: errorTokenHandler,
			setup:     stateFromLogin,
			code:      "c",
			check:     wantErr("failed to exchange"),
		},
		{
			name:      "success",
			receiver:  &mockReceiver{returnID: 777},
			tokenFunc: okTokenHandler,
			setup:     stateFromLogin,
			code:      "c",
			check:     wantID(777),
		},
		{
			name:      "success with session",
			receiver:  &mockReceiver{returnID: 99},
			tokenFunc: okTokenHandler,
			setup:     stateFromLogin,
			code:      "c",
			uid:       555,
			check:     wantIDWithSession(99, 555),
		},
		{
			name:      "replay single-use",
			receiver:  &mockReceiver{returnID: 1},
			tokenFunc: okTokenHandler,
			setup:     consumedState,
			code:      "c",
			check:     wantErr("state mismatch"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.tokenFunc)
			defer ts.Close()

			var recv TokenReceiver
			if tt.receiver != nil {
				recv = tt.receiver
			}
			svc := NewOAuth2Service(
				oauth2.Config{
					ClientID:     "id",
					ClientSecret: "s",
					RedirectURL:  "http://localhost/cb",
					Endpoint: oauth2.Endpoint{
						AuthURL:  "http://example.com/auth",
						TokenURL: ts.URL,
					},
				},
				recv,
				log.New(io.Discard, "", 0),
				noUser,
			)
			state := tt.setup(svc)

			gotID, err := svc.completeLogin(t.Context(), completeLoginParams{State: state, Code: tt.code, UserID: tt.uid})
			tt.check(t, gotID, err, tt.receiver)
		})
	}
}

func TestHandleCallback_HTTPMapping(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(svc *Service) string
		receiver   TokenReceiver
		wantStatus int
	}{
		{
			name:       "missing code maps to 400",
			setup:      queryMissingCode,
			receiver:   &mockReceiver{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "nil receiver maps to 500",
			setup:      queryValidCode,
			receiver:   nil,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "state mismatch maps to 400",
			setup:      queryMismatch,
			receiver:   &mockReceiver{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"at","token_type":"bearer"}`))
			}))
			defer ts.Close()

			svc := NewOAuth2Service(
				oauth2.Config{
					ClientID: "id",
					Endpoint: oauth2.Endpoint{
						AuthURL:  "http://a",
						TokenURL: ts.URL,
					},
				},
				tt.receiver,
				log.New(io.Discard, "", 0),
				noUser,
			)

			query := tt.setup(svc)
			req := httptest.NewRequest(http.MethodGet, "/callback?"+query, nil)
			rr := httptest.NewRecorder()
			_, _ = svc.HandleCallback(rr, req)

			resp := rr.Result()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

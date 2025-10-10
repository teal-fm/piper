package atproto

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	_ "github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/session"

	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
)

type ATprotoAuthService struct {
	clientApp      *oauth.ClientApp
	DB             *db.DB
	sessionManager *session.SessionManager
	clientId       string
	callbackUrl    string
	logger         *log.Logger
}

func NewATprotoAuthService(db *db.DB, sessionManager *session.SessionManager, clientSecretKey string, clientId string, callbackUrl string, clientSecretId string) (*ATprotoAuthService, error) {
	fmt.Println(clientId, callbackUrl)

	scopes := []string{"atproto", "repo:fm.teal.*"}

	var config oauth.ClientConfig
	config = oauth.NewPublicConfig(clientId, callbackUrl, scopes)

	priv, err := crypto.ParsePrivateMultibase(clientSecretKey)
	if err != nil {
		return nil, err
	}
	if err := config.SetClientSecret(priv, clientSecretId); err != nil {
		return nil, err
	}

	//TODO write a sqlite store
	oauthClient := oauth.NewClientApp(&config, oauth.NewMemStore())

	//cli, err := oldoauth.NewClient(oldoauth.ClientArgs{
	//	ClientJwk:   jwks,
	//	ClientId:    clientId,
	//	RedirectUri: callbackUrl,
	//})
	//if err != nil {
	//	return nil, fmt.Errorf("failed to create atproto oldoauth client: %w", err)
	//}

	logger := log.New(os.Stdout, "ATProto oauth: ", log.LstdFlags|log.Lmsgprefix)

	svc := &ATprotoAuthService{
		clientApp:      oauthClient,
		callbackUrl:    callbackUrl,
		DB:             db,
		sessionManager: sessionManager,
		clientId:       clientId,
		logger:         logger,
	}
	//svc.NewXrpcClient()
	return svc, nil
}

func (a *ATprotoAuthService) GetATProtoClient(accountDID string, sessionID string, ctx context.Context) (*client.APIClient, error) {
	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		return nil, err
	}

	oauthSess, err := a.clientApp.ResumeSession(ctx, did, sessionID)
	if err != nil {
		return nil, err
	}

	return oauthSess.APIClient(), nil

}

func (a *ATprotoAuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	handle := r.URL.Query().Get("handle")
	if handle == "" {
		a.logger.Printf("ATProto Login Error: handle is required")
		http.Error(w, "handle query parameter is required", http.StatusBadRequest)
		return
	}

	authUrl, err := a.getLoginUrlAndSaveState(r.Context(), handle)
	if err != nil {
		a.logger.Printf("ATProto Login Error: Failed to get login URL for handle %s: %v", handle, err)
		http.Error(w, fmt.Sprintf("Error initiating login: %v", err), http.StatusInternalServerError)
		return
	}

	a.logger.Printf("ATProto Login: Redirecting user %s to %s", handle, authUrl.String())
	http.Redirect(w, r, authUrl.String(), http.StatusFound)
}

func (a *ATprotoAuthService) getLoginUrlAndSaveState(ctx context.Context, handle string) (*url.URL, error) {

	redirectURL, err := a.clientApp.StartAuthFlow(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("error creating oauth redirect url: %w", err)
	}
	parsedRedirectURL, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing oauth redirect url: %w", err)
	}
	return parsedRedirectURL, nil
}

func (a *ATprotoAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error) {
	ctx := r.Context()

	sessData, err := a.clientApp.ProcessCallback(ctx, r.URL.Query())
	if err != nil {
		errMsg := fmt.Errorf("processing OAuth callback: %w", err)
		http.Error(w, errMsg.Error(), http.StatusBadRequest)
		return 0, errMsg
	}

	// It's in the example repo and leaving for some debugging cause i've seen different scopes cause issues before
	// so may be some nice debugging info to have
	if !slices.Equal(sessData.Scopes, a.clientApp.Config.Scopes) {
		a.logger.Printf("session auth scopes did not match those requested")
	}

	user, err := a.DB.FindOrCreateUserByDID(sessData.AccountDID.String())
	if err != nil {
		a.logger.Printf("ATProto Callback Error: Failed to find or create user for DID %s: %v", sessData.AccountDID.String(), err)
		http.Error(w, "Failed to process user information.", http.StatusInternalServerError)
		return 0, fmt.Errorf("failed to find or create user")
	}

	//This is piper's session for manging piper, not atproto sessions
	createdSession := a.sessionManager.CreateSession(user.ID, sessData.SessionID)
	a.sessionManager.SetSessionCookie(w, createdSession)
	a.logger.Printf("Created session for user %d via service atproto", user)
	err = a.DB.SetLatestATProtoSessionId(sessData.AccountDID.String(), sessData.SessionID)
	if err != nil {
		a.logger.Printf("Failed to set latest atproto session id for user %d: %v", user.ID, err)
	}

	a.logger.Printf("ATProto Callback Success: User %d (DID: %s) authenticated.", user.ID, user.ATProtoDID)
	return user.ID, nil
}

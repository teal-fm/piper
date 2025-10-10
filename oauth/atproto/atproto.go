package atproto

import (
	"context"
	"fmt"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	_ "github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	oldoauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/haileyok/atproto-oauth-golang/helpers"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/teal-fm/piper/db"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
)

type ATprotoAuthService struct {
	//client *oldoauth.Client
	//jwks        jwk.Key
	clientApp   *oauth.ClientApp
	DB          *db.DB
	clientId    string
	callbackUrl string
	xrpc        *oldoauth.XrpcClient
	logger      *log.Logger
}

func NewATprotoAuthService(db *db.DB, clientSecretKey string, clientId string, callbackUrl string, clientSecretId string) (*ATprotoAuthService, error) {
	fmt.Println(clientId, callbackUrl)
	//TODO move to env and have defaults ifnot there
	scopes := []string{"atproto", "transition:generic"}

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
		clientApp:   oauthClient,
		callbackUrl: callbackUrl,
		DB:          db,
		clientId:    clientId,
		logger:      logger,
	}
	svc.NewXrpcClient()
	return svc, nil
}

func (a *ATprotoAuthService) GetATProtoClient(accountDID string, sessionID string) (*client.APIClient, error) {
	//DID here is the session key
	//Session id is the session id, if the backend calls it prob use the newest one created? Handle outside of the method

	//TODO need to take into account each client is for a user in logic
	//if a.clientApp != nil {
	//	return a.clientApp, nil
	//}
	//TODO I have no idea if this is right
	context := context.Background()

	did, err := syntax.ParseDID(accountDID)
	if err != nil {
		return nil, err
	}

	oauthSess, err := a.clientApp.ResumeSession(context, did, sessionID)
	if err != nil {
		return nil, err
	}

	return oauthSess.APIClient(), nil

	//if a.client == nil {
	//	cli, err := oldoauth.NewClient(oldoauth.ClientArgs{
	//		ClientJwk:   a.jwks,
	//		ClientId:    a.clientId,
	//		RedirectUri: a.callbackUrl,
	//	})
	//	if err != nil {
	//		return nil, fmt.Errorf("failed to create atproto oldoauth client: %w", err)
	//	}
	//	a.client = cli
	//}
	//
	//return a.client, nil
}

func LoadJwks(jwksBytes []byte) (jwk.Key, error) {
	key, err := helpers.ParseJWKFromBytes(jwksBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWK from bytes: %w", err)
	}
	return key, nil
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
	if !slices.Equal(sessData.Scopes, a.clientApp.Config.Scopes) {
		a.logger.Printf("session auth scopes did not match those requested")
		//slog.Warn("session auth scopes did not match those requested", "requested", s.OAuth.Config.Scopes, "granted", sessData.Scopes)
	}

	//state := r.URL.Query().Get("state")
	//code := r.URL.Query().Get("code")
	//issuer := r.URL.Query().Get("iss") // Issuer (auth base URL) is needed for token request
	//
	//if state == "" || code == "" || issuer == "" {
	//	errMsg := r.URL.Query().Get("error")
	//	errDesc := r.URL.Query().Get("error_description")
	//	a.logger.Printf("ATProto Callback Error: Missing parameters. State: '%s', Code: '%s', Issuer: '%s'. Error: '%s', Description: '%s'", state, code, issuer, errMsg, errDesc)
	//	http.Error(w, fmt.Sprintf("Authorization callback failed: %s (%s). Missing state, code, or issuer.", errMsg, errDesc), http.StatusBadRequest)
	//	return 0, fmt.Errorf("missing state, code, or issuer")
	//}
	//
	//// Retrieve saved data using state
	//data, err := a.DB.GetATprotoAuthData(state)
	//if err != nil {
	//	a.logger.Printf("ATProto Callback Error: Failed to retrieve auth data for state '%s': %v", state, err)
	//	http.Error(w, "Invalid or expired state.", http.StatusBadRequest)
	//	return 0, fmt.Errorf("invalid or expired state")
	//}

	// Clean up the temporary auth data now that we've retrieved it
	// defer a.DB.DeleteATprotoAuthData(state) // Consider adding deletion logic
	// if issuers don't match, return an error
	if data.AuthServerIssuer != issuer {
		a.logger.Printf("ATProto Callback Error: Issuer mismatch for state '%s', expected '%s', got '%s'", state, data.AuthServerIssuer, issuer)
		http.Error(w, "Invalid or expired state.", http.StatusBadRequest)
		return 0, fmt.Errorf("issuer mismatch")
	}

	resp, err := a.client.InitialTokenRequest(r.Context(), code, issuer, data.PKCEVerifier, data.DPoPAuthServerNonce, data.DPoPPrivateJWK)
	if err != nil {
		a.logger.Printf("ATProto Callback Error: Failed initial token request for state '%s', issuer '%s': %v", state, issuer, err)
		http.Error(w, fmt.Sprintf("Error exchanging code for token: %v", err), http.StatusInternalServerError)
		return 0, fmt.Errorf("failed initial token request")
	}

	userID, err := a.DB.FindOrCreateUserByDID(data.DID)
	if err != nil {
		a.logger.Printf("ATProto Callback Error: Failed to find or create user for DID %s: %v", data.DID, err)
		http.Error(w, "Failed to process user information.", http.StatusInternalServerError)
		return 0, fmt.Errorf("failed to find or create user")
	}

	err = a.DB.SaveATprotoSession(resp, data.AuthServerIssuer, data.DPoPPrivateJWK, data.PDSUrl)
	if err != nil {
		a.logger.Printf("ATProto Callback Error: Failed to save ATProto tokens for user %d (DID %s): %v", userID.ID, data.DID, err)
	}

	a.logger.Printf("ATProto Callback Success: User %d (DID: %s) authenticated.", userID.ID, data.DID)
	return userID.ID, nil
}

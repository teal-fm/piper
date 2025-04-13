// Modify piper/oauth/atproto/atproto.go
package atproto

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/haileyok/atproto-oauth-golang/helpers"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/teal-fm/piper/db"
	"github.com/teal-fm/piper/models"
	// woof
)

type ATprotoAuthService struct {
	client   *oauth.Client
	jwks     jwk.Key
	DB       *db.DB
	clientId string
}

func NewATprotoAuthService(db *db.DB, jwks jwk.Key, clientId string, callbackUrl string) (*ATprotoAuthService, error) {
	fmt.Println(clientId, callbackUrl)
	cli, err := oauth.NewClient(oauth.ClientArgs{
		ClientJwk:   jwks,
		ClientId:    clientId,
		RedirectUri: callbackUrl,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create atproto oauth client: %w", err)
	}
	return &ATprotoAuthService{
		client:   cli,
		jwks:     jwks,
		DB:       db,
		clientId: clientId,
	}, nil
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
		log.Printf("ATProto Login Error: handle is required")
		http.Error(w, "handle query parameter is required", http.StatusBadRequest)
		return
	}

	authUrl, err := a.getLoginUrlAndSaveState(r.Context(), handle)
	if err != nil {
		log.Printf("ATProto Login Error: Failed to get login URL for handle %s: %v", handle, err)
		http.Error(w, fmt.Sprintf("Error initiating login: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("ATProto Login: Redirecting user %s to %s", handle, authUrl.String())
	http.Redirect(w, r, authUrl.String(), http.StatusFound)
}

func (a *ATprotoAuthService) getLoginUrlAndSaveState(ctx context.Context, handle string) (*url.URL, error) {
	scope := "atproto"
	// resolve
	ui, err := a.getUserInformation(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("failed to get user information for %s: %w", handle, err)
	}

	// create a dpop jwk for this session
	k, err := helpers.GenerateKey(nil) // Generate ephemeral DPoP key for this flow
	if err != nil {
		return nil, fmt.Errorf("failed to generate DPoP key: %w", err)
	}

	// Send PAR auth req
	parResp, err := a.client.SendParAuthRequest(ctx, ui.AuthServer, ui.AuthMeta, ui.Handle, scope, k)
	if err != nil {
		return nil, fmt.Errorf("failed PAR request to %s: %w", ui.AuthServer, err)
	}

	// Save state including generated PKCE verifier and DPoP key
	data := &models.ATprotoAuthData{
		State:               parResp.State,
		DID:                 ui.DID,
		PDSUrl:              ui.AuthServer,
		AuthServerIssuer:    ui.AuthMeta.Issuer,
		PKCEVerifier:        parResp.PkceVerifier,
		DPoPAuthServerNonce: parResp.DpopAuthserverNonce,
		DPoPPrivateJWK:      k,
	}

	// print data
	fmt.Println(data)

	err = a.DB.SaveATprotoAuthData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to save ATProto auth data for state %s: %w", parResp.State, err)
	}

	// Construct authorization URL using the request_uri from PAR response
	authEndpointURL, err := url.Parse(ui.AuthMeta.AuthorizationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization endpoint URL %s: %w", ui.AuthMeta.AuthorizationEndpoint, err)
	}
	q := authEndpointURL.Query()
	q.Set("client_id", a.clientId)
	q.Set("request_uri", parResp.RequestUri)
	q.Set("state", parResp.State)
	authEndpointURL.RawQuery = q.Encode()

	return authEndpointURL, nil
}

func (a *ATprotoAuthService) HandleCallback(w http.ResponseWriter, r *http.Request) (int64, error) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	issuer := r.URL.Query().Get("iss") // Issuer (PDS URL) is needed for token request

	if state == "" || code == "" || issuer == "" {
		errMsg := r.URL.Query().Get("error")
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("ATProto Callback Error: Missing parameters. State: '%s', Code: '%s', Issuer: '%s'. Error: '%s', Description: '%s'", state, code, issuer, errMsg, errDesc)
		http.Error(w, fmt.Sprintf("Authorization callback failed: %s (%s). Missing state, code, or issuer.", errMsg, errDesc), http.StatusBadRequest)
		return 0, fmt.Errorf("missing state, code, or issuer")
	}

	// Retrieve saved data using state
	data, err := a.DB.GetATprotoAuthData(state)
	if err != nil {
		log.Printf("ATProto Callback Error: Failed to retrieve auth data for state '%s': %v", state, err)
		http.Error(w, "Invalid or expired state.", http.StatusBadRequest)
		return 0, fmt.Errorf("invalid or expired state")
	}

	// Clean up the temporary auth data now that we've retrieved it
	// defer a.DB.DeleteATprotoAuthData(state) // Consider adding deletion logic
	// if issuers don't match, return an error
	if data.AuthServerIssuer != issuer {
		log.Printf("ATProto Callback Error: Issuer mismatch for state '%s', expected '%s', got '%s'", state, data.AuthServerIssuer, issuer)
		http.Error(w, "Invalid or expired state.", http.StatusBadRequest)
		return 0, fmt.Errorf("issuer mismatch")
	}

	resp, err := a.client.InitialTokenRequest(r.Context(), code, issuer, data.PKCEVerifier, data.DPoPAuthServerNonce, data.DPoPPrivateJWK)
	if err != nil {
		log.Printf("ATProto Callback Error: Failed initial token request for state '%s', issuer '%s': %v", state, issuer, err)
		http.Error(w, fmt.Sprintf("Error exchanging code for token: %v", err), http.StatusInternalServerError)
		return 0, fmt.Errorf("failed initial token request")
	}

	userID, err := a.DB.FindOrCreateUserByDID(data.DID)
	if err != nil {
		log.Printf("ATProto Callback Error: Failed to find or create user for DID %s: %v", data.DID, err)
		http.Error(w, "Failed to process user information.", http.StatusInternalServerError)
		return 0, fmt.Errorf("failed to find or create user")
	}

	err = a.DB.SaveATprotoSession(resp)
	if err != nil {
		log.Printf("ATProto Callback Error: Failed to save ATProto tokens for user %d (DID %s): %v", userID.ID, data.DID, err)
	}

	log.Printf("ATProto Callback Success: User %d (DID: %s) authenticated.", userID.ID, data.DID)
	return userID.ID, nil // Return the piper user ID
}

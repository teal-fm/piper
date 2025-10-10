// oauth/atproto/http.go
package atproto

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func strPtr(raw string) *string {
	return &raw
}

func (a *ATprotoAuthService) HandleJwks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	body := a.clientApp.Config.PublicJWKS()
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	//pubKey, err := a.jwks.PublicKey()
	//if err != nil {
	//	http.Error(w, fmt.Sprintf("Error getting public key from JWK: %v", err), http.StatusInternalServerError)
	//	log.Printf("Error getting public key from JWK: %v", err)
	//	return
	//}
	//
	//w.Header().Set("Content-Type", "application/json")
	//if err := json.NewEncoder(w).Encode(helpers.CreateJwksResponseObject(pubKey)); err != nil {
	//	log.Printf("Error encoding JWKS response: %v", err)
	//}
}

func (a *ATprotoAuthService) HandleClientMetadata(w http.ResponseWriter, r *http.Request, serverUrlRoot, serverMetadataUrl, serverCallbackUrl string) {

	meta := a.clientApp.Config.ClientMetadata()
	if a.clientApp.Config.IsConfidential() {
		meta.JWKSURI = strPtr(fmt.Sprintf("%s/oauth/jwks.json", serverUrlRoot))
	}
	meta.ClientName = strPtr("Piper Telekinesis")
	meta.ClientURI = strPtr(serverUrlRoot)

	// internal consistency check
	if err := meta.Validate(a.clientApp.Config.ClientID); err != nil {
		a.logger.Printf("validating client metadata", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	//metadata := map[string]any{
	//	"client_id":                       serverMetadataUrl,
	//	"client_name":                     "Piper Telekinesis",
	//	"client_uri":                      serverUrlRoot,
	//	"logo_uri":                        fmt.Sprintf("%s/logo.png", serverUrlRoot),
	//	"tos_uri":                         fmt.Sprintf("%s/tos", serverUrlRoot),
	//	"policy_url":                      fmt.Sprintf("%s/policy", serverUrlRoot),
	//	"redirect_uris":                   []string{serverCallbackUrl},
	//	"grant_types":                     []string{"authorization_code", "refresh_token"},
	//	"response_types":                  []string{"code"},
	//	"application_type":                "web",
	//	"dpop_bound_access_tokens":        true,
	//	"jwks_uri":                        fmt.Sprintf("%s/oauth/jwks.json", serverUrlRoot),
	//	"scope":                           "atproto transition:generic",
	//	"token_endpoint_auth_method":      "private_key_jwt",
	//	"token_endpoint_auth_signing_alg": "ES256",
	//}
	//w.Header().Set("Content-Type", "application/json")
	//if err := json.NewEncoder(w).Encode(metadata); err != nil {
	//	log.Printf("Error encoding client metadata: %v", err)
	//}
}

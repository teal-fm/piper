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

}

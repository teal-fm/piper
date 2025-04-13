// Add this struct definition to piper/models/atproto.go
package models

import (
	"github.com/lestrrat-go/jwx/v2/jwk"
)

type ATprotoAuthData struct {
	State               string  `json:"state"`
	DID                 string  `json:"did"`
	PDSUrl              string  `json:"pds_url"`
	AuthServerIssuer    string  `json:"authserver_issuer"`
	PARState            string  `json:"par_state"`
	PKCEVerifier        string  `json:"pkce_verifier"`
	DPoPAuthServerNonce string  `json:"dpop_authserver_nonce"`
	DPoPPrivateJWK      jwk.Key `json:"dpop_private_jwk"`
}

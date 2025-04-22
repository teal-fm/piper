package models

import (
	"time"

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

type TealFmPlayLexicon struct {
	Type                  string    `json:"$type"`
	Duration              int       `json:"duration"`
	TrackName             string    `json:"trackName"`
	PlayedTime            time.Time `json:"playedTime"`
	ArtistMbIDs           []string  `json:"artistMbIds"`
	ArtistNames           []string  `json:"artistNames"`
	ReleaseMbID           string    `json:"releaseMbId"`
	ReleaseName           string    `json:"releaseName"`
	RecordingMbID         string    `json:"recordingMbId"`
	SubmissionClientAgent string    `json:"submissionClientAgent"`
}

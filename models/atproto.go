package models

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	cbg "github.com/whyrusleeping/cbor-gen"
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

type ATprotoAuthSession struct {
	ID                  string    `json:"id"`
	DID                 string    `json:"did"`
	PDSUrl              string    `json:"pds_url"`
	AuthServerIssuer    string    `json:"authserver_issuer"`
	AccessToken         string    `json:"access_token"`
	RefreshToken        string    `json:"refresh_token"`
	DpopPdsNonce        string    `json:"dpop_pds_nonce"`
	DpopAuthServerNonce string    `json:"dpop_authserver_nonce"`
	DpopPrivateJWK      jwk.Key   `json:"dpop_private_jwk"`
	TokenExpiry         time.Time `json:"expires_at"`
}

type TealFmFeedPlay struct {
	Type                  string   `json:"$type"`
	Duration              int      `json:"duration"`
	TrackName             string   `json:"trackName"`
	PlayedTime            string   `json:"playedTime"`
	ArtistMbIDs           []string `json:"artistMbIds"`
	ArtistNames           []string `json:"artistNames"`
	ReleaseMBID           string   `json:"releaseMbId"`
	ReleaseName           string   `json:"releaseName"`
	RecordingMBID         string   `json:"recordingMbId"`
	SubmissionClientAgent string   `json:"submissionClientAgent"`
}

func (tfmTrack TealFmFeedPlay) MarshalCBOR(w io.Writer) error {
	// in case the pointer is nil
	// if tfmTrack == nil {
	// 	return fmt.Errorf("cannot marshal nil TealFmFeedPlay")
	// }
	fmt.Println("Marshalling", tfmTrack)
	trackBytes, err := json.Marshal(tfmTrack)
	if err != nil {
		return fmt.Errorf("failed to marshal trackMap to bytes: %w", err)
	}

	cw := cbg.NewCborWriter(w)
	if err := cbg.WriteByteArray(cw, trackBytes); err != nil {
		return fmt.Errorf("failed to write bytes as CBOR: %w", err)
	}
	return nil
}

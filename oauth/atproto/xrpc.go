package atproto

import (
	"log/slog"

	oauth "github.com/haileyok/atproto-oauth-golang"
)

func (atp *ATprotoAuthService) NewXrpcClient() {
	atp.xrpc = &oauth.XrpcClient{
		OnDpopPdsNonceChanged: func(did, newNonce string) {
			_, err := atp.DB.Exec("UPDATE users SET atproto_pds_nonce = ? WHERE atproto_did = ?", newNonce, did)
			if err != nil {
				slog.Default().Error("error updating pds nonce", "err", err)
			}
		},
	}
}

func (atp *ATprotoAuthService) GetXrpcClient() *oauth.XrpcClient {
	if atp.xrpc == nil {
		atp.NewXrpcClient()
	}
	return atp.xrpc
}

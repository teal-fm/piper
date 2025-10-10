package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/teal-fm/piper/models"
)

//type ATprotoAuthData struct {
//	State               string    `json:"state"`
//	DID                 string    `json:"did"`
//	PDSUrl              string    `json:"pds_url"`
//	AuthServerIssuer    string    `json:"authserver_issuer"`
//	PKCEVerifier        string    `json:"pkce_verifier"`
//	DPoPAuthServerNonce string    `json:"dpop_authserver_nonce"`
//	DPoPPrivateJWK      jwk.Key   `json:"dpop_private_jwk"`
//	CreatedAt           time.Time `json:"created_at"`
//}

//func (db *DB) SaveATprotoAuthData(data *models.ATprotoAuthData) error {
//	dpopPrivateJWKBytes, err := json.Marshal(data.DPoPPrivateJWK)
//	if err != nil {
//		return err
//	}
//
//	_, err = db.Exec(`
//	INSERT INTO atproto_auth_data (state, did, pds_url, authserver_issuer, pkce_verifier, dpop_authserver_nonce, dpop_private_jwk)
//	VALUES (?, ?, ?, ?, ?, ?, ?)`,
//		data.State, data.DID, data.PDSUrl, data.AuthServerIssuer, data.PKCEVerifier, data.DPoPAuthServerNonce, string(dpopPrivateJWKBytes))
//
//	return err
//}
//
//func (db *DB) GetATprotoAuthData(state string) (*models.ATprotoAuthData, error) {
//	var data models.ATprotoAuthData
//	var dpopPrivateJWKString string
//
//	err := db.QueryRow(`
//	SELECT state, did, pds_url, authserver_issuer, pkce_verifier, dpop_authserver_nonce, dpop_private_jwk
//	FROM atproto_auth_data
//	WHERE state = ?`,
//		state).Scan(
//		&data.State,
//		&data.DID,
//		&data.PDSUrl,
//		&data.AuthServerIssuer,
//		&data.PKCEVerifier,
//		&data.DPoPAuthServerNonce,
//		&dpopPrivateJWKString,
//	)
//	if err != nil {
//		if err == sql.ErrNoRows {
//			return nil, fmt.Errorf("no auth data found for state %s: %w", state, err)
//		}
//		return nil, fmt.Errorf("failed to scan auth data for state %s: %w", state, err)
//	}
//
//	key, err := helpers.ParseJWKFromBytes([]byte(dpopPrivateJWKString))
//	if err != nil {
//		return nil, fmt.Errorf("failed to parse DPoPPrivateJWK for state %s: %w", state, err)
//	}
//	data.DPoPPrivateJWK = key
//
//	return &data, nil
//}

func (db *DB) FindOrCreateUserByDID(did string) (*models.User, error) {
	var user models.User
	err := db.QueryRow(`
	SELECT id, atproto_did, created_at, updated_at
	FROM users
	WHERE atproto_did = ?`,
		did).Scan(&user.ID, &user.ATProtoDID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		now := time.Now().UTC()
		// create user!
		result, insertErr := db.Exec(`
			INSERT INTO users (atproto_did, created_at, updated_at)
			VALUES (?, ?, ?)
			`,
			did,
			now,
			now)
		if insertErr != nil {
			return nil, fmt.Errorf("failed to create user: %w", insertErr)
		}
		lastID, idErr := result.LastInsertId()
		if idErr != nil {
			return nil, fmt.Errorf("failed to get last insert id: %w", idErr)
		}
		user.ID = lastID
		user.ATProtoDID = &did
		user.CreatedAt = now
		user.UpdatedAt = now
		return &user, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to find user by DID: %w", err)
	}

	return &user, err
}

func (db *DB) SetLatestATProtoSessionId(did string, atProtoSessionID string) error {
	db.logger.Printf("Setting latest atproto session id for did %s to %s", did, atProtoSessionID)
	now := time.Now().UTC()

	result, err := db.Exec(`
		UPDATE users
		SET 
		    most_recent_at_session_id = ?
			updated_at = ?
		WHERE atproto_did = ?`,
		atProtoSessionID,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to update atproto session for did %s: %w", did, atProtoSessionID)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// it's possible the update succeeded here?
		return fmt.Errorf("failed to check rows affected after updating atproto session for did %s: %w", did, atProtoSessionID)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no user found with did %s to update session, creating new session", did)
	}

	return nil
}

//// create or update the current user's ATproto session data.
//func (db *DB) SaveATprotoSession(tokenResp *oauth.TokenResponse, authserverIss string, dpopPrivateJWK jwk.Key, pdsUrl string) error {
//	db.logger.Printf("Saving session with PDS url %s", pdsUrl)
//	expiryTime := time.Now().UTC().Add(time.Second * time.Duration(tokenResp.ExpiresIn))
//	now := time.Now().UTC()
//
//	dpopPrivateJWKBytes, err := json.Marshal(dpopPrivateJWK)
//	if err != nil {
//		return err
//	}
//
//	result, err := db.Exec(`
//		UPDATE users
//		SET atproto_access_token = ?,
//			atproto_refresh_token = ?,
//			atproto_token_expiry = ?,
//			atproto_scope = ?,
//			atproto_sub = ?,
//			atproto_authserver_issuer = ?,
//			atproto_token_type = ?,
//			atproto_authserver_nonce = ?,
//			atproto_dpop_private_jwk = ?,
//			atproto_pds_url = ?,
//			atproto_pds_nonce = ?,
//			updated_at = ?
//		WHERE atproto_did = ?`,
//		tokenResp.AccessToken,
//		tokenResp.RefreshToken,
//		expiryTime,
//		tokenResp.Scope,
//		tokenResp.Sub,
//		authserverIss,
//		tokenResp.TokenType,
//		tokenResp.DpopAuthserverNonce,
//		string(dpopPrivateJWKBytes),
//		pdsUrl,
//		// will get set later
//		"",
//		now,
//		tokenResp.Sub,
//	)
//
//	if err != nil {
//		return fmt.Errorf("failed to update atproto session for did %s: %w", tokenResp.Sub, err)
//	}
//
//	rowsAffected, err := result.RowsAffected()
//	if err != nil {
//		// it's possible the update succeeded here?
//		return fmt.Errorf("failed to check rows affected after updating atproto session for did %s: %w", tokenResp.Sub, err)
//	}
//
//	if rowsAffected == 0 {
//		return fmt.Errorf("no user found with did %s to update session, creating new session", tokenResp.Sub)
//	}
//
//	return nil
//}

//func (db *DB) GetAtprotoSession(did string, ctx context.Context, oauthClient oauth.Client) (*models.ATprotoAuthSession, error) {
//	var oauthSession models.ATprotoAuthSession
//	var authserverIss string
//	var jwkBytes string
//
//	err := db.QueryRow(
//		`
//		SELECT id,
//		       atproto_did,
//		       atproto_pds_url,
//		       atproto_authserver_issuer,
//		       atproto_access_token,
//		       atproto_refresh_token,
//		       atproto_pds_nonce,
//		       atproto_authserver_nonce,
//		       atproto_dpop_private_jwk,
//		       atproto_token_expiry
//		FROM users
//		WHERE atproto_did = ?`,
//		did,
//	).Scan(
//		&oauthSession.ID,
//		&oauthSession.DID,
//		&oauthSession.PDSUrl,
//		&authserverIss,
//		&oauthSession.AccessToken,
//		&oauthSession.RefreshToken,
//		&oauthSession.DpopPdsNonce,
//		&oauthSession.DpopAuthServerNonce,
//		&jwkBytes,
//		&oauthSession.TokenExpiry,
//	)
//
//	if err != nil {
//		return nil, fmt.Errorf("failed to get atproto session for did %s: %w", did, err)
//	}
//
//	privateJwk, err := helpers.ParseJWKFromBytes([]byte(jwkBytes))
//	if err != nil {
//		return nil, fmt.Errorf("failed to parse DPoPPrivateJWK: %w", err)
//	} else {
//		// add jwk to the struct
//		oauthSession.DpopPrivateJWK = privateJwk
//	}
//
//	// printout the session details
//	db.logger.Printf("Getting session details for the did: %+v\n", oauthSession.DID)
//
//	// if token is expired, refresh it
//	if time.Now().UTC().After(oauthSession.TokenExpiry) {
//
//		resp, err := oauthClient.RefreshTokenRequest(ctx, oauthSession.RefreshToken, authserverIss, oauthSession.DpopAuthServerNonce, privateJwk)
//		if err != nil {
//			return nil, err
//		}
//
//		if err := db.SaveATprotoSession(resp, authserverIss, privateJwk, oauthSession.PDSUrl); err != nil {
//			return nil, fmt.Errorf("failed to save refreshed token: %w", err)
//		}
//
//		oauthSession = models.ATprotoAuthSession{
//			ID:                  oauthSession.ID,
//			DID:                 oauthSession.DID,
//			PDSUrl:              oauthSession.PDSUrl,
//			AuthServerIssuer:    authserverIss,
//			AccessToken:         resp.AccessToken,
//			RefreshToken:        resp.RefreshToken,
//			DpopPdsNonce:        oauthSession.DpopPdsNonce,
//			DpopAuthServerNonce: resp.DpopAuthserverNonce,
//			DpopPrivateJWK:      privateJwk,
//			TokenExpiry:         time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second),
//		}
//
//	}
//
//	return &oauthSession, nil
//}

//func AtpSessionToAuthArgs(sess *models.ATprotoAuthSession) *oauth.XrpcAuthedRequestArgs {
//	//Commenting out so jwts and tokens are not in logs
//	//fmt.Printf("DID: %s\nPDS URL: %s\nISS: %s\nAccess Token: %s\nNonce: %s\nPrivate JWK: %s\n", sess.DID, sess.PDSUrl, sess.AuthServerIssuer, sess.AccessToken, sess.DpopPdsNonce, sess.DpopPrivateJWK)
//	return &oauth.XrpcAuthedRequestArgs{
//		Did:            sess.DID,
//		PdsUrl:         sess.PDSUrl,
//		Issuer:         sess.AuthServerIssuer,
//		AccessToken:    sess.AccessToken,
//		DpopPdsNonce:   sess.DpopPdsNonce,
//		DpopPrivateJwk: sess.DpopPrivateJWK,
//	}
//}

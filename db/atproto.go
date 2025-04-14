package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/haileyok/atproto-oauth-golang/helpers"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/teal-fm/piper/models"
)

type ATprotoAuthData struct {
	State               string    `json:"state"`
	DID                 string    `json:"did"`
	PDSUrl              string    `json:"pds_url"`
	AuthServerIssuer    string    `json:"authserver_issuer"`
	PKCEVerifier        string    `json:"pkce_verifier"`
	DPoPAuthServerNonce string    `json:"dpop_authserver_nonce"`
	DPoPPrivateJWK      jwk.Key   `json:"dpop_private_jwk"`
	CreatedAt           time.Time `json:"created_at"`
}

func (db *DB) SaveATprotoAuthData(data *models.ATprotoAuthData) error {
	dpopPrivateJWKBytes, err := json.Marshal(data.DPoPPrivateJWK)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
	INSERT INTO atproto_auth_data (state, did, pds_url, authserver_issuer, pkce_verifier, dpop_authserver_nonce, dpop_private_jwk)
	VALUES (?, ?, ?, ?, ?, ?, ?)`,
		data.State, data.DID, data.PDSUrl, data.AuthServerIssuer, data.PKCEVerifier, data.DPoPAuthServerNonce, string(dpopPrivateJWKBytes))

	return err
}

func (db *DB) GetATprotoAuthData(state string) (*models.ATprotoAuthData, error) {
	var data models.ATprotoAuthData
	var dpopPrivateJWKString string

	err := db.QueryRow(`
	SELECT state, did, pds_url, authserver_issuer, pkce_verifier, dpop_authserver_nonce, dpop_private_jwk
	FROM atproto_auth_data
	WHERE state = ?`,
		state).Scan(
		&data.State,
		&data.DID,
		&data.PDSUrl,
		&data.AuthServerIssuer,
		&data.PKCEVerifier,
		&data.DPoPAuthServerNonce,
		&dpopPrivateJWKString,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no auth data found for state %s: %w", state, err)
		}
		return nil, fmt.Errorf("failed to scan auth data for state %s: %w", state, err)
	}

	key, err := helpers.ParseJWKFromBytes([]byte(dpopPrivateJWKString))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DPoPPrivateJWK for state %s: %w", state, err)
	}
	data.DPoPPrivateJWK = key

	return &data, nil
}

func (db *DB) FindOrCreateUserByDID(did string) (*models.User, error) {
	var user models.User
	err := db.QueryRow(`
	SELECT id, atproto_did, created_at, updated_at
	FROM users
	WHERE atproto_did = ?`,
		did).Scan(&user.ID, &user.ATProtoDID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		now := time.Now()
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

// create or update the current user's ATproto session data.
func (db *DB) SaveATprotoSession(tokenResp *oauth.TokenResponse) error {

	expiryTime := time.Now().Add(time.Second * time.Duration(tokenResp.ExpiresIn))
	now := time.Now()

	result, err := db.Exec(`
		UPDATE users
		SET atproto_access_token = ?,
			atproto_refresh_token = ?,
			atproto_token_expiry = ?,
			atproto_scope = ?,
			atproto_token_type = ?,
			updated_at = ?
		WHERE atproto_did = ?`,
		tokenResp.AccessToken,
		tokenResp.RefreshToken,
		expiryTime,
		tokenResp.Scope,
		tokenResp.TokenType,
		now,
		tokenResp.Sub,
	)

	if err != nil {
		return fmt.Errorf("failed to update atproto session for did %s: %w", tokenResp.Sub, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// it's possible the update succeeded here?
		return fmt.Errorf("failed to check rows affected after updating atproto session for did %s: %w", tokenResp.Sub, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no user found with did %s to update session, creating new session", tokenResp.Sub)
	}

	return nil
}

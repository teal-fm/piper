package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
		    most_recent_at_session_id = ?,
			updated_at = ?
		WHERE atproto_did = ?`,
		atProtoSessionID,
		now,
		did,
	)
	if err != nil {
		db.logger.Printf("%v", err)
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

type SqliteATProtoStore struct {
	db *sql.DB
}

var _ *oauth.ClientAuthStore = &SqliteATProtoStore{}

func NewSqliteATProtoStore(db *sql.DB) *SqliteATProtoStore {
	return &SqliteATProtoStore{
		db: db,
	}
}

func sessionKey(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s/%s", did, sessionID)
}

func (s *SqliteATProtoStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	lookUpKey := sessionKey(did, sessionID)
	session := oauth.ClientSessionData{}
	err := s.db.QueryRow(`
	SELECT *	
		account_did,
	    look_up_key,
		session_id,
		host_url,
		authserver_url,
		authserver_token_endpoint,
		authserver_revocation_endpoint,
		scopes,
		access_token,
		refresh_token,
		dpop_authserver_nonce,
		dpop_host_nonce,
		dpop_privatekey_multibase,
	FROM atproto_sessions 
	WHERE look_up_key = ?`, lookUpKey).Scan(
		session.AccountDID,
		session.SessionID,
		session.HostURL,
		session.AuthServerURL,
		session.AuthServerTokenEndpoint,
		session.AuthServerRevocationEndpoint,
		session.Scopes,
		session.AccessToken,
		session.RefreshToken,
		session.DPoPAuthServerNonce,
		session.DPoPHostNonce,
		session.DPoPPrivateKeyMultibase)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &session, nil
}

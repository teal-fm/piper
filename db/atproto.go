package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/teal-fm/piper/models"
)

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

var _ oauth.ClientAuthStore = (*SqliteATProtoStore)(nil)

func NewSqliteATProtoStore(db *sql.DB) *SqliteATProtoStore {
	return &SqliteATProtoStore{
		db: db,
	}
}

func sessionKey(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s/%s", did, sessionID)
}

func splitScopes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func joinScopes(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	return strings.Join(scopes, " ")
}

func (s *SqliteATProtoStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	lookUpKey := sessionKey(did, sessionID)

	var (
		accountDIDStr                string
		lookUpKeyStr                 string
		sessionIDStr                 string
		hostURL                      string
		authServerURL                string
		authServerTokenEndpoint      string
		authServerRevocationEndpoint string
		scopesStr                    string
		accessToken                  string
		refreshToken                 string
		dpopAuthServerNonce          string
		dpopHostNonce                string
		dpopPrivateKeyMultibase      string
	)

	err := s.db.QueryRow(`
		SELECT account_did,
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
		       dpop_privatekey_multibase
		FROM atproto_sessions
		WHERE look_up_key = ?
	`, lookUpKey).Scan(
		&accountDIDStr,
		&lookUpKeyStr,
		&sessionIDStr,
		&hostURL,
		&authServerURL,
		&authServerTokenEndpoint,
		&authServerRevocationEndpoint,
		&scopesStr,
		&accessToken,
		&refreshToken,
		&dpopAuthServerNonce,
		&dpopHostNonce,
		&dpopPrivateKeyMultibase,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", lookUpKey)
	}
	if err != nil {
		return nil, err
	}

	accDID, err := syntax.ParseDID(accountDIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid account DID in session: %w", err)
	}

	sess := oauth.ClientSessionData{
		AccountDID:                   accDID,
		SessionID:                    sessionIDStr,
		HostURL:                      hostURL,
		AuthServerURL:                authServerURL,
		AuthServerTokenEndpoint:      authServerTokenEndpoint,
		AuthServerRevocationEndpoint: authServerRevocationEndpoint,
		Scopes:                       splitScopes(scopesStr),
		AccessToken:                  accessToken,
		RefreshToken:                 refreshToken,
		DPoPAuthServerNonce:          dpopAuthServerNonce,
		DPoPHostNonce:                dpopHostNonce,
		DPoPPrivateKeyMultibase:      dpopPrivateKeyMultibase,
	}

	return &sess, nil
}

func (s *SqliteATProtoStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	lookUpKey := sessionKey(sess.AccountDID, sess.SessionID)
	// simple upsert: delete then insert
	_, _ = s.db.Exec(`DELETE FROM atproto_sessions WHERE look_up_key = ?`, lookUpKey)
	_, err := s.db.Exec(`
		INSERT INTO atproto_sessions (
			look_up_key,
			account_did,
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
			dpop_privatekey_multibase
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		lookUpKey,
		sess.AccountDID.String(),
		sess.SessionID,
		sess.HostURL,
		sess.AuthServerURL,
		sess.AuthServerTokenEndpoint,
		sess.AuthServerRevocationEndpoint,
		joinScopes(sess.Scopes),
		sess.AccessToken,
		sess.RefreshToken,
		sess.DPoPAuthServerNonce,
		sess.DPoPHostNonce,
		sess.DPoPPrivateKeyMultibase,
	)
	return err
}

func (s *SqliteATProtoStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	lookUpKey := sessionKey(did, sessionID)
	_, err := s.db.Exec(`DELETE FROM atproto_sessions WHERE look_up_key = ?`, lookUpKey)
	return err
}

func (s *SqliteATProtoStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	var (
		authServerURL                string
		accountDIDStr                sql.NullString
		scopesStr                    string
		requestURI                   string
		authServerTokenEndpoint      string
		authServerRevocationEndpoint string
		pkceVerifier                 string
		dpopAuthServerNonce          string
		dpopPrivateKeyMultibase      string
	)
	err := s.db.QueryRow(`
		SELECT authserver_url,
		       account_did,
		       scopes,
		       request_uri,
		       authserver_token_endpoint,
		       authserver_revocation_endpoint,
		       pkce_verifier,
		       dpop_authserver_nonce,
		       dpop_privatekey_multibase
		FROM atproto_state
		WHERE state = ?
	`, state).Scan(
		&authServerURL,
		&accountDIDStr,
		&scopesStr,
		&requestURI,
		&authServerTokenEndpoint,
		&authServerRevocationEndpoint,
		&pkceVerifier,
		&dpopAuthServerNonce,
		&dpopPrivateKeyMultibase,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("request info not found: %s", state)
	}
	if err != nil {
		return nil, err
	}
	var accountDIDPtr *syntax.DID
	if accountDIDStr.Valid && accountDIDStr.String != "" {
		acc, err := syntax.ParseDID(accountDIDStr.String)
		if err != nil {
			return nil, fmt.Errorf("invalid account DID in auth request: %w", err)
		}
		accountDIDPtr = &acc
	}
	info := oauth.AuthRequestData{
		State:                        state,
		AuthServerURL:                authServerURL,
		AccountDID:                   accountDIDPtr,
		Scopes:                       splitScopes(scopesStr),
		RequestURI:                   requestURI,
		AuthServerTokenEndpoint:      authServerTokenEndpoint,
		AuthServerRevocationEndpoint: authServerRevocationEndpoint,
		PKCEVerifier:                 pkceVerifier,
		DPoPAuthServerNonce:          dpopAuthServerNonce,
		DPoPPrivateKeyMultibase:      dpopPrivateKeyMultibase,
	}
	return &info, nil
}

func (s *SqliteATProtoStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	// ensure not already exists
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM atproto_state WHERE state = ?`, info.State).Scan(&exists)
	if err == nil {
		return fmt.Errorf("auth request already saved for state %s", info.State)
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var accountDIDStr interface{}
	if info.AccountDID != nil {
		accountDIDStr = info.AccountDID.String()
	} else {
		accountDIDStr = nil
	}
	_, err = s.db.Exec(`
		INSERT INTO atproto_state (
			state,
			authserver_url,
			account_did,
			scopes,
			request_uri,
			authserver_token_endpoint,
			authserver_revocation_endpoint,
			pkce_verifier,
			dpop_authserver_nonce,
			dpop_privatekey_multibase
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		info.State,
		info.AuthServerURL,
		accountDIDStr,
		joinScopes(info.Scopes),
		info.RequestURI,
		info.AuthServerTokenEndpoint,
		info.AuthServerRevocationEndpoint,
		info.PKCEVerifier,
		info.DPoPAuthServerNonce,
		info.DPoPPrivateKeyMultibase,
	)
	return err
}

func (s *SqliteATProtoStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	_, err := s.db.Exec(`DELETE FROM atproto_state WHERE state = ?`, state)
	return err
}

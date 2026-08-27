package apikey

import (
	"testing"

	"github.com/teal-fm/piper/db"
)

func testDatabase(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	if err != nil {
		t.Fatalf("new database: %v", err)
	}
	if err := database.Initialize(); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestCreateAPIKeySeparatesSecretFromID(t *testing.T) {
	database := testDatabase(t)
	manager := NewApiKeyManager(database)
	apiKey, rawKey, err := manager.CreateApiKey(1, "test", 30)
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	if rawKey == "" || rawKey == apiKey.ID {
		t.Fatalf("raw key %q must differ from public ID %q", rawKey, apiKey.ID)
	}
	if _, ok := manager.GetApiKey(rawKey); !ok {
		t.Fatal("raw key did not authenticate")
	}
	if _, ok := manager.GetApiKey(apiKey.ID); ok {
		t.Fatal("public ID authenticated as a secret")
	}
	var storedHash string
	if err := database.QueryRow(`SELECT key_hash FROM api_keys WHERE id = ?`, apiKey.ID).Scan(&storedHash); err != nil {
		t.Fatalf("load stored hash: %v", err)
	}
	if storedHash == rawKey || storedHash == "" {
		t.Fatalf("database stored unsafe key value %q", storedHash)
	}
}

func TestLegacyAPIKeyMigrationPreservesAuthentication(t *testing.T) {
	database := testDatabase(t)
	if _, err := database.Exec(`
		CREATE TABLE api_keys (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP,
			expires_at TIMESTAMP
		)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	const legacyKey = "legacy-secret"
	if _, err := database.Exec(`INSERT INTO api_keys (id, user_id, name, created_at, expires_at) VALUES (?, 1, 'legacy', CURRENT_TIMESTAMP, datetime('now', '+1 day'))`, legacyKey); err != nil {
		t.Fatalf("insert legacy key: %v", err)
	}
	manager := NewApiKeyManager(database)
	apiKey, ok := manager.GetApiKey(legacyKey)
	if !ok {
		t.Fatal("legacy key stopped authenticating after migration")
	}
	if apiKey.ID == legacyKey {
		t.Fatal("legacy secret remained the public row ID")
	}
}

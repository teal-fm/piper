package db

import "testing"

func TestLastFMUsernameLifecycle(t *testing.T) {
	database := newTestDB(t)
	userID := createTestUser(t, database)

	if err := database.AddLastFMUsername(userID, "  listener  "); err != nil {
		t.Fatalf("add Last.fm username: %v", err)
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.LastFMUsername == nil || *user.LastFMUsername != "listener" {
		t.Fatalf("got username %v, want listener", user.LastFMUsername)
	}

	if err := database.ClearLastFMUsername(userID); err != nil {
		t.Fatalf("clear Last.fm username: %v", err)
	}
	user, err = database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.LastFMUsername != nil {
		t.Fatalf("got username %q after clear, want nil", *user.LastFMUsername)
	}
}

func TestBlankLastFMUsernameIsDisconnected(t *testing.T) {
	database := newTestDB(t)
	userID := createTestUser(t, database)

	if err := database.AddLastFMUsername(userID, "   "); err != nil {
		t.Fatalf("add blank Last.fm username: %v", err)
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.LastFMUsername != nil {
		t.Fatalf("blank username persisted as %q", *user.LastFMUsername)
	}
}

func TestLegacyUnicodeWhitespaceLastFMUsernameIsNormalized(t *testing.T) {
	database := newTestDB(t)
	userID := createTestUser(t, database)
	if _, err := database.Exec(`UPDATE users SET lastfm_username = ? WHERE id = ?`, "\t\n\u2003", userID); err != nil {
		t.Fatalf("seed legacy username: %v", err)
	}
	if err := database.normalizeLastFMUsernames(); err != nil {
		t.Fatalf("normalize usernames: %v", err)
	}
	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.LastFMUsername != nil {
		t.Fatalf("legacy whitespace username remained %q", *user.LastFMUsername)
	}
}

func TestGetAllUsersWithLastFMUsesUnicodeWhitespaceRules(t *testing.T) {
	database := newTestDB(t)
	blankUserID := createTestUser(t, database)
	connectedUserID := createTestUser(t, database)
	if _, err := database.Exec(`UPDATE users SET lastfm_username = ? WHERE id = ?`, "\t\u2003", blankUserID); err != nil {
		t.Fatalf("seed blank username: %v", err)
	}
	if _, err := database.Exec(`UPDATE users SET lastfm_username = ? WHERE id = ?`, "\tlistener\u2003", connectedUserID); err != nil {
		t.Fatalf("seed connected username: %v", err)
	}
	users, err := database.GetAllUsersWithLastFM()
	if err != nil {
		t.Fatalf("get connected users: %v", err)
	}
	if len(users) != 1 || users[0].LastFMUsername == nil || *users[0].LastFMUsername != "listener" {
		t.Fatalf("got %#v, want one normalized listener", users)
	}
}

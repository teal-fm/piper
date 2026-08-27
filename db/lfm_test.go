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

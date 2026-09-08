package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/teal-fm/piper/models"
)

func TestListenBrainzAccountLifecycle(t *testing.T) {
	database, err := New(filepath.Join(t.TempDir(), "piper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}

	userID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.LinkListenBrainz(userID, "Listener", "secret"); err != nil {
		t.Fatal(err)
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.ListenBrainzUsername == nil || *user.ListenBrainzUsername != "Listener" || user.ListenBrainzToken == nil || *user.ListenBrainzToken != "secret" {
		t.Fatalf("linked account was not loaded: %+v", user)
	}
	syncedAt := time.Unix(123, 0).UTC()
	if err := database.SaveListenBrainzSyncTimestamp(userID, syncedAt); err != nil {
		t.Fatal(err)
	}
	if err := database.LinkListenBrainz(userID, "listener", "new-secret"); err != nil {
		t.Fatal(err)
	}
	user, err = database.GetUserByID(userID)
	if err != nil || user.ListenBrainzSyncedAt == nil || !user.ListenBrainzSyncedAt.Equal(syncedAt) {
		t.Fatalf("same-account relink reset sync cursor: %+v, %v", user, err)
	}
	if err := database.LinkListenBrainz(userID, "different-listener", "new-secret"); err != nil {
		t.Fatal(err)
	}
	user, err = database.GetUserByID(userID)
	if err != nil || user.ListenBrainzSyncedAt != nil {
		t.Fatalf("replacement account kept sync cursor: %+v, %v", user, err)
	}
	if err := database.LinkListenBrainz(userID, "Listener", "secret"); err != nil {
		t.Fatal(err)
	}
	users, err := database.GetAllUsersWithListenBrainz()
	if err != nil || len(users) != 1 {
		t.Fatalf("linked users = %v, %v", users, err)
	}
	byName, err := database.GetUserByListenBrainz("listener")
	if err != nil || byName == nil || byName.ID != userID {
		t.Fatalf("case-insensitive lookup = %+v, %v", byName, err)
	}

	secondID, err := database.CreateUser(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.LinkListenBrainz(secondID, "listener", "other-secret"); err == nil {
		t.Fatal("expected one ListenBrainz account to be linked to only one Piper user")
	}

	if err := database.ClearListenBrainz(userID); err != nil {
		t.Fatal(err)
	}
	user, err = database.GetUserByID(userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.ListenBrainzUsername != nil || user.ListenBrainzToken != nil {
		t.Fatalf("account was not cleared: %+v", user)
	}
}

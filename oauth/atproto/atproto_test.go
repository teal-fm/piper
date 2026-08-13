package atproto

import (
	"slices"
	"testing"
)

func TestATProtoOAuthScopesIncludeRequiredWriteActions(t *testing.T) {
	want := []string{
		"atproto",
		"repo:fm.teal.feed.play?action=create",
		"repo:fm.teal.actor.status?action=create&action=update",
	}

	if got := atprotoOAuthScopes(); !slices.Equal(got, want) {
		t.Fatalf("atprotoOAuthScopes() = %q, want %q", got, want)
	}
}

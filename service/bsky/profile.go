// Package bsky reads public account profiles from the Bluesky AppView.
//
// Piper only ever persists a user's DID, which is not something anyone
// recognises. The AppView turns that DID into a handle, a display name and an
// avatar so the web UI can show who is logged in.
package bsky

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// appViewURL is the public, unauthenticated Bluesky AppView.
const appViewURL = "https://public.api.bsky.app"

// Profile is the subset of app.bsky.actor.defs#profileViewDetailed that piper
// displays. Every field except DID may be empty: handles can be invalidated,
// and display names and avatars are optional.
type Profile struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	// Avatar is a ready-to-use CDN URL, not a blob reference.
	Avatar string `json:"avatar"`
}

// DefaultClient is used when FetchProfile is called without one. The timeout is
// short because the fetch sits in the login redirect path.
var DefaultClient = &http.Client{
	Timeout: 5 * time.Second,
}

// FetchProfile looks up an actor's public profile by DID (or handle). Pass a nil
// client to use DefaultClient; tests pass one pointed at an httptest server.
func FetchProfile(ctx context.Context, client *http.Client, actor string) (*Profile, error) {
	if actor == "" {
		return nil, fmt.Errorf("actor cannot be empty")
	}
	if client == nil {
		client = DefaultClient
	}

	endpoint := fmt.Sprintf("%s/xrpc/app.bsky.actor.getProfile?actor=%s", appViewURL, url.QueryEscape(actor))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profile for %s: %w", actor, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile lookup for %s returned %s", actor, resp.Status)
	}

	var profile Profile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("failed to decode profile for %s: %w", actor, err)
	}

	return &profile, nil
}

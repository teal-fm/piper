package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const tealProfileCollection = "fm.teal.actor.profile"

type Account struct {
	Handle    string
	PDS       string
	AvatarURL string
}

type cacheEntry struct {
	account   Account
	expiresAt time.Time
}

type Resolver struct {
	directory identity.Directory
	client    *http.Client

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func NewResolver() *Resolver {
	return &Resolver{
		directory: identity.DefaultDirectory(),
		client:    &http.Client{Timeout: 4 * time.Second},
		cache:     make(map[string]cacheEntry),
	}
}

func (r *Resolver) Resolve(ctx context.Context, rawDID string) (Account, error) {
	r.mu.RLock()
	cached, ok := r.cache[rawDID]
	r.mu.RUnlock()
	if ok && time.Now().Before(cached.expiresAt) {
		return cached.account, nil
	}

	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return Account{}, fmt.Errorf("parse DID: %w", err)
	}
	ident, err := r.directory.LookupDID(ctx, did)
	if err != nil {
		return Account{}, fmt.Errorf("resolve DID: %w", err)
	}

	handle := ident.Handle.String()
	if handle == "handle.invalid" {
		handle = ""
	}
	pds := strings.TrimRight(ident.PDSEndpoint(), "/")
	account := Account{
		Handle: handle,
		PDS:    strings.TrimPrefix(strings.TrimPrefix(pds, "https://"), "http://"),
	}
	account.AvatarURL = r.tealAvatar(ctx, pds, rawDID)
	if account.AvatarURL == "" {
		account.AvatarURL = r.blueskyAvatar(ctx, rawDID)
	}

	r.mu.Lock()
	r.cache[rawDID] = cacheEntry{account: account, expiresAt: time.Now().Add(10 * time.Minute)}
	r.mu.Unlock()
	return account, nil
}

func (r *Resolver) tealAvatar(ctx context.Context, pds, did string) string {
	if pds == "" {
		return ""
	}
	endpoint, err := url.Parse(pds + "/xrpc/com.atproto.repo.getRecord")
	if err != nil {
		return ""
	}
	query := endpoint.Query()
	query.Set("repo", did)
	query.Set("collection", tealProfileCollection)
	query.Set("rkey", "self")
	endpoint.RawQuery = query.Encode()

	var record struct {
		Value struct {
			Avatar struct {
				Ref struct {
					Link string `json:"$link"`
				} `json:"ref"`
			} `json:"avatar"`
		} `json:"value"`
	}
	if err := r.getJSON(ctx, endpoint.String(), &record); err != nil || record.Value.Avatar.Ref.Link == "" {
		return ""
	}

	blobURL, err := url.Parse(pds + "/xrpc/com.atproto.sync.getBlob")
	if err != nil {
		return ""
	}
	query = blobURL.Query()
	query.Set("did", did)
	query.Set("cid", record.Value.Avatar.Ref.Link)
	blobURL.RawQuery = query.Encode()
	return blobURL.String()
}

func (r *Resolver) blueskyAvatar(ctx context.Context, did string) string {
	endpoint, _ := url.Parse("https://public.api.bsky.app/xrpc/app.bsky.actor.getProfile")
	query := endpoint.Query()
	query.Set("actor", did)
	endpoint.RawQuery = query.Encode()

	var actor struct {
		Avatar string `json:"avatar"`
	}
	if err := r.getJSON(ctx, endpoint.String(), &actor); err != nil {
		return ""
	}
	return actor.Avatar
}

func (r *Resolver) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

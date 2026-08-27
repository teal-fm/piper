package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const tealProfileCollection = "fm.teal.actor.profile"
const playCollection = "fm.teal.feed.play"

var ErrProfilePending = errors.New("profile lookup is still pending")

type Account struct {
	Handle    string `json:"handle"`
	PDS       string `json:"pds"`
	AvatarURL string `json:"avatar_url"`
}

type RecordTarget struct {
	TrackName string
	PlayedAt  time.Time
}

type cacheEntry struct {
	account   Account
	expiresAt time.Time
}

type Resolver struct {
	directory      identity.Directory
	client         *http.Client
	refreshTimeout time.Duration

	mu         sync.RWMutex
	cache      map[string]cacheEntry
	refreshing map[string]bool
}

func NewResolver() *Resolver {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeDialContext
	client := &http.Client{
		Timeout:   4 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return validateHTTPSURL(req.URL)
		},
	}
	return &Resolver{
		directory:      identity.DefaultDirectory(),
		client:         client,
		refreshTimeout: 5 * time.Second,
		cache:          make(map[string]cacheEntry),
		refreshing:     make(map[string]bool),
	}
}

// Cached returns immediately. Expired data remains usable while one background
// refresh runs, and a first-time lookup never delays the connections page.
func (r *Resolver) Cached(rawDID string) (Account, bool) {
	r.mu.Lock()
	cached, ok := r.cache[rawDID]
	needsRefresh := !ok || time.Now().After(cached.expiresAt)
	if needsRefresh && !r.refreshing[rawDID] {
		r.refreshing[rawDID] = true
		go r.refresh(rawDID)
	}
	r.mu.Unlock()
	return cached.account, ok
}

func (r *Resolver) refresh(rawDID string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.refreshTimeout)
	defer cancel()
	account, err := r.resolve(ctx, rawDID)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.refreshing, rawDID)
	if err == nil {
		r.cache[rawDID] = cacheEntry{account: account, expiresAt: time.Now().Add(10 * time.Minute)}
	}
}

func (r *Resolver) Resolve(ctx context.Context, rawDID string) (Account, error) {
	r.mu.RLock()
	cached, ok := r.cache[rawDID]
	r.mu.RUnlock()
	if ok && time.Now().Before(cached.expiresAt) {
		return cached.account, nil
	}

	account, err := r.resolve(ctx, rawDID)
	if err != nil {
		return Account{}, err
	}
	r.mu.Lock()
	r.cache[rawDID] = cacheEntry{account: account, expiresAt: time.Now().Add(10 * time.Minute)}
	r.mu.Unlock()
	return account, nil
}

func (r *Resolver) resolve(ctx context.Context, rawDID string) (Account, error) {
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

func (r *Resolver) LatestRecords(ctx context.Context, rawDID string, targets map[string]RecordTarget) (map[string]string, error) {
	account, ready := r.Cached(rawDID)
	if !ready || account.PDS == "" {
		return nil, ErrProfilePending
	}
	endpoint, err := url.Parse("https://" + account.PDS + "/xrpc/com.atproto.repo.listRecords")
	if err != nil {
		return nil, err
	}
	records := make(map[string]string)
	if len(targets) == 0 {
		return records, nil
	}
	cursor := ""
	for page := 0; page < 10 && len(records) < len(targets); page++ {
		query := endpoint.Query()
		query.Set("repo", rawDID)
		query.Set("collection", playCollection)
		query.Set("limit", "100")
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		endpoint.RawQuery = query.Encode()
		var result struct {
			Cursor  string `json:"cursor"`
			Records []struct {
				URI   string `json:"uri"`
				Value struct {
					MusicServiceURI string `json:"musicServiceUri"`
					TrackName       string `json:"trackName"`
					PlayedTime      string `json:"playedTime"`
				} `json:"value"`
			} `json:"records"`
		}
		if err := r.getJSON(ctx, endpoint.String(), &result); err != nil {
			return nil, err
		}
		for _, record := range result.Records {
			service := musicServiceName(record.Value.MusicServiceURI)
			target, wanted := targets[service]
			if !wanted || records[service] != "" || !recordMatchesTarget(record.Value.TrackName, record.Value.PlayedTime, target) {
				continue
			}
			atURI, err := syntax.ParseATURI(record.URI)
			if err != nil {
				continue
			}
			records[service] = atURI.String()
		}
		cursor = result.Cursor
		if cursor == "" {
			break
		}
	}
	return records, nil
}

func recordMatchesTarget(trackName, playedTime string, target RecordTarget) bool {
	if target.TrackName != "" && !strings.EqualFold(strings.TrimSpace(trackName), strings.TrimSpace(target.TrackName)) {
		return false
	}
	if target.PlayedAt.IsZero() {
		return true
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, playedTime)
	return err == nil && recordedAt.Equal(target.PlayedAt)
}

func musicServiceName(rawURI string) string {
	normalized := strings.ToLower(rawURI)
	switch {
	case strings.Contains(normalized, "spotify"):
		return "spotify"
	case strings.Contains(normalized, "music.apple"), strings.Contains(normalized, "applemusic"):
		return "applemusic"
	case strings.Contains(normalized, "last.fm"), strings.Contains(normalized, "lastfm"):
		return "lastfm"
	default:
		return ""
	}
}

func (r *Resolver) getJSON(ctx context.Context, endpoint string, target any) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if err := validateHTTPSURL(parsed); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
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

func validateHTTPSURL(target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
		return errors.New("remote profile URL must be an HTTPS URL without user information")
	}
	return nil
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if !safePublicAddress(address) {
			continue
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
	}
	return nil, fmt.Errorf("remote profile host %q has no allowed public address", host)
}

var blockedAddressRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func safePublicAddress(address netip.Addr) bool {
	if address.Is4In6() {
		address = address.Unmap()
	}
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	for _, blocked := range blockedAddressRanges {
		if blocked.Contains(address) {
			return false
		}
	}
	return true
}

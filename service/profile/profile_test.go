package profile

import (
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestCachedReturnsExpiredAccountWithoutWaiting(t *testing.T) {
	resolver := &Resolver{
		cache: map[string]cacheEntry{
			"did:plc:test": {
				account:   Account{Handle: "listener.example"},
				expiresAt: time.Now().Add(-time.Minute),
			},
		},
		refreshing: map[string]bool{"did:plc:test": true},
	}

	start := time.Now()
	account, ok := resolver.Cached("did:plc:test")
	if !ok || account.Handle != "listener.example" {
		t.Fatalf("got %#v, %v", account, ok)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("cached lookup took %s", elapsed)
	}
}

func TestValidateHTTPSURL(t *testing.T) {
	for _, rawURL := range []string{"http://pds.example", "https://user@pds.example", "https://"} {
		target, _ := url.Parse(rawURL)
		if err := validateHTTPSURL(target); err == nil {
			t.Errorf("validateHTTPSURL(%q) succeeded", rawURL)
		}
	}
	target, _ := url.Parse("https://pds.example")
	if err := validateHTTPSURL(target); err != nil {
		t.Fatalf("valid HTTPS URL rejected: %v", err)
	}
}

func TestSafePublicAddress(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"169.254.169.254":      false,
		"192.0.2.10":           false,
		"::1":                  false,
		"fc00::1":              false,
		"2001:db8::1":          false,
		"1.1.1.1":              true,
		"2606:4700:4700::1111": true,
	}
	for rawAddress, want := range tests {
		if got := safePublicAddress(netip.MustParseAddr(rawAddress)); got != want {
			t.Errorf("safePublicAddress(%s) = %v, want %v", rawAddress, got, want)
		}
	}
}

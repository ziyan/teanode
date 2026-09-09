package safefetch_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

func TestOnlyPublicAddressesAreDialled(t *testing.T) {
	t.Parallel()

	refused := []struct {
		name    string
		address string
	}{
		{"loopback", "127.0.0.1:80"},
		{"loopback by another name", "127.1.2.3:80"},
		{"loopback over IPv6", "[::1]:80"},
		{"unspecified", "0.0.0.0:80"},
		// The three RFC 1918 ranges, which is where everything a self-hosted
		// mail server sits beside lives.
		{"private 10", "10.0.0.1:80"},
		{"private 172.16", "172.16.0.1:80"},
		{"private 192.168", "192.168.1.1:80"},
		// The cloud metadata service, which is the address this whole check
		// exists for.
		{"link local", "169.254.169.254:80"},
		{"link local over IPv6", "[fe80::1]:80"},
		{"unique local over IPv6", "[fd00::1]:80"},
		// Carrier-grade NAT and the reserved ranges, all routable inside
		// somebody's network and none of them the public internet.
		{"carrier grade nat", "100.64.0.1:80"},
		{"reserved 192.0.0", "192.0.0.1:80"},
		{"benchmarking 198.18", "198.18.0.1:80"},
		// Transition prefixes carry an IPv4 address inside them, which a
		// host speaking them would reach without this check seeing it.
		{"6to4 wrapping a private address", "[2002:c0a8:101::1]:80"},
		{"teredo", "[2001:0:c0a8:101::1]:80"},
		{"nat64 wrapping a private address", "[64:ff9b::c0a8:101]:80"},
		// A name rather than an address cannot reach here, because this runs
		// on what is actually being dialled.
		{"not an address", "example.com:80"},
	}
	for _, refusal := range refused {
		t.Run(refusal.name, func(t *testing.T) {
			t.Parallel()
			if err := safefetch.AllowAddress(refusal.address); err == nil {
				t.Errorf("%s was allowed", refusal.address)
			}
		})
	}

	allowed := []string{"93.184.216.34:80", "1.1.1.1:443", "[2606:4700:4700::1111]:443"}
	for _, address := range allowed {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			if err := safefetch.AllowAddress(address); err != nil {
				t.Errorf("%s was refused: %s", address, err)
			}
		})
	}
}

func TestOnlyFetchableAddressesAreParsed(t *testing.T) {
	t.Parallel()

	refused := map[string]string{
		"nothing":              "",
		"a local scheme":       "file:///etc/passwd",
		"a data url":           "data:image/png;base64,AAAA",
		"a scheme of its own":  "gopher://example.com/1",
		"no host":              "http:///a.png",
		"credentials in it":    "http://user:secret@example.com/a.png",
		"longer than anything": "http://example.com/" + string(make([]byte, 4096)),
	}
	for name, raw := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := safefetch.ParseTarget(raw); err == nil {
				t.Errorf("%q was accepted", raw)
			}
		})
	}

	for _, raw := range []string{
		"http://example.com/a.png",
		"https://example.com/a.png?width=2&height=3",
		"https://example.com:8443/a.png",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := safefetch.ParseTarget(raw); err != nil {
				t.Errorf("%q was refused: %s", raw, err)
			}
		})
	}
}

// A message links to what it links to; the proxy serves an image or nothing.
// Without this the endpoint would relay an internal service's reply to
// whoever wrote the message, which is the same hole one step later.

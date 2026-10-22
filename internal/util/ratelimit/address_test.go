package ratelimit_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/ratelimit"
)

func TestAddressKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		address  string
		expected string
	}{
		{"an IPv4 address is kept", "192.0.2.10", "192.0.2.10"},
		{"an IPv4 address written in IPv6 is the IPv4 address", "::ffff:192.0.2.10", "192.0.2.10"},
		{"an IPv6 address becomes its /64", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::/64"},
		{"another address in the same /64 has the same key", "2001:db8:1:2::1", "2001:db8:1:2::/64"},
		{"a zone is dropped", "fe80::1%eth0", "fe80::/64"},
		{"something that is not an address is kept", "not-an-address", "not-an-address"},
		{"nothing is kept", "", ""},
	}
	for _, testCase := range cases {
		if actual := ratelimit.AddressKey(testCase.address); actual != testCase.expected {
			t.Errorf("%s: AddressKey(%q) = %q, expected %q", testCase.name, testCase.address, actual, testCase.expected)
		}
	}
}

// A caller with a /64 must not get a burst for every address in it.
func TestAddressesOfOneNetworkShareABurst(t *testing.T) {
	t.Parallel()
	registry := ratelimit.NewRegistry(0.0001, 3, 1000, 0)

	allowed := 0
	for _, address := range []string{
		"2001:db8:1:2::1", "2001:db8:1:2::2", "2001:db8:1:2::3", "2001:db8:1:2::4", "2001:db8:1:2::5",
	} {
		if registry.Allow(ratelimit.AddressKey(address)) {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("%d attempts from one /64 were allowed, but the capacity is 3", allowed)
	}
}

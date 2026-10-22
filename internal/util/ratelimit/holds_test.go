package ratelimit_test

import (
	"net"
	"testing"

	"github.com/ziyan/teanode/internal/util/ratelimit"
)

func tcpAddress(t *testing.T, address string) net.Addr {
	t.Helper()
	return &net.TCPAddr{IP: net.ParseIP(address), Port: 40000}
}

// One address cannot take every slot: past its limit it is refused, and
// another address is not.
func TestAnAddressCannotHoldMoreThanItsLimit(t *testing.T) {
	t.Parallel()
	holds := ratelimit.NewAddressHolds(2)
	first := tcpAddress(t, "192.0.2.1")

	release, ok := holds.Acquire(first)
	if !ok {
		t.Fatal("the first hold was refused")
	}
	if _, ok := holds.Acquire(first); !ok {
		t.Fatal("the second hold was refused, but the limit is 2")
	}
	if _, ok := holds.Acquire(first); ok {
		t.Error("a third hold was given, but the limit is 2")
	}
	if _, ok := holds.Acquire(tcpAddress(t, "192.0.2.2")); !ok {
		t.Error("another address was refused because the first held its limit")
	}

	release()
	if _, ok := holds.Acquire(first); !ok {
		t.Error("a hold was refused after one was given back")
	}
}

// Every address of one IPv6 /64 is one holder, as it is for the rate limits.
func TestAddressesOfOneNetworkShareTheirHolds(t *testing.T) {
	t.Parallel()
	holds := ratelimit.NewAddressHolds(2)

	for _, address := range []string{"2001:db8:1:2::1", "2001:db8:1:2::2"} {
		if _, ok := holds.Acquire(tcpAddress(t, address)); !ok {
			t.Fatalf("%s was refused", address)
		}
	}
	if _, ok := holds.Acquire(tcpAddress(t, "2001:db8:1:2::3")); ok {
		t.Error("a third address of the same /64 was given a hold")
	}
	if _, ok := holds.Acquire(tcpAddress(t, "2001:db8:1:3::1")); !ok {
		t.Error("an address of another /64 was refused")
	}
}

package ratelimit

import "net/netip"

// AddressKey is the key a remote address is limited under: the address itself,
// or for IPv6 the /64 it is in.
//
// A /64 is what one network is usually given, so a caller holding one has more
// addresses than anybody could count, and a limiter keyed by the whole address
// hands each of them a burst of their own. An IPv4 address, and an IPv6 address
// that is only an IPv4 one written the long way (::ffff:192.0.2.1), is one
// caller and is kept as it is. Something that is not an address is returned
// unchanged, so a caller never has to check.
//
// This is for the limiter's key only. What is logged or shown as the address
// stays the whole address.
func AddressKey(address string) string {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return address
	}
	if parsed.Is4In6() {
		return parsed.Unmap().String()
	}
	if parsed.Is4() {
		return address
	}
	network, err := parsed.WithZone("").Prefix(64)
	if err != nil {
		return address
	}
	return network.String()
}

package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// RemoteAddress is who asked, as far as this server can honestly tell.
//
// Behind a CDN or a load balancer the connection comes from the proxy, so
// every audit row and every "last used from" recorded the proxy's address —
// one line of CloudFront's, over and over, saying nothing about who did
// anything. The client's address is in X-Forwarded-For, which the proxy adds.
//
// That header is also a plain request header, so anybody who can reach this
// server directly can write whatever they like in it. It is therefore read
// only when the connection itself came from an address the operator has said
// is a proxy, listed in server.trustedProxies. With nothing listed, the
// header is ignored and the connection's own address is used, which is the
// right answer for a server that faces the internet.
//
// The header is a list — client, then each proxy that forwarded it — and the
// entries a proxy did not add can be forged by the client. So it is read from
// the right, skipping the addresses that are themselves trusted proxies; the
// first one that is not is the closest address this server has any reason to
// believe. Everything to the left of it came from the client and is ignored.
func RemoteAddress(request *http.Request, trustedProxies []string) string {
	if request == nil {
		return ""
	}
	peer := hostOf(request.RemoteAddr)
	trusted := parsePrefixes(trustedProxies)
	if len(trusted) == 0 || !isTrusted(peer, trusted) {
		return peer
	}

	forwarded := request.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return peer
	}
	entries := strings.Split(forwarded, ",")
	for index := len(entries) - 1; index >= 0; index-- {
		candidate := hostOf(strings.TrimSpace(entries[index]))
		if candidate == "" {
			continue
		}
		if _, err := netip.ParseAddr(candidate); err != nil {
			// Not an address. A forged header can say anything, and this is
			// how it says it.
			continue
		}
		if !isTrusted(candidate, trusted) {
			return candidate
		}
	}
	// Every hop was a proxy of ours, which happens when the client's own
	// address is missing: the nearest thing to the truth is the connection.
	return peer
}

// hostOf drops the port, when there is one. An entry in X-Forwarded-For is
// usually a bare address, but CloudFront and some load balancers include the
// port, and an IPv6 address is bracketed when it does.
func hostOf(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

// parsePrefixes reads the configured list, which may name a single address or
// a range. Anything unparseable is left out rather than treated as a wildcard.
func parsePrefixes(values []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			prefixes = append(prefixes, prefix)
			continue
		}
		if address, err := netip.ParseAddr(value); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
		}
	}
	return prefixes
}

func isTrusted(host string, trusted []netip.Prefix) bool {
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	// An address that arrived as ::ffff:1.2.3.4 is the same address as
	// 1.2.3.4, and an operator writes the second.
	address = address.Unmap()
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

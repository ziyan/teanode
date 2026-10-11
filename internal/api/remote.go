package api

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
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

// IsSecure reports whether a request reached this server over HTTPS: on
// its own TLS listener, or through a proxy that terminated TLS and said
// so. The header is believed only from a proxy in server.trustedProxies,
// for the same reason X-Forwarded-For is: anyone who can reach the server
// directly can write it.
//
// The safe direction to be wrong in is towards "not secure", which makes
// a cookie more restrictive; the direction that matters is a proxy that
// terminates TLS and does not say so, which issues a cookie that a browser
// then sends in the clear. That is the operator's proxy to configure.
func IsSecure(request *http.Request, trustedProxies []string) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	trusted := parsePrefixes(trustedProxies)
	if len(trusted) == 0 || !isTrusted(hostOf(request.RemoteAddr), trusted) {
		return false
	}
	forwarded := request.Header.Get("X-Forwarded-Proto")
	if forwarded == "" {
		return false
	}
	// A chain of proxies appends, so the client's own protocol is first.
	if index := strings.Index(forwarded, ","); index >= 0 {
		forwarded = forwarded[:index]
	}
	return strings.EqualFold(strings.TrimSpace(forwarded), "https")
}

// IsLoopbackHost says whether a host never leaves this machine.
//
// Two callers with the same question and different reasons. A connected
// server declared at a loopback address is one an operator is running beside
// this one, where plain HTTP carries nothing anybody else can read. And a
// program being authorized here receives its approval on a loopback port,
// which cannot have a certificate, so that is the one place an http redirect
// address is allowed.
//
// One copy, because it is a security check in both places and two copies of a
// security check drift.
func IsLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	if address := net.ParseIP(host); address != nil {
		return address.IsLoopback()
	}
	return false
}

// IsLocalRequest says a request came from this machine and named it by its
// loopback address, with nothing in between: the server's own command line
// (client.Local), or a browser on the server itself. Plain HTTP is safe
// there, because nothing else is on the way.
//
// The Host header alone does not say so. It is whatever the client wrote,
// and a proxy that forwards plain HTTP may set it to 127.0.0.1, so the
// connection has to come from loopback as well. A proxy on this same machine
// passes both of those, so a request that carries a proxy's forwarding
// headers is not local either: whoever it is forwarding for is somewhere
// else.
func IsLocalRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	remote, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || !IsLoopbackHost(remote) {
		return false
	}
	host := request.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	if !IsLoopbackHost(host) {
		return false
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip"} {
		if request.Header.Get(header) != "" {
			return false
		}
	}
	return true
}

// IsPrivateUseRedirect says whether address is an app's own return address
// (RFC 8252 section 7.1): a scheme that is a reverse domain name, such as
// com.example.app:/oauth, with a path and no host.
//
// Only an app installed on a device can receive one: the device hands the
// address to whichever app claimed the scheme, never to a web page. That
// is what lets a sign-in sheet on a phone return to its app, where a
// self-hosted server's address could never have been claimed in advance.
// The dot is what keeps out every scheme a browser or an operating system
// already means something by (javascript:, file:, data:, mailto:).
func IsPrivateUseRedirect(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Opaque != "" || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !strings.Contains(scheme, ".") || strings.HasPrefix(scheme, ".") || strings.HasSuffix(scheme, ".") ||
		strings.Contains(scheme, "..") {
		return false
	}
	// One slash after the colon, as the specification writes it: two would
	// make what follows a host, and none would make it opaque.
	return strings.HasPrefix(address[len(parsed.Scheme):], ":/") && !strings.HasPrefix(address[len(parsed.Scheme):], "://") &&
		len(parsed.Path) > 1
}

// IsNativeAppClient says whether every address a program registered is an
// app's own return address: what a program must have registered as to be
// handed a connect code's token (internal/api/v1api/apioauth/connect.go).
func IsNativeAppClient(redirects []string) bool {
	if len(redirects) == 0 {
		return false
	}
	for _, redirect := range redirects {
		if !IsPrivateUseRedirect(redirect) {
			return false
		}
	}
	return true
}

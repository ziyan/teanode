package passwords

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NormalizedHost is the host of a site a login keeps or of a page the browser
// is on: lower case, without a leading "www.", a trailing dot, a port, a path
// or a user name. A site typed without a scheme ("example.com/sign-in") is
// read as if it had one. It is empty when there is no host or the scheme is
// not http or https. It follows the rule of PasswordsSiteMatch in TeaNodeKit,
// so the dashboard, the iPhone and the agent agree on what a site is.
func NormalizedHost(site string) string {
	return hostOf(site, true)
}

// hostOf is the host of a site, with its leading "www." dropped or kept.
func hostOf(site string, shouldDropWWW bool) string {
	text := strings.TrimSpace(site)
	if schemeEnd := strings.Index(text, "://"); schemeEnd >= 0 {
		scheme := strings.ToLower(text[:schemeEnd])
		if scheme != "http" && scheme != "https" {
			return ""
		}
		text = text[schemeEnd+len("://"):]
	} else {
		text = strings.TrimPrefix(text, "//")
	}
	if authorityEnd := strings.IndexAny(text, "/?#"); authorityEnd >= 0 {
		text = text[:authorityEnd]
	}
	// Anything before the last "@" is a user name and password, never the host.
	if atSign := strings.LastIndex(text, "@"); atSign >= 0 {
		text = text[atSign+1:]
	}
	if strings.HasPrefix(text, "[") {
		closing := strings.Index(text, "]")
		if closing < 0 {
			return ""
		}
		return strings.ToLower(text[:closing+1])
	}
	if colon := strings.LastIndex(text, ":"); colon >= 0 {
		port := text[colon+1:]
		for _, character := range port {
			if character < '0' || character > '9' {
				return ""
			}
		}
		text = text[:colon]
	}
	host := strings.ToLower(text)
	host = strings.TrimRight(host, ".")
	if shouldDropWWW && strings.HasPrefix(host, "www.") && strings.Contains(host[len("www."):], ".") {
		host = host[len("www."):]
	}
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		return ""
	}
	return host
}

// IsHostOfSites reports whether the page's host is the host of one of the
// sites: the same host and nothing looser. "example.com" and
// "www.example.com" are the same site; "login.example.com" and
// "example.com.evil.test" are not.
func IsHostOfSites(sites []string, page string) bool {
	pageHost := NormalizedHost(page)
	if pageHost == "" {
		return false
	}
	for _, site := range sites {
		if NormalizedHost(site) == pageHost {
			return true
		}
	}
	return false
}

// IsRelyingPartyOfHost reports whether a passkey for the relying party id may
// be used on a page at the host, by the WebAuthn rule: the id is the host
// itself or a domain above it that is not a public suffix (the registrable
// domain or a name between it and the host). A passkey for "example.com" is
// used on "login.example.com" but not on "example.com.evil.test", and no
// passkey is used on a host by way of a public suffix such as "com".
func IsRelyingPartyOfHost(relyingPartyID, page string) bool {
	relyingParty := strings.TrimRight(strings.ToLower(strings.TrimSpace(relyingPartyID)), ".")
	host := strings.TrimRight(hostOf(page, false), ".")
	if relyingParty == "" || host == "" {
		return false
	}
	if relyingParty == host {
		return true
	}
	if !strings.HasSuffix(host, "."+relyingParty) {
		return false
	}
	// An address has no registrable domain to share.
	if strings.HasPrefix(host, "[") || isAddress(host) {
		return false
	}
	// The relying party must be at or below the registrable domain.
	registrable, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return false
	}
	return relyingParty == registrable || strings.HasSuffix(relyingParty, "."+registrable)
}

// isAddress reports whether the host is an IPv4 address.
func isAddress(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

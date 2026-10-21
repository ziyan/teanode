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
	if strings.IndexFunc(text, isBackslashOrControl) >= 0 {
		return ""
	}
	if scheme, rest, hasScheme := splitScheme(text); hasScheme {
		if strings.ToLower(scheme) != "http" && strings.ToLower(scheme) != "https" {
			return ""
		}
		text = rest
	} else if strings.HasPrefix(text, "//") {
		text = text[len("//"):]
	} else if isSchemeWithoutSlashes(text) {
		// "javascript:", "mailto:", "about:blank": not a host and a port.
		return ""
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

// splitScheme splits "scheme://rest" when the text starts with a scheme
// followed by two slashes. A "://" further on, in a path or a query, is not a
// scheme.
func splitScheme(text string) (scheme, rest string, hasScheme bool) {
	schemeEnd := strings.Index(text, "://")
	if schemeEnd <= 0 || !isSchemeToken(text[:schemeEnd]) {
		return "", text, false
	}
	return text[:schemeEnd], text[schemeEnd+len("://"):], true
}

// isSchemeToken is true for a letter followed by letters, digits, "+", "-"
// and ".".
func isSchemeToken(text string) bool {
	for index := 0; index < len(text); index++ {
		character := text[index]
		isLetter := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
		isOther := (character >= '0' && character <= '9') || character == '+' || character == '-' || character == '.'
		if !isLetter && (index == 0 || !isOther) {
			return false
		}
	}
	return text != ""
}

// isSchemeWithoutSlashes is true for a scheme and a colon that is not a host
// and a port: "mailto:ada@example.com" is, "localhost:8080/sign-in" is not.
func isSchemeWithoutSlashes(text string) bool {
	colon := strings.Index(text, ":")
	if colon <= 0 || !isSchemeToken(text[:colon]) {
		return false
	}
	rest := text[colon+1:]
	digitsEnd := 0
	for digitsEnd < len(rest) && rest[digitsEnd] >= '0' && rest[digitsEnd] <= '9' {
		digitsEnd++
	}
	if digitsEnd > 0 && (digitsEnd == len(rest) || strings.IndexByte("/?#", rest[digitsEnd]) >= 0) {
		return false
	}
	return true
}

// isBackslashOrControl is true for a backslash and for an ASCII control
// character, including a delete.
func isBackslashOrControl(character rune) bool {
	return character == '\\' || character < 0x20 || character == 0x7f
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

// IsOriginOfSites reports whether the page is at the origin of one of the
// sites, which is stricter than IsHostOfSites and is what the agent's sign-in
// uses before it puts a secret into a page: the host is the same, and so is
// the port. A site that names a port ("example.com:8443") matches only a page
// on that port; a site that names none matches only a page on the default
// port of the site's scheme (443 for a site saved without a scheme or as
// https). A page on http matches only a site saved as http, so a password
// saved for a secure site is never typed into its plain-text twin.
func IsOriginOfSites(sites []string, page string) bool {
	pageHost := NormalizedHost(page)
	if pageHost == "" {
		return false
	}
	pageScheme, pagePort := schemeOf(page), EffectivePort(page)
	for _, site := range sites {
		if NormalizedHost(site) != pageHost {
			continue
		}
		siteScheme := schemeOf(site)
		if pageScheme == "http" && siteScheme != "http" {
			continue
		}
		sitePort, isNamed := explicitPortOf(site)
		if !isNamed {
			sitePort = defaultPortOf(siteScheme)
		}
		// A port that could not be read is zero on both sides and is not a
		// match for anything, itself included.
		if sitePort > 0 && sitePort == pagePort {
			return true
		}
	}
	return false
}

// EffectivePort is the port a page is on: the one its address names, or the
// default of its scheme. It is zero when the address names an unreadable port.
func EffectivePort(page string) int {
	if port, isNamed := explicitPortOf(page); isNamed {
		return port
	}
	return defaultPortOf(schemeOf(page))
}

// schemeOf is "http" when the address says so and "https" otherwise, which is
// what a site typed without a scheme means.
func schemeOf(site string) string {
	if scheme, _, hasScheme := splitScheme(strings.TrimSpace(site)); hasScheme && strings.EqualFold(scheme, "http") {
		return "http"
	}
	return "https"
}

func defaultPortOf(scheme string) int {
	if scheme == "http" {
		return 80
	}
	return 443
}

// explicitPortOf is the port an address names, and whether it names one.
// A port that is not a number from 1 to 65535 is named, and is zero, which
// matches nothing.
func explicitPortOf(site string) (int, bool) {
	text := strings.TrimSpace(site)
	if _, rest, hasScheme := splitScheme(text); hasScheme {
		text = rest
	} else {
		text = strings.TrimPrefix(text, "//")
	}
	if authorityEnd := strings.IndexAny(text, "/?#"); authorityEnd >= 0 {
		text = text[:authorityEnd]
	}
	if atSign := strings.LastIndex(text, "@"); atSign >= 0 {
		text = text[atSign+1:]
	}
	if strings.HasPrefix(text, "[") {
		closing := strings.Index(text, "]")
		if closing < 0 || !strings.HasPrefix(text[closing+1:], ":") {
			return 0, false
		}
		text = text[closing+1:]
	}
	colon := strings.LastIndex(text, ":")
	if colon < 0 || colon+1 == len(text) {
		return 0, false
	}
	port := 0
	for _, character := range text[colon+1:] {
		if character < '0' || character > '9' || port > 65535 {
			return 0, true
		}
		port = port*10 + int(character-'0')
	}
	if port < 1 || port > 65535 {
		return 0, true
	}
	return port, true
}

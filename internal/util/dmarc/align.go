package dmarc

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// IsAligned says whether a domain that authenticated a message, the SPF
// sender domain or a DKIM signature's d=, is aligned with the From domain in
// a mode, as RFC 7489 section 3.1 defines it. Strict needs the same name.
// Relaxed needs the same organizational domain, which takes in a signature by
// the From domain's parent or by a sibling of it as well as one by a
// subdomain: mail from news.example.com signed by mail.example.com, or from
// alerts.example.com signed by example.com, is aligned. Checking only for a
// subdomain of the From domain failed both, and under a reject policy that
// refused mail every other receiver accepts.
func IsAligned(authenticatedDomain, fromDomain string, mode AlignmentMode) bool {
	authenticatedDomain = normalizedDomain(authenticatedDomain)
	fromDomain = normalizedDomain(fromDomain)
	if authenticatedDomain == "" || fromDomain == "" {
		return false
	}
	if authenticatedDomain == fromDomain {
		return true
	}
	if mode != AlignmentRelaxed {
		return false
	}
	return OrganizationalDomain(authenticatedDomain) == OrganizationalDomain(fromDomain)
}

// OrganizationalDomain is the registered domain a name falls under, one label
// below its public suffix, RFC 7489 section 3.2. A name the suffix list cannot
// place is its own organizational domain.
func OrganizationalDomain(domain string) string {
	domain = normalizedDomain(domain)
	organizational, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return domain
	}
	return organizational
}

func normalizedDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

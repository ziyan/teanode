package dmarc

import (
	"context"

	"golang.org/x/net/publicsuffix"
)

// Discovery is where a policy was found and what it means for the message
// that was checked.
type Discovery struct {
	// Record is the policy, or nil when the sender publishes none.
	Record *Record

	// Domain is the name the record was found at, which is the author's
	// domain or the organizational domain above it.
	Domain string

	// Organizational is true when the record came from the organizational
	// domain rather than the author's own — the case where the subdomain
	// policy applies instead of the domain policy.
	Organizational bool
}

// Policy is the one that applies to the message, which is not always the "p"
// tag: a record found at the organizational domain governs its subdomains
// through "sp", and only falls back to "p" when the record does not set one.
func (self *Discovery) Policy() Policy {
	if self == nil || self.Record == nil {
		return PolicyNone
	}
	if self.Organizational && self.Record.SubdomainPolicy != "" {
		return self.Record.SubdomainPolicy
	}
	return self.Record.Policy
}

// Discover finds the policy that governs a domain, as RFC 7489 section 6.6.3
// describes it: ask the domain itself, and if it publishes nothing, ask the
// organizational domain above it.
//
// The second question is the one that was missing, and it is not an edge case.
// Bulk senders send from a subdomain as a matter of course —
// rs.email.example.com, notifications.example.net — and almost none of them
// publish a record there, because they do not have to: the policy at the
// organizational domain covers them through "sp". Asking only the exact name
// answered "this sender has no policy" for a very large share of real mail,
// including mail whose organizational domain says "p=reject". Every receiver
// worth passing gets this right, so the disagreement showed up as our own
// header saying dmarc=none beside a major provider's saying dmarc=pass on the
// same message.
//
// A lookup failure at the organizational domain is not fatal: the answer for
// the domain itself has already been obtained, and reporting no policy is
// better than refusing to evaluate the message at all.
func Discover(ctx context.Context, domain string, options *LookupOptions) (*Discovery, error) {
	record, err := Lookup(ctx, domain, options)
	if err != nil {
		return nil, err
	}
	if record != nil {
		return &Discovery{Record: record, Domain: domain}, nil
	}

	organizational, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil || organizational == domain {
		return &Discovery{Domain: domain}, nil
	}

	record, err = Lookup(ctx, organizational, options)
	if err != nil || record == nil {
		return &Discovery{Domain: domain}, nil
	}
	return &Discovery{Record: record, Domain: organizational, Organizational: true}, nil
}

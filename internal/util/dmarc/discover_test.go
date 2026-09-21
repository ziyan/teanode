package dmarc_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/ziyan/teanode/internal/util/dmarc"
)

// zone answers TXT lookups from a fixed map, and reports anything else as not
// found — which is what a resolver does for a name that does not exist, and
// what the discovery has to treat as "ask the domain above".
type zone map[string][]string

func (self zone) LookupTXT(_ context.Context, name string) ([]string, error) {
	if records, ok := self[name]; ok {
		return records, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// The case found in real mail: a large sender sends from a subdomain such as
// rs.email.example.com, publishes no record there, and is governed by the
// p=reject at example.com. Asking only the exact name answered "no policy", so
// this server reported dmarc=none on a message every other receiver reported
// as dmarc=pass.
func TestAPolicyIsInheritedFromTheOrganizationalDomain(t *testing.T) {
	t.Parallel()

	records := zone{"_dmarc.example.com": {"v=DMARC1; p=reject; sp=quarantine"}}

	found, err := dmarc.Discover(context.Background(), "rs.email.example.com", &dmarc.LookupOptions{Resolver: records})
	if err != nil {
		t.Fatalf("Discover: %s", err)
	}
	if found.Record == nil {
		t.Fatal("no policy found; the domain above the sender publishes one")
	}
	if found.Domain != "example.com" || !found.Organizational {
		t.Errorf("the policy was found at %q (organizational: %v), want example.com", found.Domain, found.Organizational)
	}
	// The subdomain policy is the one that governs a subdomain. Reporting "p"
	// would overstate a policy that deliberately treats subdomains gently.
	if found.Policy() != dmarc.PolicyQuarantine {
		t.Errorf("the policy that applies is %q, want the subdomain policy", found.Policy())
	}
}

// Without sp, the domain policy covers subdomains too.
func TestTheDomainPolicyAppliesWhenThereIsNoSubdomainPolicy(t *testing.T) {
	t.Parallel()

	records := zone{"_dmarc.example.com": {"v=DMARC1; p=reject"}}
	found, err := dmarc.Discover(context.Background(), "mail.example.com", &dmarc.LookupOptions{Resolver: records})
	if err != nil {
		t.Fatalf("Discover: %s", err)
	}
	if found.Policy() != dmarc.PolicyReject {
		t.Errorf("the policy that applies is %q, want reject", found.Policy())
	}
}

// A sender's own record wins, and is not a subdomain policy even when the
// domain above publishes something stricter.
func TestASendersOwnRecordWins(t *testing.T) {
	t.Parallel()

	records := zone{
		"_dmarc.mail.example.com": {"v=DMARC1; p=none"},
		"_dmarc.example.com":      {"v=DMARC1; p=reject"},
	}
	found, err := dmarc.Discover(context.Background(), "mail.example.com", &dmarc.LookupOptions{Resolver: records})
	if err != nil {
		t.Fatalf("Discover: %s", err)
	}
	if found.Organizational {
		t.Error("the sender's own record was reported as inherited")
	}
	if found.Policy() != dmarc.PolicyNone {
		t.Errorf("the policy that applies is %q, want the sender's own", found.Policy())
	}
}

// Nobody publishing anything is still no policy, and must not be reported as
// one belonging to a public suffix.
func TestNoPolicyAnywhere(t *testing.T) {
	t.Parallel()

	found, err := dmarc.Discover(context.Background(), "mail.example.com", &dmarc.LookupOptions{Resolver: zone{}})
	if err != nil {
		t.Fatalf("Discover: %s", err)
	}
	if found.Record != nil {
		t.Error("a policy was found where none is published")
	}
	if found.Policy() != dmarc.PolicyNone {
		t.Errorf("the policy is %q, want none", found.Policy())
	}
}

// Nothing inherits from a public suffix. A record at "_dmarc.com" governs
// nobody, and treating it as the policy for every .com domain would apply a
// stranger's rules to the whole registry — so the walk up stops at the
// organizational domain and does not go past it.
func TestNothingInheritsFromAPublicSuffix(t *testing.T) {
	t.Parallel()

	records := zone{"_dmarc.com": {"v=DMARC1; p=reject"}}
	for _, domain := range []string{"example.com", "mail.example.com"} {
		found, err := dmarc.Discover(context.Background(), domain, &dmarc.LookupOptions{Resolver: records})
		if err != nil {
			t.Fatalf("Discover(%q): %s", domain, err)
		}
		if found.Record != nil {
			t.Errorf("%q inherited a policy from a public suffix", domain)
		}
	}
}

// A resolver that fails on the sender's own name is a real error and has to be
// reported, rather than quietly becoming "no policy".
func TestALookupFailureIsReported(t *testing.T) {
	t.Parallel()

	failing := failingResolver{}
	if _, err := dmarc.Discover(context.Background(), "mail.example.com", &dmarc.LookupOptions{Resolver: failing}); err == nil {
		t.Error("a resolver failure was swallowed")
	}
}

type failingResolver struct{}

func (failingResolver) LookupTXT(context.Context, string) ([]string, error) {
	return nil, errors.New("resolver is down")
}

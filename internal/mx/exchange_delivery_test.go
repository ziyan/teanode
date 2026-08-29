package mx

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// TestSigningDomainIsWhereTheKeyIsPublished pins the d= value used for the ARC
// seal on forwarded mail.
//
// It used to be Hostname(), so a seal said d=mail.example.com while the key
// was published at teanode1._domainkey.example.com. Every receiver that
// checked a forwarded message looked up a name with nothing under it and could
// not verify the seal — which is the entire purpose of sealing. Nothing in the
// server could notice, because only the receiver ever verifies.
func TestSigningDomainIsWhereTheKeyIsPublished(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain *config.Domain
		want   string
	}{
		{
			name:   "a domain with a mail host still signs as the domain",
			domain: &config.Domain{Domain: "example.com", Subdomain: "mail"},
			want:   "example.com",
		},
		{
			name:   "a domain with no subdomain signs as itself",
			domain: &config.Domain{Domain: "example.com"},
			want:   "example.com",
		},
		{
			name:   "a deleted domain signs as nothing rather than panicking",
			domain: nil,
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := signingDomain(test.domain); got != test.want {
				t.Errorf("signingDomain() = %q, want %q", got, test.want)
			}
		})
	}

	// The point of the whole thing: the name a receiver resolves has to be the
	// one the operator was told to publish.
	domain := &config.Domain{Domain: "example.com", Subdomain: "mail", DKIM: config.DomainKey{Selector: "teanode1"}}
	resolved := config.DomainKeyName(domain.DKIM.Selector, signingDomain(domain))
	published := config.DomainKeyName(domain.DKIM.Selector, domain.Domain)
	if resolved != published {
		t.Errorf("a receiver would look up %q, but the key is published at %q", resolved, published)
	}
}

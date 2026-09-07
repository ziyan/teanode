package config_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// findDomain is the one named, out of a test's list.
func findDomain(domains []*models.Domain, name string) *models.Domain {
	for _, domain := range domains {
		if domain.Domain == name {
			return domain
		}
	}
	return nil
}

// Which name a domain's mail arrives at, and so which name its MX records
// point at, its certificate is issued for, and its headers report.
//
// The domain the server is named under keeps the server's own name. Every
// other domain gets one of its own: telling them all to point at one name
// publishes, in each of them, the name of a different one — look up the MX of
// any and you have the set.
func TestMailHostsFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		serverName string
		domain     string
		want       string
	}{
		// The host lives under primary.test, so that domain uses it directly.
		{"mail.primary.test", "primary.test", "mail.primary.test"},
		{"mail.primary.test", "second.test", "mx.second.test"},
		// The longest match wins: a.example.com owns it, not example.com.
		{"mail.a.example.com", "a.example.com", "mail.a.example.com"},
		{"mail.a.example.com", "example.com", "mx.example.com"},
		// The name is a configured domain itself.
		{"primary.test", "primary.test", "primary.test"},
		// Nobody owns it — the server is named under a domain it does not
		// serve — so every domain is treated as the owner, which is better
		// than nobody being shown the records for it.
		{"mail.elsewhere.test", "primary.test", "mail.elsewhere.test"},
		{"mail.elsewhere.test", "second.test", "mail.elsewhere.test"},
		// A near miss is not a match: notprimary.test does not end in
		// ".primary.test", so nobody owns that name either.
		{"mail.notprimary.test", "primary.test", "mail.notprimary.test"},
	}

	for _, test := range tests {
		t.Run(test.serverName+"/"+test.domain, func(t *testing.T) {
			configuration := config.Default()
			configuration.Server.Name = test.serverName
			domains := []*models.Domain{
				{Domain: "primary.test"},
				{Domain: "second.test"},
				{Domain: "a.example.com"},
				{Domain: "example.com"},
			}
			domain := findDomain(domains, test.domain)
			if domain == nil {
				t.Fatalf("%q is not in the test configuration", test.domain)
			}
			if got := configuration.MailHostFor(domain, domains); got != test.want {
				t.Errorf("MailHostFor(%q) with server %q = %q, want %q",
					test.domain, test.serverName, got, test.want)
			}
		})
	}
}

// A server reached on several names gives them all to the domain it is named
// under, because they are already in that zone — and gives every other domain
// exactly one, because a second name for the same host is a record to publish
// and a certificate name to renew for nothing.
func TestAPairOfServerNamesBecomesOneNameElsewhere(t *testing.T) {
	t.Parallel()

	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Server.MailServers = []string{"mx1.primary.test", "mx2.primary.test"}
	domains := []*models.Domain{{Domain: "primary.test"}, {Domain: "other.test"}}

	owner := configuration.MailHostsFor(findDomain(domains, "primary.test"), domains)
	if strings.Join(owner, ",") != "mx1.primary.test,mx2.primary.test" {
		t.Errorf("the domain the server is named under got %v, want both of the server's names", owner)
	}

	other := configuration.MailHostsFor(findDomain(domains, "other.test"), domains)
	if strings.Join(other, ",") != "mx.other.test" {
		t.Errorf("another domain got %v, want the single mx.other.test", other)
	}
}

// The names are the operator's choice, not a derivation. Somebody who has
// always published a pair keeps publishing a pair; somebody who wants one
// domain to point at the server's own name can say so.
func TestConfiguredMailServersWin(t *testing.T) {
	t.Parallel()

	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	domains := []*models.Domain{
		{Domain: "primary.test"},
		{Domain: "pair.test", MailServers: []string{"mx1.pair.test", "mx2.pair.test"}},
		{Domain: "pointed.test", MailServers: []string{"mail.primary.test"}},
		// Tidied on the way through, so a list typed with a trailing comma
		// and a stray capital means what it looks like.
		{Domain: "spaced.test", MailServers: []string{" MX.Spaced.Test. ", ""}},
		// Nothing said, so the default applies.
		{Domain: "default.test"},
	}

	for _, test := range []struct{ domain, want string }{
		{"pair.test", "mx1.pair.test,mx2.pair.test"},
		{"pointed.test", "mail.primary.test"},
		{"spaced.test", "mx.spaced.test"},
		{"default.test", "mx.default.test"},
	} {
		t.Run(test.domain, func(t *testing.T) {
			got := configuration.MailHostsFor(findDomain(domains, test.domain), domains)
			if strings.Join(got, ",") != test.want {
				t.Errorf("MailHostsFor(%q) = %v, want %s", test.domain, got, test.want)
			}
		})
	}
}

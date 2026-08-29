package dns

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// deadVerifier resolves nothing, on purpose: these tests are about what the
// server asks an operator to publish, not about what is published.
func deadVerifier(t *testing.T, configuration *config.Configuration) *verifier {
	t.Helper()
	verifier := &verifier{
		config:   config.NewMemoryStore(configuration),
		settings: &Settings{Nameserver: "127.0.0.1:1"},
		client:   new(dns.Client),
		status:   make(map[string]*RecordSet),
	}
	verifier.client.Timeout = 50 * time.Millisecond
	return verifier
}

func manyDomains() *config.Configuration {
	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Server.MailServers = []string{"mx1.primary.test", "mx2.primary.test"}
	configuration.Server.Secret = "a-secret-long-enough-to-sign-with"
	configuration.Domains = []*config.Domain{
		{ID: "primary.test", Domain: "primary.test", Subdomain: "mail"},
		{ID: "other.test", Domain: "other.test", Subdomain: "mail"},
	}
	return configuration
}

// A domain that is not the one the server is named under publishes its own
// names for the same host. Pointing it at mx1.primary.test works, and tells
// anybody who looks up its MX which other domains it sits beside.
func TestADomainPublishesItsOwnMailServerNames(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	other := configuration.FindDomain("other.test")
	set := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, other)

	var mx, addresses []string
	for _, record := range set.Records {
		switch {
		case record.Type == "MX" && record.Name == "other.test.":
			mx = append(mx, record.Expected)
		case record.Type == "A":
			addresses = append(addresses, record.Name)
		}
	}

	// One name, not one per name the server answers on. Both would resolve to
	// the same host, so a second is a record to publish and a certificate name
	// to renew, for nothing.
	if strings.Join(mx, ",") != "mx.other.test." {
		t.Errorf("the MX records name %v, want the single mx.other.test.", mx)
	}
	// And the name it now points at is this domain's to create, so it has to
	// be on the page.
	if strings.Join(addresses, ",") != "mx.other.test." {
		t.Errorf("address records are for %v, want this domain's own mail server", addresses)
	}

	// Nothing anywhere in the set may name the server's own domain, which is
	// the whole point.
	for _, record := range set.Records {
		if strings.Contains(record.Expected, "primary.test") {
			t.Errorf("the %s record at %s still names the server's domain: %q",
				record.Type, record.Name, record.Expected)
		}
	}
}

// The domain the server is named under keeps the server's names: a second name
// for the same host, in the same zone, buys nothing.
func TestTheDomainThatOwnsTheServerNameKeepsIt(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	primary := configuration.FindDomain("primary.test")
	set := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, primary)

	var mx []string
	for _, record := range set.Records {
		if record.Type == "MX" && record.Name == "primary.test." {
			mx = append(mx, record.Expected)
		}
	}
	want := "mx1.primary.test.,mx2.primary.test."
	if strings.Join(mx, ",") != want {
		t.Errorf("the MX records name %v, want %s", mx, want)
	}
}

// The bounce and report name gets an MX of its own rather than an alias to the
// server. That was the last record telling every domain to name a different
// one.
func TestTheBounceNameGetsItsOwnMailExchanger(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	other := configuration.FindDomain("other.test")
	set := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, other)

	// The MX rows only. An SPF record lives at this name too, and it is a TXT.
	var found []*Record
	for _, record := range set.Records {
		if record.Name == "mail.other.test." && record.Type != "TXT" {
			found = append(found, record)
		}
	}
	if len(found) == 0 {
		t.Fatal("nothing is asked for at the bounce name")
	}
	for _, record := range found {
		if record.Type == "CNAME" {
			t.Errorf("the bounce name is still an alias to %q", record.Expected)
		}
		if record.Type != "MX" {
			t.Errorf("the bounce name has a %s record, want MX", record.Type)
		}
		if !strings.HasSuffix(record.Expected, ".other.test.") {
			t.Errorf("the bounce name points at %q, which is not this domain's", record.Expected)
		}
	}
}

// An installation that already points its MX at the server's names is correct
// and must not be told otherwise. A lookup on the older shape finds those
// names, and this is what decides whether they count.
func TestTheOlderShapeStillCounts(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	hosts := mailHostsFor(configuration, configuration.FindDomain("other.test"))
	if len(hosts) != 1 {
		t.Fatalf("got %d mail hosts, want 1", len(hosts))
	}

	// The name asked for now, the same name spelled differently, and both of
	// the server's own names, which is what an installation set up before this
	// still points at.
	for _, published := range []string{
		"mx.other.test.", "MX.Other.Test.", "mx1.primary.test.", "mx2.primary.test.",
	} {
		if !hosts[0].namesThisServer(published) {
			t.Errorf("%q should count as reaching this server", published)
		}
	}
	for _, published := range []string{"mx.example.com.", "mx1.other.test.", ""} {
		if hosts[0].namesThisServer(published) {
			t.Errorf("%q should not count as reaching this server", published)
		}
	}
}

// The DMARC record has to name an address this server accepts. A recipient
// starting "rua-" is checked against the server secret; anything else at that
// name is unknown and refused, so the plain "rua@" this used to ask for was an
// address that silently threw every report away.
func TestTheReportAddressIsOneTheServerAccepts(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	other := configuration.FindDomain("other.test")
	set := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, other)

	var dmarc *Record
	for _, record := range set.Records {
		if record.Name == "_dmarc.other.test." {
			dmarc = record
		}
	}
	if dmarc == nil {
		t.Fatal("no DMARC record is asked for")
	}

	_, address, found := strings.Cut(dmarc.Expected, "rua=mailto:")
	if !found {
		t.Fatalf("the DMARC record names no report address: %q", dmarc.Expected)
	}
	if !strings.HasSuffix(address, "@mail.other.test") {
		t.Errorf("reports go to %q, which is not this domain's own name", address)
	}

	prefix, id, err := mailparse.ValidateAddress(address, configuration.Secret())
	if err != nil {
		t.Fatalf("the server would refuse the address it asks for: %s", err)
	}
	if prefix != "rua" {
		t.Errorf("the address validates as %q, want rua", prefix)
	}

	// Stable, or every check would ask for a DNS record that differs from the
	// one published last time.
	again := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, other)
	for _, record := range again.Records {
		if record.Name == "_dmarc.other.test." && record.Expected != dmarc.Expected {
			t.Errorf("the report address changed between two checks:\n  %s\n  %s", dmarc.Expected, record.Expected)
		}
	}
	if id == "" {
		t.Error("the address carries no identifier")
	}
}

// TestAuthorisesSending is the check behind the SPF row.
//
// It cannot decide whether a record permits this particular server — an
// include is a question for a resolver, and with a proxy or a relay the
// address mail leaves from is not one this server knows. It decides the two
// things it can: whether there is a record, and whether it permits anything
// at all.
func TestAuthorisesSending(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"v=spf1 ip4:203.0.113.9 -all":                  true,
		"v=spf1 ip6:2001:db8::1 -all":                  true,
		"v=spf1 include:example.net -all":              true,
		"v=spf1 a mx -all":                             true,
		"V=SPF1 IP4:203.0.113.9 -ALL":                  true,
		"v=spf1 ip4:203.0.113.9 ip4:203.0.113.10 ~all": true,

		// A blanket refusal. The right record for a domain that sends nothing,
		// and the wrong one for the name that is the return path of everything
		// this server sends — and it is what more than one registrar publishes
		// by default, so it turns up without anybody choosing it.
		"v=spf1 -all":                  false,
		"v=spf1 ~all":                  false,
		"v=spf1 ?all":                  false,
		"v=spf1 +all":                  false,
		"v=spf1":                       false,
		"v=spf1 -ip4:203.0.113.9 -all": false,

		// Not an SPF record at all.
		"v=DKIM1; k=rsa; p=MIIB":           false,
		"v=spf2.0/pra include:example.net": false,
		"":                                 false,
	}
	for record, want := range tests {
		if got := authorisesSending(record); got != want {
			t.Errorf("authorisesSending(%q) = %v, want %v", record, got, want)
		}
	}
}

// The row itself, and the regression that made it necessary: the SPF record
// lived on an alias, the alias was removed, and nothing anywhere reported that
// the sending address had stopped being authorised.
func TestTheReturnPathIsAskedForAnSPFRecord(t *testing.T) {
	t.Parallel()

	configuration := manyDomains()
	other := configuration.FindDomain("other.test")
	set := deadVerifier(t, configuration).resolveDomainRecords(context.Background(), configuration, other)

	var spf *Record
	for _, record := range set.Records {
		if record.Type == "TXT" && record.Name == "mail.other.test." {
			spf = record
		}
	}
	if spf == nil {
		t.Fatal("nothing asks for an SPF record at the return path")
	}
	if !strings.HasPrefix(spf.Expected, "v=spf1 ") || !strings.HasSuffix(spf.Expected, " -all") {
		t.Errorf("the SPF record asked for is %q", spf.Expected)
	}
	// Nothing is published in this test, so it must not be satisfied.
	if spf.Verified {
		t.Error("the SPF row verified with nothing published")
	}
	if spf.Optional {
		t.Error("the SPF row is optional; unauthenticated outgoing mail is not optional")
	}

	// And nothing is asked for at the domain itself, where other senders'
	// records live.
	for _, record := range set.Records {
		if record.Type == "TXT" && record.Name == "other.test." {
			t.Errorf("an SPF record is asked for at the apex: %q", record.Expected)
		}
	}
}

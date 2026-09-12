package dns

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// TestPublishesDKIMKey guards a mistake that reported a domain's DKIM as
// correctly published when it was in fact revoked. example.com really does
// publish "v=DKIM1; p=" at every _domainkey name, which is how this was found.
func TestPublishesDKIMKey(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"v=DKIM1; k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQ": true,
		"v=DKIM1;p=MIGfMA0GCSqGSIb3DQEB":                       true,

		// RFC 6376 section 3.6.1 makes v= RECOMMENDED, defaulting to DKIM1, so
		// a record without it is a key record and verifiers accept it. This is
		// the shape a real deployment was publishing when the dashboard told
		// its operator to change DNS that was already correct.
		"k=rsa; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQ": true,
		"p=MIGfMA0GCSqGSIb3DQEB":                      true,

		"v=DKIM1; p=":     false, // revoked
		"v=DKIM1; p= ":    false, // revoked, padded
		"v=DKIM1; k=rsa":  false, // no key at all
		"k=rsa":           false, // no key, and no version either
		"v=spf1 -all":     false, // not a DKIM record
		"v=DKIM2; p=MIGf": false, // some future thing this does not understand
		"":                false,
	}
	for record, want := range tests {
		if got := publishesDkimKey(record); got != want {
			t.Errorf("publishesDkimKey(%q) = %v, want %v", record, got, want)
		}
	}
}

// TestEveryDomainPublishesItsOwnKey is the shape after the shared-key CNAME
// was removed.
//
// Two domains signing with the same key are each told to publish that key at
// their own name, rather than one of them being told to alias the other. The
// alias was a convenience for rotation and cost a concept — a "primary"
// domain — that had to be chosen, explained, and got wrong.
//
// Nothing breaks for a deployment that already published those aliases: a TXT
// lookup follows a CNAME, so the record found at the alias is the key this
// expects, and the verification below the record list accepts it.
//
// The nameserver here answers nothing, which is deliberate: this is about what
// the server asks an operator to publish, not about what is published.
func TestEveryDomainPublishesItsOwnKey(t *testing.T) {
	t.Parallel()

	shared, err := models.GenerateDomainKey("teanode1")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	expected, err := shared.PublicKeyRecord()
	if err != nil {
		t.Fatalf("PublicKeyRecord: %s", err)
	}

	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	domains := []*models.Domain{
		{ID: "primary.test", Domain: "primary.test", DKIM: shared},
		{ID: "same.test", Domain: "same.test", DKIM: shared},
	}

	// An address nothing listens on, so every lookup fails and the record set
	// comes back with what to publish and nothing found.
	verifier := &verifier{
		config:   config.NewMemoryStore(configuration),
		settings: &Settings{Nameserver: "127.0.0.1:1"},
		client:   new(dns.Client),
		status:   make(map[string]*RecordSet),
	}
	verifier.client.Timeout = 50 * time.Millisecond

	for _, domain := range domains {
		t.Run(domain.Domain, func(t *testing.T) {
			set := verifier.resolveDomainRecords(context.Background(), configuration, domain, domains)
			name := dnsName(models.DomainKeyName("teanode1", domain.Domain))

			var found *Record
			for _, record := range set.Records {
				if record.Name == name {
					found = record
				}
			}
			if found == nil {
				t.Fatalf("no record at %q", name)
			}
			if found.Type != "TXT" {
				t.Errorf("record at %q is a %s, want TXT — the CNAME is gone", name, found.Type)
			}
			if found.Expected != expected {
				t.Errorf("record at %q carries %q, want this domain's own key", name, found.Expected)
			}
		})
	}
}

// The advisory that lets a phone find this server from a mail address alone.
func TestTheContactsServiceRecordIsAdvisedWhenThereIsAPortToAdvise(t *testing.T) {
	checker := &verifier{}
	configuration := config.Default()
	configuration.Server.Name = "mail.example.com"
	configuration.Listen.HTTPS = ":10443"
	domain := &models.Domain{Domain: "example.com"}

	ours := []mailHost{{Name: "mx.example.com."}}
	record := checker.checkContactsService(context.Background(), configuration, domain, ours)
	if record == nil {
		t.Fatal("a server with an HTTPS listener has a record to advise")
	}
	if record.Type != "SRV" || record.Name != "_carddavs._tcp.example.com" {
		t.Fatalf("the record: %+v", record)
	}
	// This domain's own mail host, not the server's name: every row on a
	// domain's page has to be something its reader can publish.
	if record.Expected != "0 1 10443 mx.example.com" {
		t.Fatalf("names this domain's own host and the port: %q", record.Expected)
	}
	if !record.Optional {
		t.Error("nothing breaks without it, so it is optional")
	}

	// A domain whose mail is addressed to somebody else's name has nothing
	// it could publish, so it is advised nothing.
	theirs := []mailHost{{Name: "mail.someone-else.test."}}
	if record := checker.checkContactsService(context.Background(), configuration, domain, theirs); record != nil {
		t.Errorf("a host outside this domain is not this domain's to publish: %+v", record)
	}

	// With TLS ended somewhere in front there is no port this server can
	// honestly name, so it advises nothing rather than a wrong number.
	configuration.Listen.HTTPS = ""
	if record := checker.checkContactsService(context.Background(), configuration, domain, ours); record != nil {
		t.Errorf("with no HTTPS listener there is no honest port to advise: %+v", record)
	}
}

func TestAListenAddressYieldsItsPort(t *testing.T) {
	for address, want := range map[string]int{
		":10443": 10443, "127.0.0.1:443": 443, "[::]:8443": 8443,
		"": 0, "nonsense": 0, ":0": 0, ":70000": 0,
	} {
		if got := portOf(address); got != want {
			t.Errorf("portOf(%q) = %d, want %d", address, got, want)
		}
	}
}

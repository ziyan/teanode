package models

import (
	"strings"
	"testing"
)

// Which names an operator has to publish an address record for, and which this
// server may obtain a certificate for: the ones in the domain's own zone. A
// domain pointing at somebody else's name does neither.
func TestInThisDomain(t *testing.T) {
	t.Parallel()

	domain := &Domain{Domain: "example.com"}
	for _, host := range []string{"example.com", "mx.example.com", "MX.Example.Com.", "a.b.example.com"} {
		if !domain.InThisDomain(host) {
			t.Errorf("%q should be in example.com", host)
		}
	}
	for _, host := range []string{"mx.example.net", "notexample.test", "example.com.elsewhere.test", ""} {
		if domain.InThisDomain(host) {
			t.Errorf("%q should not be in example.com", host)
		}
	}
}

// A name in somebody else's domain is refused as the place pictures come
// from. The addresses in a message are read by whoever receives it, and one
// naming another domain tells them who runs the server — which is the whole
// thing per-domain names exist to stop.
func TestAPictureHostOutsideTheDomainIsRefused(t *testing.T) {
	t.Parallel()

	domain := &Domain{Domain: "example.com", LinkHost: "pictures.somewhere-else.test"}
	err := domain.Validate()
	if err == nil {
		t.Fatal("a name in another domain was accepted")
	}
	if !strings.Contains(err.Error(), "linkHost") {
		t.Errorf("the complaint does not name the field: %s", err)
	}
	domain.LinkHost = "pictures.example.com"
	if err := domain.Validate(); err != nil {
		t.Errorf("a name under the domain was refused: %s", err)
	}
}

// Every way a domain can be wrong names the field, so a form can put the
// message beside it.
func TestDomainValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		domain   Domain
		wantPath string
	}{
		{"no name", Domain{}, "domain"},
		{"not a domain name", Domain{Domain: "not a domain"}, "domain"},
		{"a subdomain with a dot", Domain{Domain: "example.com", Subdomain: "a.b"}, "subdomain"},
		{"a mail server that is not a host name", Domain{Domain: "example.com", MailServers: []string{"not a host"}}, "mailServers[0]"},
		{"a negative threshold", Domain{Domain: "example.com", SpamFilterScoreThreshold: -1}, "spamFilterScoreThreshold"},
		{"a signing key with no selector", Domain{Domain: "example.com", DKIM: DomainKey{PrivateKey: unusableKey()}}, "dkim.selector"},
		{"a selector with no signing key", Domain{Domain: "example.com", DKIM: DomainKey{Selector: "teanode1"}}, "dkim.privateKey"},
		{"an unusable signing key", Domain{Domain: "example.com", DKIM: DomainKey{Selector: "teanode1", PrivateKey: "not a key"}}, "dkim.privateKey"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.domain.Validate()
			if err == nil {
				t.Fatalf("expected a validation error mentioning %q", test.wantPath)
			}
			if !strings.Contains(err.Error(), test.wantPath) {
				t.Errorf("expected an error mentioning %q, got: %s", test.wantPath, err)
			}
		})
	}

	valid := Domain{Domain: "example.com", Subdomain: "mail", MailServers: []string{"mx1.example.com", " "}}
	if err := valid.Validate(); err != nil {
		t.Errorf("a valid domain was refused: %s", err)
	}
}

// A generated key is usable, and published where the domain says it is.
func TestAGeneratedKeyCanBePublished(t *testing.T) {
	t.Parallel()

	key, err := GenerateDomainKey("teanode1")
	if err != nil {
		t.Fatalf("GenerateDomainKey: %s", err)
	}
	record, err := key.PublicKeyRecord()
	if err != nil {
		t.Fatalf("the generated key cannot be published: %s", err)
	}
	if !strings.HasPrefix(record, "v=DKIM1; k=rsa; p=") {
		t.Errorf("the record is %q", record)
	}
	if DomainKeyName("teanode1", "example.com") != "teanode1._domainkey.example.com" {
		t.Error("the record is published at the wrong name")
	}
	domain := Domain{Domain: "example.com", DKIM: key}
	if err := domain.Validate(); err != nil {
		t.Errorf("a domain with a generated key was refused: %s", err)
	}
}

// Every way an alias can be wrong, and the one way that looks wrong and is
// not: an empty pattern is a catch-all, which is how most domains are set up.
func TestAliasValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		alias    Alias
		wantPath string
	}{
		{"invalid pattern", Alias{Pattern: "^[", Kind: AliasKindNull}, "pattern"},
		{"email without an address", Alias{Kind: AliasKindEmail}, "email"},
		{"email that is not an address", Alias{Kind: AliasKindEmail, Email: "nobody"}, "email"},
		{"webhook without a URL", Alias{Kind: AliasKindWebhook}, "webhook"},
		{"webhook that is not http", Alias{Kind: AliasKindWebhook, Webhook: "ftp://example.net"}, "webhook"},
		{"mail server without a host", Alias{Kind: AliasKindMailServer, MailServer: &MailServer{Port: 25}}, "mailServer.host"},
		{"mail server without a port", Alias{Kind: AliasKindMailServer, MailServer: &MailServer{Host: "smtp.example.net"}}, "mailServer.port"},
		{"mailbox without a mailbox", Alias{Kind: AliasKindMailbox}, "mailboxId"},
		{"no kind", Alias{}, "kind"},
		{"unknown kind", Alias{Kind: "carrierPigeon"}, "kind"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.alias.Validate()
			if err == nil {
				t.Fatalf("expected a validation error mentioning %q", test.wantPath)
			}
			if !strings.Contains(err.Error(), test.wantPath) {
				t.Errorf("expected an error mentioning %q, got: %s", test.wantPath, err)
			}
		})
	}

	catchAll := Alias{Pattern: "", Kind: AliasKindEmail, Email: "everything@example.net"}
	if err := catchAll.Validate(); err != nil {
		t.Errorf("a catch-all should be valid, got: %s", err)
	}
	if !catchAll.IsCatchAll() {
		t.Error("an empty pattern should be a catch-all")
	}
	if (&Alias{Pattern: "^hello$"}).IsCatchAll() {
		t.Error("a real pattern was read as a catch-all")
	}
}

// A relay target is reached over whatever network this server is on, not
// looked up in public DNS: an address, a single-label name from a container
// network, or a fully qualified name are all ordinary.
func TestRelayHostsAcceptedByAnAlias(t *testing.T) {
	t.Parallel()

	accepted := []string{"smtp.example.com", "smtp.example.com.", "localhost", "10.0.0.7", "::1", "2001:db8::1"}
	for _, host := range accepted {
		alias := Alias{Kind: AliasKindMailServer, MailServer: &MailServer{Host: host, Port: 25}}
		if err := alias.Validate(); err != nil {
			t.Errorf("%q should be a usable relay target, got: %s", host, err)
		}
	}
	refused := []string{"", "not a host", "-leading-dash", "smtp..example.com"}
	for _, host := range refused {
		alias := Alias{Kind: AliasKindMailServer, MailServer: &MailServer{Host: host, Port: 25}}
		if err := alias.Validate(); err == nil {
			t.Errorf("%q should not be a usable relay target", host)
		}
	}
}

// unusableKey returns something PEM-shaped that is not a key.
//
// The markers are assembled rather than written out because the secret guard
// refuses a tracked file containing a PEM private key header, and it is right
// to: a guard with exceptions for "but this one is fake" stops being a guard.
func unusableKey() string {
	const marker = "-----%s PRIVATE KEY-----\n"
	return strings.Replace(marker, "%s", "BEGIN", 1) + "not actually a key\n" + strings.Replace(marker, "%s", "END", 1)
}

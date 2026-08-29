package config

import (
	"strings"
	"testing"
)

// Where a picture in a message is fetched from, when nobody has said. The
// name mail arrives at is the right guess: it is under the domain, this
// server answers on it, and it needs no second record.
func TestPicturesComeFromTheMailHostByDefault(t *testing.T) {
	t.Parallel()

	configuration := Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Server.MailServers = []string{"mx1.primary.test"}
	configuration.Domains = []*Domain{{Domain: "primary.test"}, {Domain: "other.test"}}

	if host := configuration.LinkHostFor(configuration.FindDomain("other.test")); host != "mx.other.test" {
		t.Errorf("got %q, want the name its mail arrives at", host)
	}
}

// And when somebody has said. A mail server name points at a host whose port
// 443 may belong to something else entirely — a gateway, a controller, a
// router's own page — and then the mail is delivered while every picture in
// it is broken. This is how an operator says where the HTTPS actually is,
// without moving where the mail goes.
func TestAConfiguredNameIsUsedForPictures(t *testing.T) {
	t.Parallel()

	configuration := Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Server.MailServers = []string{"mx1.primary.test"}
	configuration.Domains = []*Domain{
		{Domain: "primary.test"},
		{Domain: "other.test", LinkHost: "other.test"},
		// Tidied the way every other host name is, so a name pasted with a
		// trailing dot and a stray capital means what it looks like.
		{Domain: "spaced.test", LinkHost: " Pictures.Spaced.Test. "},
	}

	if host := configuration.LinkHostFor(configuration.FindDomain("other.test")); host != "other.test" {
		t.Errorf("got %q, want the configured name", host)
	}
	if host := configuration.LinkHostFor(configuration.FindDomain("spaced.test")); host != "pictures.spaced.test" {
		t.Errorf("got %q, want it trimmed and lowercased", host)
	}
}

// A name in somebody else's domain is refused. The addresses in a message are
// read by whoever receives it, and one naming another domain tells them who
// runs the server — which is the whole thing per-domain names exist to stop.
// It is also a name this server could not obtain a certificate for.
func TestAPictureHostOutsideTheDomainIsRefused(t *testing.T) {
	t.Parallel()

	configuration := configurationRelayingTo("smtp.example.com")
	configuration.Domains[0].LinkHost = "pictures.somewhere-else.test"

	err := configuration.Validate()
	if err == nil {
		t.Fatal("a name in another domain was accepted")
	}
	if !strings.Contains(err.Error(), "linkHost") {
		t.Errorf("the complaint does not name the field: %s", err)
	}

	// The same name under the domain is fine.
	configuration.Domains[0].LinkHost = "pictures.example.com"
	if err := configuration.Validate(); err != nil {
		t.Errorf("a name under the domain was refused: %s", err)
	}
}

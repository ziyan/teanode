package config

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Where a picture in a message is fetched from, when nobody has said. The
// name mail arrives at is the right guess: it is under the domain, this
// server answers on it, and it needs no second record.
func TestPicturesComeFromTheMailHostByDefault(t *testing.T) {
	t.Parallel()

	configuration := Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Server.MailServers = []string{"mx1.primary.test"}
	domains := []*models.Domain{{Domain: "primary.test"}, {Domain: "other.test"}}

	if host := configuration.LinkHostFor(domains[1], domains); host != "mx.other.test" {
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
	domains := []*models.Domain{
		{Domain: "primary.test"},
		{Domain: "other.test", LinkHost: "other.test"},
		// Tidied the way every other host name is, so a name pasted with a
		// trailing dot and a stray capital means what it looks like.
		{Domain: "spaced.test", LinkHost: " Pictures.Spaced.Test. "},
	}

	if host := configuration.LinkHostFor(domains[1], domains); host != "other.test" {
		t.Errorf("got %q, want the configured name", host)
	}
	if host := configuration.LinkHostFor(domains[2], domains); host != "pictures.spaced.test" {
		t.Errorf("got %q, want it trimmed and lowercased", host)
	}
}

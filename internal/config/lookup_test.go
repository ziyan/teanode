package config

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A listen address yields its port, so that what this server writes into mail
// points at the port it actually answers on.
func TestAListenAddressYieldsItsPort(t *testing.T) {
	for address, want := range map[string]int{
		":10443": 10443, "127.0.0.1:443": 443, "[::]:8443": 8443,
		"": 0, "nonsense": 0, ":0": 0, ":70000": 0,
	} {
		if got := ListenPortOf(address); got != want {
			t.Errorf("ListenPortOf(%q) = %d, want %d", address, got, want)
		}
	}
}

// The port belongs to the name, not to what this server binds.
//
// This is the whole point of the setting. A deployment can be reached under
// one name straight on a port of its own, and under another through something
// that forwards the usual port to it. Taking the port from the listener would
// write the direct one into every address, and every link through the
// forwarder would point at a port it does not answer on -- which is exactly
// the regression this test exists to stop.
func TestThePortBelongsToTheNameAndNotToTheListener(t *testing.T) {
	direct := &models.Domain{Domain: "teanode.com", LinkHost: "mail.teanode.com:10443"}
	forwarded := &models.Domain{Domain: "example.com", LinkHost: "mx1.teanode.com"}
	domains := []*models.Domain{direct, forwarded}

	// This server binds a port of its own; something in front forwards the
	// usual one for the other name.
	configuration := &Configuration{}
	configuration.Listen.HTTPS = ":10443"

	if got := configuration.LinkBaseFor(direct, domains); got != "https://mail.teanode.com:10443" {
		t.Fatalf("the name that carries a port keeps it: %q", got)
	}
	if got := configuration.LinkBaseFor(forwarded, domains); got != "https://mx1.teanode.com" {
		t.Fatalf("and the name that does not is left alone: %q", got)
	}

	// A DNS answer gets the name without the port, because a name is not a
	// thing that has one: an SRV target with a colon in it is not a target.
	if got := configuration.LinkHostFor(direct, domains); got != "mail.teanode.com" {
		t.Fatalf("the name alone: %q", got)
	}
	// And the port to advertise beside it is the one the name carries.
	if got := configuration.LinkPortFor(direct, domains); got != 10443 {
		t.Fatalf("the port the name is reached on: %d", got)
	}
	// With nothing said, what this server binds is the best guess there is.
	if got := configuration.LinkPortFor(forwarded, domains); got != 10443 {
		t.Fatalf("falling back to the listener: %d", got)
	}
}

// A trailing dot and an odd port are read the way somebody meant them.
func TestHowALinkHostIsRead(t *testing.T) {
	domains := []*models.Domain{}
	for _, shape := range []struct {
		written   string
		authority string
		host      string
	}{
		{"mail.teanode.com", "mail.teanode.com", "mail.teanode.com"},
		{"mail.teanode.com.", "mail.teanode.com", "mail.teanode.com"},
		{"mail.teanode.com:10443", "mail.teanode.com:10443", "mail.teanode.com"},
		{"MAIL.TEANODE.COM:10443", "mail.teanode.com:10443", "mail.teanode.com"},
		// A colon with nothing after it is somebody half-typing, not a
		// port, and the name is what they meant.
		{"mail.teanode.com:", "mail.teanode.com", "mail.teanode.com"},
	} {
		domain := &models.Domain{Domain: "teanode.com", LinkHost: shape.written}
		configuration := &Configuration{}
		if got := configuration.LinkAuthorityFor(domain, domains); got != shape.authority {
			t.Errorf("%q as an authority: %q, want %q", shape.written, got, shape.authority)
		}
		if got := configuration.LinkHostFor(domain, domains); got != shape.host {
			t.Errorf("%q as a name: %q, want %q", shape.written, got, shape.host)
		}
	}
}

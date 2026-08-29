package config

import (
	"testing"
)

// TestRelayHostAcceptsWhatAServerCanActuallyReach covers the difference
// between a name this server publishes and a name it connects to. A relay
// target is reached over whatever network this server is on, so an address or
// a single-label name is ordinary and must not be refused.
func TestRelayHostAcceptsWhatAServerCanActuallyReach(t *testing.T) {
	accepted := []string{
		"smtp.example.com",
		"smtp.example.com.",
		// A container or Kubernetes service name, and a name resolved through
		// a search domain. Both are how an internal smarthost is usually
		// named.
		"mailpit",
		"smtp-relay",
		"localhost",
		"10.0.0.7",
		"::1",
		"2001:db8::1",
	}
	for _, host := range accepted {
		t.Run(host, func(t *testing.T) {
			configuration := configurationRelayingTo(host)
			if err := configuration.Validate(); err != nil {
				t.Errorf("%q should be a usable relay target, got: %s", host, err)
			}
		})
	}

	refused := []string{"", "not a host", "-leading-dash", "smtp..example.com"}
	for _, host := range refused {
		t.Run("refuses "+host, func(t *testing.T) {
			configuration := configurationRelayingTo(host)
			if err := configuration.Validate(); err == nil {
				t.Errorf("%q should not be a usable relay target", host)
			}
		})
	}
}

// configurationRelayingTo builds the smallest valid configuration whose one
// alias relays to a host, so the test asserts on that host and nothing else.
func configurationRelayingTo(host string) *Configuration {
	configuration := Default()
	configuration.Server.Name = "mail.example.com"
	configuration.Server.Secret = "a-secret-long-enough-to-pass"
	configuration.Session.Key = "a-session-key-long-enough"
	configuration.TLS.ACME.Enabled = false
	configuration.TLS.CertificateFile = "teanode.crt"
	configuration.TLS.PrivateKeyFile = "teanode.key"
	configuration.Domains = []*Domain{{
		ID:     NewID(),
		Domain: "example.com",
		Aliases: []*Alias{{
			ID:         NewID(),
			Pattern:    "^.*$",
			Kind:       AliasKindMailServer,
			MailServer: &MailServer{Host: host, Port: 25},
		}},
	}}
	return configuration
}

// TestReverseDNSIsRequiredByDefault pins the default on. It is a spam defence
// that costs one DNS lookup, and a Go bool defaults to false, so nothing but
// this test stands between a refactor and every deployment quietly accepting
// mail from hosts with no reverse DNS.
func TestReverseDNSIsRequiredByDefault(t *testing.T) {
	if !Default().SMTP.RequireReverseDNS {
		t.Error("smtp.requireReverseDns should default to true")
	}
	if !Example().SMTP.RequireReverseDNS {
		t.Error("the configuration written by 'teanode config init' should require reverse DNS")
	}

	// It still has to be possible to turn off, for a server that does not see
	// the real client address.
	configuration, err := Parse([]byte(minimalConfiguration + "smtp:\n  requireReverseDns: false\n"))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	if configuration.SMTP.RequireReverseDNS {
		t.Error("smtp.requireReverseDns: false should turn the check off")
	}
}

// minimalConfiguration is the smallest thing Parse accepts, so a test can add
// the one setting it cares about.
const minimalConfiguration = `server:
  name: mail.example.com
  dataDirectory: /var/lib/teanode
  secret: a-secret-long-enough-to-pass
session:
  key: a-session-key-long-enough
tls:
  certificateFile: teanode.crt
  privateKeyFile: teanode.key
  acme:
    enabled: false
domains:
  - id: 01ARZ3NDEKTSV4RRFFQ69G5FAV
    domain: example.com
    aliases:
      - id: 01ARZ3NDEKTSV4RRFFQ69G5FAW
        pattern: ^.*$
        kind: email
        email: you@example.net
`

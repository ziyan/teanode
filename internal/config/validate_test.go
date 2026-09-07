package config

import (
	"testing"
)

// validConfiguration builds the smallest valid configuration, so a test can
// break one thing and assert on that alone.
func validConfiguration() *Configuration {
	configuration := Default()
	configuration.Server.Name = "mail.example.com"
	configuration.Server.Secret = "a-secret-long-enough-to-pass"
	configuration.Session.Key = "a-session-key-long-enough"
	configuration.TLS.ACME.Enabled = false
	configuration.TLS.CertificateFile = "teanode.crt"
	configuration.TLS.PrivateKeyFile = "teanode.key"
	return configuration
}

// The smallest valid configuration is valid, and a relay named without a
// usable host is not: the one setting the mail path cannot do without.
func TestTheSmallestConfigurationIsValid(t *testing.T) {
	t.Parallel()

	if err := validConfiguration().Validate(); err != nil {
		t.Fatalf("the smallest configuration should be valid, got: %s", err)
	}
	for _, host := range []string{"smtp.example.com", "10.0.0.7", "2001:db8::1"} {
		configuration := validConfiguration()
		configuration.SMTP.Relay.Enabled = true
		configuration.SMTP.Relay.Host = host
		configuration.SMTP.Relay.Port = 587
		if err := configuration.Validate(); err != nil {
			t.Errorf("%q should be a usable relay, got: %s", host, err)
		}
	}
	for _, host := range []string{"", "not a host", "smtp..example.com"} {
		configuration := validConfiguration()
		configuration.SMTP.Relay.Enabled = true
		configuration.SMTP.Relay.Host = host
		configuration.SMTP.Relay.Port = 587
		if err := configuration.Validate(); err == nil {
			t.Errorf("%q should not be a usable relay", host)
		}
	}
}

// TestReverseDNSIsRequiredByDefault pins the default on. It is a spam defence
// that costs one DNS lookup, and a Go bool defaults to false, so nothing but
// this test stands between a refactor and every deployment quietly accepting
// mail from hosts with no reverse name.
func TestReverseDNSIsRequiredByDefault(t *testing.T) {
	t.Parallel()

	if !Default().SMTP.RequireReverseDNS {
		t.Error("smtp.requireReverseDns should default to true")
	}

	configuration, err := Parse([]byte(minimalConfiguration))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	if !configuration.SMTP.RequireReverseDNS {
		t.Error("a file that does not mention smtp.requireReverseDns should get the default, which is on")
	}

	// It still has to be possible to turn off, for a server that does not see
	// the real client address.
	configuration, err = Parse([]byte(minimalConfiguration + "smtp:\n  requireReverseDns: false\n"))
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
`

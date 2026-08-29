package mx

import (
	"crypto/tls"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func servedConfiguration(t *testing.T) config.Store {
	t.Helper()
	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	configuration.Domains = []*config.Domain{
		{ID: "primary.test", Domain: "primary.test", Subdomain: "mail"},
		{ID: "other.test", Domain: "other.test", Subdomain: "mail"},
		{ID: "third.test", Domain: "third.test", Subdomain: "mail"},
	}
	return config.NewMemoryStore(configuration)
}

// The name reported as having received a message is the one the sender
// reached, not the one name the server calls itself. A sender that looked up
// other.test, was handed mx.other.test and connected to it used to be told, in
// the message it had just delivered, that it had reached a host under a
// different domain entirely.
func TestReceivedByNamesTheHostTheSenderReached(t *testing.T) {
	t.Parallel()

	exchange := &exchange{
		config:   servedConfiguration(t),
		settings: &Settings{Server: "mail.primary.test"},
	}

	tests := []struct {
		name       string
		serverName string
		recipients []string
		want       string
	}{
		{
			// The sender's own statement of where it was connecting, which is
			// the question being answered and is authoritative.
			name:       "the name the client asked for",
			serverName: "mx.other.test",
			recipients: []string{"someone@other.test"},
			want:       "mx.other.test",
		},
		{
			// No TLS, so no statement. The recipient's domain says which MX
			// the sender must have looked up.
			name:       "derived from the recipient",
			recipients: []string{"someone@other.test"},
			want:       "mx.other.test",
		},
		{
			// Two served domains in one delivery: the sender connected once,
			// and there is no single right answer.
			name:       "two served domains",
			recipients: []string{"someone@other.test", "someone@third.test"},
			want:       "mail.primary.test",
		},
		{
			// The domain the server is named under keeps the server's name,
			// because that is the name its MX points at.
			name:       "the domain the server is named under",
			recipients: []string{"someone@primary.test"},
			want:       "mail.primary.test",
		},
		{
			// A domain this server does not serve. The message is refused
			// anyway, and inventing a name for it would be a guess.
			name:       "a domain that is not served",
			recipients: []string{"someone@elsewhere.test"},
			want:       "mail.primary.test",
		},
		{
			name:       "no recipients at all",
			recipients: nil,
			want:       "mail.primary.test",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := &mailparse.Envelope{Recipients: test.recipients}
			if test.serverName != "" {
				envelope.TLS = &tls.ConnectionState{ServerName: test.serverName}
			}
			if got := exchange.receivedBy(envelope); got != test.want {
				t.Errorf("receivedBy() = %q, want %q", got, test.want)
			}
		})
	}
}

// A connection that used TLS without naming a host — an older sender, or one
// connecting to an address rather than a name — must not be reported as having
// reached the empty string.
func TestReceivedByFallsBackWhenNoNameWasGiven(t *testing.T) {
	t.Parallel()

	exchange := &exchange{
		config:   servedConfiguration(t),
		settings: &Settings{Server: "mail.primary.test"},
	}
	envelope := &mailparse.Envelope{
		TLS:        &tls.ConnectionState{},
		Recipients: []string{"someone@other.test"},
	}
	if got := exchange.receivedBy(envelope); got != "mx.other.test" {
		t.Errorf("receivedBy() = %q, want the name derived from the recipient", got)
	}
}

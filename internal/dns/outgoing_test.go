package dns

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// servedConfigurationForOutgoing is a minimal server with one domain.
func servedConfigurationForOutgoing(t *testing.T) config.Store {
	t.Helper()
	configuration := config.Default()
	configuration.Server.Name = "mail.primary.test"
	return config.NewMemoryStore(configuration)
}

// The three answers a receiver computes, and what each combination means. The
// arithmetic is trivial; getting it backwards is not, and the failure it
// reports is one that went unnoticed for weeks on a real deployment.
func TestOutgoingIdentityVerified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		identity *OutgoingIdentity
		want     bool
	}{
		{
			name: "confirmed and the greeting agrees",
			identity: &OutgoingIdentity{
				Address: "203.0.113.10", ReverseName: "mail.example.com",
				ForwardAddresses: []string{"203.0.113.10"}, Confirmed: true,
				HelloName: "mail.example.com", HelloMatches: true,
			},
			want: true,
		},
		{
			// The failure that was live: the reverse name resolves somewhere
			// else, so forward confirmation fails on every message.
			name: "the reverse name points at another machine",
			identity: &OutgoingIdentity{
				Address: "203.0.113.10", ReverseName: "mx1.example.com",
				ForwardAddresses: []string{"203.0.113.99"}, Confirmed: false,
				HelloName: "mail.example.com", HelloMatches: true,
			},
			want: false,
		},
		{
			name: "no reverse name at all",
			identity: &OutgoingIdentity{
				Address: "203.0.113.10", Confirmed: false,
				HelloName: "mail.example.com", HelloMatches: true,
			},
			want: false,
		},
		{
			// The other half that was live: the greeting named a host with no
			// address record.
			name: "the greeting names a host that is elsewhere",
			identity: &OutgoingIdentity{
				Address: "203.0.113.10", ReverseName: "mail.example.com",
				ForwardAddresses: []string{"203.0.113.10"}, Confirmed: true,
				HelloName: "mail.example.com", HelloMatches: false,
			},
			want: false,
		},
		{
			// A relay sends the mail from its own address. Nothing can be
			// established, and reporting a failure would send an operator to
			// change something that is not wrong.
			name:     "handed to a relay",
			identity: &OutgoingIdentity{Via: "relay", Unknown: "outgoing mail is handed to a relay"},
			want:     false,
		},
		{name: "nothing checked yet", identity: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.identity.Verified(); got != test.want {
				t.Errorf("Verified() = %v, want %v", got, test.want)
			}
		})
	}
}

// A relay is not a failure to report. The check must say it cannot know rather
// than report the address mail arrives at as though mail left from it.
func TestARelayIsReportedAsUnknown(t *testing.T) {
	t.Parallel()

	configuration := servedConfigurationForOutgoing(t)
	current := configuration.Current()
	current.SMTP.Relay.Enabled = true
	current.SMTP.Relay.Host = "smtp.example.net"

	verifier := &verifier{config: configuration, settings: &Settings{Nameserver: "127.0.0.1:1"}}
	identity := verifier.checkOutgoingIdentity(t.Context())

	if identity.Via != "relay" {
		t.Errorf("via is %q, want relay", identity.Via)
	}
	if identity.Unknown == "" {
		t.Error("a relay was not reported as something this server cannot establish")
	}
	if identity.Address != "" {
		t.Errorf("an address was reported as the sending one: %q", identity.Address)
	}
}

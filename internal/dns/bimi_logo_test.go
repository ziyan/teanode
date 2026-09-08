package dns

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// A logo this server hosts is read out of storage rather than fetched from
// our own address.
//
// Not an optimisation. The fetch goes through the guard that refuses anything
// but a public address, and a great many of these servers answer to a name
// that resolves inside the network they run in — so checking a perfectly
// correct record would fail with "not a public address" and tell the operator
// to fix something that is not wrong.
//
// Two names count as ours: the one a domain publishes its pictures under,
// which is what a record written today says, and the server's own, which is
// what records written before that said. A server called mx1.example.com
// serving mail.example.com has both.
func TestALogoOnThisServerIsRecognisedAsOurs(t *testing.T) {
	t.Parallel()

	configuration := config.Default()
	configuration.Server.Name = "mail.example.com"
	verifier := &verifier{config: config.NewMemoryStore(configuration)}

	tests := []struct {
		name    string
		address string
		host    string
		want    string
	}{
		{
			name:    "the name this domain publishes under, which is what a record says today",
			address: "https://mail.example.com/.well-known/bimi/01abc.svg",
			host:    "mail.example.com",
			want:    "01abc",
		},
		{
			// The node's own name. A record written before the address moved
			// still says this, and the file is still ours to read.
			name:    "our own address",
			address: "https://mail.example.com/.well-known/bimi/01abc.svg",
			want:    "01abc",
		},
		{
			name:    "a domain whose pictures are on a name of its own",
			address: "https://images.example.org/.well-known/bimi/01abc.svg",
			host:    "images.example.org",
			want:    "01abc",
		},
		{
			// Somebody else's server is somebody else's, whatever this domain
			// publishes its own pictures under.
			name:    "another server, while this domain has a picture name",
			address: "https://vmc.example.net/9e57aa28.svg",
			host:    "images.example.org",
		},
		{
			name:    "somebody else's server, which is fetched",
			address: "https://vmc.example.net/9e57aa28.svg",
		},
		{
			// A name that merely ends the same way is not this server, and
			// treating it as ours would read a file for an address we do not
			// serve.
			name:    "a name that only looks like ours",
			address: "https://mail.example.com.example.net/.well-known/bimi/01abc.svg",
		},
		{
			name:    "our name over plain http, which is not the address we publish",
			address: "http://mail.example.com/.well-known/bimi/01abc.svg",
		},
		{
			name:    "our server, but not a logo address",
			address: "https://mail.example.com/media/01abc",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := verifier.ownLogoID(test.address, test.host); got != test.want {
				t.Errorf("ownLogoID(%q, %q) = %q, want %q", test.address, test.host, got, test.want)
			}
		})
	}
}

package smtpd

import (
	"crypto/tls"
	"errors"
	"testing"
)

// A server with no certificate yet — the first minutes of a new deployment,
// before ACME finishes — used to advertise STARTTLS anyway. A sender that took
// the offer failed the handshake, and Exchange Online's response to that is to
// reconnect immediately and deliver in the clear. The mail arrives unencrypted
// and nothing says so.
func TestStartTLSIsOnlyOfferedWhenItCanBeDone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *tls.Config
		offered bool
	}{
		{
			name:    "no TLS configured at all",
			config:  nil,
			offered: false,
		},
		{
			name: "a certificate source that has nothing yet",
			config: &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return nil, errors.New("no certificate obtained yet")
			}},
			offered: false,
		},
		{
			name: "a source that returns nothing without saying why",
			config: &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return nil, nil
			}},
			offered: false,
		},
		{
			name: "a certificate source with one to give",
			config: &tls.Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &tls.Certificate{}, nil
			}},
			offered: true,
		},
		{
			name:    "a statically configured certificate",
			config:  &tls.Config{Certificates: []tls.Certificate{{}}},
			offered: true,
		},
	}

	for _, test := range tests {
		session := &session{settings: &Settings{TLSConfig: test.config}}
		if got := session.canStartTls(); got != test.offered {
			t.Errorf("%s: canStartTls() = %v, want %v", test.name, got, test.offered)
		}
	}
}

// The certificate source is asked the same question a handshake would ask, so
// the advertisement cannot disagree with what happens next.
func TestTheCertificateSourceIsAskedWithoutSNI(t *testing.T) {
	t.Parallel()

	var asked *tls.ClientHelloInfo
	session := &session{settings: &Settings{TLSConfig: &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			asked = hello
			return &tls.Certificate{}, nil
		},
	}}}
	if !session.canStartTls() {
		t.Fatal("a source with a certificate was reported as having none")
	}
	if asked == nil {
		t.Fatal("the certificate source was never asked")
	}
	if asked.ServerName != "" {
		t.Errorf("asked with ServerName %q; a sender without SNI is the case that has to work", asked.ServerName)
	}
	if len(asked.SupportedVersions) == 0 {
		t.Error("asked with no supported versions, which some sources reject out of hand")
	}
}

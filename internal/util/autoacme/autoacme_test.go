package autoacme

import (
	"crypto/tls"
	"errors"
	"testing"

	"golang.org/x/crypto/acme"
)

// openForTest builds a manager without starting the renewal loop. Open would
// immediately try to obtain a certificate from a real certificate authority,
// which a unit test must never do.
func openForTest(t *testing.T, challenge string) (*manager, error) {
	t.Helper()

	return newManager(&Settings{
		Certificates: []CertificateRequest{{Hosts: []string{"mail.example.com"}}},
		Challenge:    challenge,
		ACMEEmail:    "you@example.com",
	})
}

// TestOpenSelectsSolver checks that only the solvers actually needed are
// constructed, and in particular that an operator using http-01 or
// tls-alpn-01 never causes an AWS client to be built: the Route53 solver is
// the only thing that touches AWS, and it is only reachable through the
// dns-01 branch.
func TestOpenSelectsSolver(t *testing.T) {
	tests := []struct {
		challenge   string
		wantType    string
		wantHandler bool
	}{
		{"http-01", "http-01", true},
		{"", "http-01", true}, // empty means the default
		{"tls-alpn-01", "tls-alpn-01", false},
		{"dns-01", "dns-01", false},
	}

	for _, test := range tests {
		name := test.challenge
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			instance, err := openForTest(t, test.challenge)
			if err != nil {
				t.Fatalf("failed to open: %s", err)
			}
			defer func() {
				_ = instance.Close()
			}()

			if len(instance.solvers) != 1 {
				t.Fatalf("%d solvers were built, want the one that is used", len(instance.solvers))
			}
			for _, solver := range instance.solvers {
				if got := solver.Type(); got != test.wantType {
					t.Errorf("solver type is %q, want %q", got, test.wantType)
				}
			}
			if got := instance.HTTPHandler() != nil; got != test.wantHandler {
				t.Errorf("HTTPHandler present = %v, want %v", got, test.wantHandler)
			}
		})
	}
}

// One server can need two answers: a wildcard can only be obtained over
// dns-01, and dns-01 needs credentials for the zone the name lives in, which
// a server has for its own domain and not for everybody else's. So the
// server's own certificate is proved one way and a domain's another, in the
// same process.
func TestTwoChallengesAtOnce(t *testing.T) {
	instance, err := newManager(&Settings{
		Challenge: "dns-01",
		Certificates: []CertificateRequest{
			// The server's own, taking the configured challenge.
			{Hosts: []string{"example.com", "*.example.com"}},
			// A domain's, which cannot use the zone credentials above.
			{Key: "other.test", Hosts: []string{"mx.other.test"}, Challenge: "http-01"},
		},
	})
	if err != nil {
		t.Fatalf("failed to open: %s", err)
	}
	defer func() {
		_ = instance.Close()
	}()

	if len(instance.solvers) != 2 {
		t.Errorf("%d solvers were built, want one for each challenge in use", len(instance.solvers))
	}
	// The http-01 handler has to be mounted even though the configured
	// challenge is dns-01, or the domain certificates can never be validated.
	if instance.HTTPHandler() == nil {
		t.Error("no http-01 handler, so a challenge for a domain could not be answered")
	}
	if instance.certificates[0].challenge != "dns-01" {
		t.Errorf("the server certificate uses %q, want the configured challenge", instance.certificates[0].challenge)
	}
	if instance.certificates[1].challenge != "http-01" {
		t.Errorf("the domain certificate uses %q, want its own", instance.certificates[1].challenge)
	}
}

func TestOpenRejectsUnknownChallenge(t *testing.T) {
	_, err := openForTest(t, "carrier-pigeon-01")
	if !errors.Is(err, ErrUnknownChallenge) {
		t.Fatalf("expected ErrUnknownChallenge, got %v", err)
	}
}

func TestOpenRequiresHosts(t *testing.T) {
	_, err := Open(&Settings{})
	if !errors.Is(err, ErrNoHosts) {
		t.Fatalf("expected ErrNoHosts, got %v", err)
	}
}

// TestGetCertificateRejectsUnexpectedALPNHandshake covers the case where
// somebody opens an "acme-tls/1" connection when no challenge is outstanding.
// Handing back the real certificate there would be wrong, and panicking would
// be worse; the handshake is refused.
func TestGetCertificateRejectsUnexpectedALPNHandshake(t *testing.T) {
	for _, challenge := range []string{"http-01", "tls-alpn-01"} {
		t.Run(challenge, func(t *testing.T) {
			instance, err := openForTest(t, challenge)
			if err != nil {
				t.Fatalf("failed to open: %s", err)
			}
			defer func() {
				_ = instance.Close()
			}()

			_, err = instance.GetCertificate(&tls.ClientHelloInfo{
				ServerName:      "mail.example.com",
				SupportedProtos: []string{acme.ALPNProto},
			})
			if !errors.Is(err, ErrInvalidClientHello) {
				t.Errorf("expected ErrInvalidClientHello, got %v", err)
			}
		})
	}
}

// TestGetCertificateServesALPNChallenge is the other half: once a challenge is
// outstanding, the handshake gets the challenge certificate rather than the
// real one.
func TestGetCertificateServesALPNChallenge(t *testing.T) {
	instance, err := openForTest(t, "tls-alpn-01")
	if err != nil {
		t.Fatalf("failed to open: %s", err)
	}
	defer func() {
		_ = instance.Close()
	}()

	challengeCertificate := tls.Certificate{Certificate: [][]byte{{9, 9, 9}}}
	solver := instance.alpnSolver
	if err := solver.Present(t.Context(), &fakeACMEClient{certificate: challengeCertificate}, challengesFor("mail.example.com")); err != nil {
		t.Fatalf("failed to present: %s", err)
	}

	certificate, err := instance.GetCertificate(&tls.ClientHelloInfo{
		ServerName:      "mail.example.com",
		SupportedProtos: []string{acme.ALPNProto},
	})
	if err != nil {
		t.Fatalf("failed to get the challenge certificate: %s", err)
	}
	if len(certificate.Certificate) != 1 || certificate.Certificate[0][0] != 9 {
		t.Error("the handshake did not receive the challenge certificate")
	}

	// An ordinary client on the same name must not get the challenge
	// certificate; with no real certificate issued yet, it gets an error.
	if _, err := instance.GetCertificate(&tls.ClientHelloInfo{
		ServerName:      "mail.example.com",
		SupportedProtos: []string{"h2"},
	}); !errors.Is(err, ErrNoCertificate) {
		t.Errorf("an ordinary client got %v, want ErrNoCertificate", err)
	}
}

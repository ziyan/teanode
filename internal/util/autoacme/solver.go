package autoacme

import (
	"context"
	"crypto/tls"

	"golang.org/x/crypto/acme"
)

// acmeClient is the part of *acme.Client that solvers use. Depending on the
// interface rather than the concrete client lets a solver be tested without
// talking to a certificate authority.
type acmeClient interface {
	HTTP01ChallengeResponse(token string) (string, error)
	TLSALPN01ChallengeCert(token, domain string, options ...acme.CertOption) (tls.Certificate, error)
	DNS01ChallengeRecord(token string) (string, error)
}

// Challenge is one challenge the certificate authority has offered, paired
// with the name it authorizes.
type Challenge struct {
	// Domain is the name being authorized, for example "mail.example.com".
	// For a wildcard authorization this is the bare domain without the "*.".
	Domain string

	// Challenge is the challenge the authority offered for that name.
	Challenge *acme.Challenge
}

// Solver proves to a certificate authority that this server controls a name.
//
// The three implementations differ in what the operator must have available:
// http01Solver needs port 80 reachable from the internet, tlsAlpn01Solver
// needs port 443, and route53Solver needs AWS credentials for the hosted zone
// but is the only one that can obtain a wildcard certificate.
//
// Present is given every challenge for an order at once rather than one at a
// time, because DNS-01 has to write all of an order's TXT values into a single
// record set and then wait once for propagation. The HTTP and ALPN solvers
// simply loop.
type Solver interface {
	// Type is the ACME challenge type this solver answers, one of "http-01",
	// "tls-alpn-01" or "dns-01".
	Type() string

	// Present makes the responses available to the authority and returns only
	// when they are actually reachable, so the caller may immediately ask the
	// authority to validate.
	Present(ctx context.Context, client acmeClient, challenges []Challenge) error

	// CleanUp removes whatever Present put in place. It is called even when
	// validation failed, and must tolerate being called when Present did not
	// finish.
	CleanUp(ctx context.Context, challenges []Challenge) error
}

package autoacme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

// acmeIdentifierExtension is the OID of the extension that carries the
// challenge value in a tls-alpn-01 certificate, from RFC 8737 section 6.1.
var acmeIdentifierExtension = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}

// tlsAlpn01Solver answers the tls-alpn-01 challenge: the certificate authority
// opens a TLS connection to port 443 of the name being authorized, offering
// the "acme-tls/1" application protocol, and expects to be handed a
// self-signed certificate carrying the challenge value in an extension. No
// HTTP request is ever made and nothing is served, so this works on a host
// where port 80 is blocked but port 443 is not.
//
// What is kept, in the challenge store every instance shares, is the key
// authorization under the name being authorized. The certificate is built from
// it by whichever instance the handshake reaches, because the handshake can
// reach any of them. The certificate is then handed out by the manager's
// GetCertificate, which is why this solver has to be reachable from there.
type tlsAlpn01Solver struct {
	store ChallengeStore
}

func newTlsalpn01Solver(store ChallengeStore) *tlsAlpn01Solver {
	return &tlsAlpn01Solver{store: store}
}

func (self *tlsAlpn01Solver) Type() string {
	return "tls-alpn-01"
}

func (self *tlsAlpn01Solver) Present(ctx context.Context, client acmeClient, challenges []Challenge) error {
	for _, challenge := range challenges {
		// For http-01 the response is the key authorization, and that is also
		// what the tls-alpn-01 extension is a hash of.
		keyAuthorization, err := client.HTTP01ChallengeResponse(challenge.Challenge.Token)
		if err != nil {
			return fmt.Errorf("autoacme: cannot build the tls-alpn-01 key authorization for %q: %w", challenge.Domain, err)
		}
		if err := self.store.Put(ctx, self.Type(), strings.ToLower(challenge.Domain), keyAuthorization, time.Now().Add(challengeLifetime)); err != nil {
			return fmt.Errorf("autoacme: cannot store the tls-alpn-01 key authorization for %q: %w", challenge.Domain, err)
		}
		log.Debugf("serving tls-alpn-01 challenge for %q", challenge.Domain)
	}
	return nil
}

func (self *tlsAlpn01Solver) CleanUp(ctx context.Context, challenges []Challenge) error {
	var firstErr error
	for _, challenge := range challenges {
		if err := self.store.Delete(ctx, strings.ToLower(challenge.Domain)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// certificateFor returns the challenge certificate for a name, if an order is
// currently waiting on it.
func (self *tlsAlpn01Solver) certificateFor(ctx context.Context, serverName string) (*tls.Certificate, bool) {
	domain := strings.ToLower(serverName)
	keyAuthorization, ok, err := self.store.Find(ctx, self.Type(), domain)
	if err != nil {
		log.Warningf("failed to look up the tls-alpn-01 challenge for %q: %s", serverName, err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	certificate, err := tlsAlpn01Certificate(domain, keyAuthorization)
	if err != nil {
		log.Warningf("failed to build the tls-alpn-01 certificate for %q: %s", serverName, err)
		return nil, false
	}
	return certificate, true
}

// tlsAlpn01Certificate builds the self-signed certificate a tls-alpn-01
// handshake is answered with: it names the domain and carries the SHA-256 of
// the key authorization in the critical acmeIdentifier extension. It is the
// same certificate (apart from its throwaway key) that
// acme.Client.TLSALPN01ChallengeCert builds, made from the key authorization
// so that an instance without the account key can build it.
func tlsAlpn01Certificate(domain, keyAuthorization string) (*tls.Certificate, error) {
	digest := sha256.Sum256([]byte(keyAuthorization))
	extensionValue, err := asn1.Marshal(digest[:])
	if err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: domain},
		DNSNames:              []string{domain},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		ExtraExtensions: []pkix.Extension{{
			Id:       acmeIdentifierExtension,
			Critical: true,
			Value:    extensionValue,
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// isAlpnChallenge reports whether a TLS handshake is a certificate authority
// performing a tls-alpn-01 challenge rather than an ordinary client.
func isAlpnChallenge(hello *tls.ClientHelloInfo) bool {
	for _, protocol := range hello.SupportedProtos {
		if protocol == acme.ALPNProto {
			return true
		}
	}
	return false
}

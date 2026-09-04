package autoacme

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/acme"
)

// tlsAlpn01Solver answers the tls-alpn-01 challenge: the certificate authority
// opens a TLS connection to port 443 of the name being authorized, offering
// the "acme-tls/1" application protocol, and expects to be handed a
// self-signed certificate carrying the challenge value in an extension. No
// HTTP request is ever made and nothing is served, so this works on a host
// where port 80 is blocked but port 443 is not.
//
// The certificates live only in memory and only while the order is open. They
// are handed out by the manager's GetCertificate, which is why this solver has
// to be reachable from there.
type tlsAlpn01Solver struct {
	mutex        sync.RWMutex
	certificates map[string]*tls.Certificate
}

func newTlsalpn01Solver() *tlsAlpn01Solver {
	return &tlsAlpn01Solver{
		certificates: make(map[string]*tls.Certificate),
	}
}

func (self *tlsAlpn01Solver) Type() string {
	return "tls-alpn-01"
}

func (self *tlsAlpn01Solver) Present(ctx context.Context, client acmeClient, challenges []Challenge) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, challenge := range challenges {
		certificate, err := client.TLSALPN01ChallengeCert(challenge.Challenge.Token, challenge.Domain)
		if err != nil {
			return fmt.Errorf("autoacme: cannot build the tls-alpn-01 certificate for %q: %w", challenge.Domain, err)
		}
		self.certificates[strings.ToLower(challenge.Domain)] = &certificate
		log.Debugf("serving tls-alpn-01 challenge for %q", challenge.Domain)
	}
	return nil
}

func (self *tlsAlpn01Solver) CleanUp(ctx context.Context, challenges []Challenge) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, challenge := range challenges {
		delete(self.certificates, strings.ToLower(challenge.Domain))
	}
	return nil
}

// certificateFor returns the challenge certificate for a name, if an order is
// currently waiting on it.
func (self *tlsAlpn01Solver) certificateFor(serverName string) (*tls.Certificate, bool) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	certificate, ok := self.certificates[strings.ToLower(serverName)]
	return certificate, ok
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

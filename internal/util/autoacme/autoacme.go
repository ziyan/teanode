// Package autoacme provides automatic ACME certificate management.
package autoacme

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/op/go-logging"
	"golang.org/x/crypto/acme"

	"github.com/ziyan/teanode/internal/util/periodic"
)

var log = logging.MustGetLogger("autoacme")

// LetsEncryptDirectoryURL is the production ACME directory, used when
// Settings.DirectoryURL is empty.
const LetsEncryptDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

var (
	ErrNoCertificate      = errors.New("autoacme: no certificate")
	ErrInvalidClientHello = errors.New("autoacme: invalid client hello")
	ErrInvalidOrderStatus = errors.New("autoacme: invalid order status")
	ErrInvalidChallenges  = errors.New("autoacme: invalid challenges")
	ErrInvalidACMEKey     = errors.New("autoacme: invalid acme key")
	ErrInvalidCertificate = errors.New("autoacme: invalid certificate")
	ErrUnknownChallenge   = errors.New("autoacme: unknown challenge type")
	ErrNoHosts            = errors.New("autoacme: no hosts to obtain a certificate for")
	ErrOrderFailed        = errors.New("autoacme: the certificate order did not complete")
)

// CertificateRequest is one certificate to obtain and keep renewed.
type CertificateRequest struct {
	// Key identifies this certificate in storage and in logs. The caller
	// chooses it and recognises it when SaveCertificate is called: the
	// domain's identifier for a domain's certificate, and the empty string
	// for the server's own.
	Key string

	// Hosts are the names the certificate must cover. The first becomes its
	// common name.
	Hosts []string

	// Challenge is how control of these names is proved, overriding
	// Settings.Challenge. Empty uses that.
	//
	// It is per certificate because one server can need two answers. A
	// wildcard can only be obtained with dns-01, and dns-01 needs credentials
	// for the zone the name lives in — which a server has for its own domain
	// and not for everybody else's. So the server's own wildcard is proved
	// one way and a domain's single name another, in the same process.
	Challenge string

	// Certificate and PrivateKey are the last issued pair in PEM, as saved.
	// Empty means none has been obtained yet, which is not an error: the
	// server serves another certificate for those names until one arrives.
	Certificate string
	PrivateKey  string
}

type Settings struct {
	// AccountKey identifies this server to the certificate authority, as a
	// PEM encoded EC key. Empty means one is generated; SaveAccountKey is
	// then called so the caller can keep it.
	AccountKey string

	// SaveAccountKey is called once with a newly generated account key.
	// Without it the key is forgotten on restart and the server registers
	// again, which works but spends rate limit.
	SaveAccountKey func(string) error

	// Challenge is how control of a name is proven: "http-01" (the default)
	// needs port 80 reachable, "tls-alpn-01" needs port 443, and "dns-01"
	// needs Route53 credentials but is the only one that can obtain a
	// wildcard certificate.
	Challenge string

	// DirectoryURL is the ACME provider. Empty means Let's Encrypt
	// production; point it at a staging directory while testing.
	DirectoryURL string

	// for acme to contact
	ACMEEmail string

	// Certificates are the certificates to obtain and keep renewed. There is
	// normally one for the server itself and one for each domain that has a
	// mail server name of its own, so that a sender connecting to a domain is
	// handed a certificate naming that domain rather than somebody else's.
	//
	// The first is the one served to a client that sends no name, which older
	// senders do not.
	Certificates []CertificateRequest

	// SaveCertificate is called with each newly issued certificate, and the
	// key of the request it satisfies, so the caller can keep it. Without it
	// a restart obtains another one, which works but spends rate limit.
	SaveCertificate func(key, certificate, privateKey string) error

	// function to get route53 zone id
	Route53ZoneID string

	// function to get nameservers used to check if record is propagated
	Route53Nameservers []string

	// aws config
	AWSConfig aws.Config
}

type Manager interface {
	// GetCertificate is a tls.Config.GetCertificate hook. It serves the
	// managed certificate, and answers tls-alpn-01 challenges when that
	// challenge type is configured.
	GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)

	// HTTPHandler answers http-01 challenges, or is nil when a different
	// challenge type is configured.
	HTTPHandler() http.Handler

	Close() error
}

// certificateState is one certificate the manager keeps renewed, and what it
// knows about trying.
type certificateState struct {
	key   string
	hosts []string

	// challenge is how control of these names is proved.
	challenge string

	// certificate is nil until one has been obtained. A name with no
	// certificate is served the fallback rather than refused.
	certificate *tls.Certificate

	// failures and nextAttempt keep one name that cannot be validated from
	// retrying every five minutes forever, and — more importantly — from
	// spending the rate limit that the other names need. A domain whose port
	// 80 is unreachable must not stop the rest from being renewed.
	failures    int
	nextAttempt time.Time
}

type manager struct {
	settings *Settings

	acmeClientMutex sync.Mutex
	acmeClient      *acme.Client

	certificateMutex sync.Mutex
	certificates     []*certificateState

	// solvers answer challenges, by type. Only the ones actually asked for
	// are constructed, so an operator whose certificates are all proved over
	// http-01 never creates an AWS client.
	solvers map[string]Solver

	// alpnSolver is the same object as solver when the tls-alpn-01 challenge
	// is in use, and nil otherwise. GetCertificate needs it directly, because
	// that challenge is answered inside the TLS handshake.
	alpnSolver *tlsALPN01Solver

	// httpSolver is the same object as solver when the http-01 challenge is in
	// use, and nil otherwise. The caller mounts its handler.
	httpSolver *http01Solver

	waitGroup sync.WaitGroup
	periodic  periodic.Periodic
}

// Open builds a manager and starts the renewal loop, which obtains a
// certificate immediately if there is not already a usable one on disk.
func Open(settings *Settings) (Manager, error) {
	self, err := newManager(settings)
	if err != nil {
		return nil, err
	}
	self.periodic.Start()
	return self, nil
}

// newManager builds a manager without starting the renewal loop, so that tests
// can exercise the challenge handling without contacting a certificate
// authority.
func newManager(settings *Settings) (*manager, error) {
	if len(settings.Certificates) == 0 {
		return nil, ErrNoHosts
	}
	self := &manager{settings: settings}
	for _, request := range settings.Certificates {
		if len(request.Hosts) == 0 {
			return nil, ErrNoHosts
		}
		certificate, err := decodeCertificate(request.Certificate, request.PrivateKey)
		if err != nil {
			// One unreadable stored certificate must not stop the server from
			// starting: it is obtained again, and the rest are unaffected.
			log.Warningf("the stored certificate for %v cannot be read and will be obtained again: %s", request.Hosts, err)
			certificate = &tls.Certificate{}
		}
		challenge := request.Challenge
		if challenge == "" {
			challenge = settings.Challenge
		}
		self.certificates = append(self.certificates, &certificateState{
			key:         request.Key,
			hosts:       request.Hosts,
			challenge:   challenge,
			certificate: certificate,
		})
	}

	for _, state := range self.certificates {
		if _, err := self.solverFor(state.challenge); err != nil {
			return nil, err
		}
	}

	log.Debugf("keeping %d certificates renewed", len(self.certificates))
	self.periodic = periodic.New(context.TODO(), &self.waitGroup, self.spinOnce, &periodic.Settings{
		Interval: 5 * time.Minute,
		Name:     "autoacme",
	})
	return self, nil
}

// solverFor builds the solver for a challenge type, once, and hands back the
// same one afterwards. The tls-alpn-01 and http-01 solvers are also reached
// directly — one from inside the TLS handshake, the other from a URL the
// caller mounts — so they are remembered separately as well.
func (self *manager) solverFor(challenge string) (Solver, error) {
	if self.solvers == nil {
		self.solvers = map[string]Solver{}
	}
	if challenge == "" {
		challenge = "http-01"
	}
	if solver, ok := self.solvers[challenge]; ok {
		return solver, nil
	}

	var solver Solver
	switch challenge {
	case "http-01":
		self.httpSolver = newHTTP01Solver()
		solver = self.httpSolver
	case "tls-alpn-01":
		self.alpnSolver = newTLSALPN01Solver()
		solver = self.alpnSolver
	case "dns-01":
		solver = newRoute53Solver(self.settings)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownChallenge, challenge)
	}
	self.solvers[challenge] = solver
	return solver, nil
}

func (self *manager) Close() error {
	defer self.waitGroup.Wait()
	self.periodic.Stop()
	return nil
}

func (self *manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// A handshake offering the "acme-tls/1" protocol is a certificate
	// authority validating a tls-alpn-01 challenge, not a real client. It
	// must be answered with the challenge certificate for that exact name.
	if isALPNChallenge(hello) {
		if self.alpnSolver == nil {
			return nil, ErrInvalidClientHello
		}
		certificate, ok := self.alpnSolver.certificateFor(hello.ServerName)
		if !ok {
			log.Warningf("received a tls-alpn-01 handshake for %q with no challenge outstanding", hello.ServerName)
			return nil, ErrInvalidClientHello
		}
		return certificate, nil
	}

	self.certificateMutex.Lock()
	defer self.certificateMutex.Unlock()

	// The certificate for the name the client asked for, so a sender
	// connecting to one domain is not handed a certificate naming another.
	for _, state := range self.certificates {
		if !usable(state.certificate) {
			continue
		}
		for _, host := range state.hosts {
			if hostMatches(host, hello.ServerName) {
				return state.certificate, nil
			}
		}
	}

	// Nothing matched, or the client sent no name at all, which older senders
	// do. The first certificate is the server's own and is the best available
	// answer: a name mismatch is something almost every sender accepts, and
	// refusing the handshake outright would refuse the mail.
	//
	// An empty certificate is not nil here — that is what a server which has
	// not obtained one yet holds — and handing it to a handshake fails
	// obscurely, so it is treated as having none.
	for _, state := range self.certificates {
		if usable(state.certificate) {
			return state.certificate, nil
		}
	}
	return nil, ErrNoCertificate
}

// usable reports whether a certificate can be served. A server that has not
// obtained one yet holds an empty certificate rather than nil.
func usable(certificate *tls.Certificate) bool {
	return certificate != nil && len(certificate.Certificate) > 0
}

// Covers reports whether a certificate for these names would already serve
// this one, so that a caller does not ask an authority for a second
// certificate covering something it already has. Exported because the caller
// building the list of certificates has to answer the same question this
// package answers on every handshake, and two implementations of a wildcard
// rule is one too many: the first attempt compared names as strings, so a
// wildcard matched nothing and a duplicate certificate was ordered for names
// the server's own already covered.
func Covers(hosts []string, name string) bool {
	for _, host := range hosts {
		if hostMatches(host, name) {
			return true
		}
	}
	return false
}

// hostMatches reports whether a name in a certificate covers the name a client
// asked for, by the rule TLS itself uses: exact, or one label below a wildcard.
func hostMatches(host, name string) bool {
	if name == "" {
		return false
	}
	if strings.EqualFold(host, name) {
		return true
	}
	if !strings.HasPrefix(host, "*.") {
		return false
	}
	// "*.example.com" covers "mx.example.com" and not "a.mx.example.com",
	// which is what a wildcard means in a certificate.
	suffix := host[1:]
	if len(name) <= len(suffix) || !strings.EqualFold(name[len(name)-len(suffix):], suffix) {
		return false
	}
	return !strings.Contains(name[:len(name)-len(suffix)], ".")
}

// HTTPHandler returns the handler that answers http-01 challenges, or nil when
// a different challenge type is configured. Mount it at
// "/.well-known/acme-challenge/" on a listener reachable on port 80, ahead of
// any authentication, since a certificate authority cannot log in.
func (self *manager) HTTPHandler() http.Handler {
	if self.httpSolver == nil {
		return nil
	}
	return self.httpSolver.Handler()
}

// ChallengePath is where HTTPHandler must be mounted.
const ChallengePath = challengePath

func (self *manager) spinOnce(ctx context.Context) error {
	// A copy of the list, so the lock is not held across the network. The
	// states themselves are only written under the lock, below.
	self.certificateMutex.Lock()
	states := make([]*certificateState, len(self.certificates))
	copy(states, self.certificates)
	self.certificateMutex.Unlock()

	var failed error
	for _, state := range states {
		if !self.shouldRequestCertificate(state) {
			continue
		}

		// In the worst case the timeout has to cover caching, host policy,
		// domain ownership verification and issuance. Per certificate, so one
		// slow name does not eat the time the others need.
		requestContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := self.requestCertificate(requestContext, state)
		cancel()

		if err != nil {
			// Recorded rather than returned, so the certificates after this
			// one in the list still get their turn.
			if failed == nil {
				failed = err
			}
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return failed
}

// requestBackoff is how long to wait after a failed attempt, doubling each
// time up to requestBackoffMax.
//
// Without it, a single name that cannot be validated — a domain whose port 80
// is unreachable, which is the ordinary way this fails — is retried every five
// minutes forever. That is not merely noise: certificate authorities count
// failures, and one broken domain would spend the allowance the other
// twenty-four need.
const (
	requestBackoff    = 5 * time.Minute
	requestBackoffMax = 24 * time.Hour
)

func (self *manager) shouldRequestCertificate(state *certificateState) bool {
	self.certificateMutex.Lock()
	defer self.certificateMutex.Unlock()

	if state.failures > 0 && time.Now().Before(state.nextAttempt) {
		return false
	}

	if !usable(state.certificate) {
		log.Warningf("no certificate yet for %v", state.hosts)
		return true
	}

	expiry, err := validateCertificate(state.certificate, state.hosts)
	if err != nil {
		log.Warningf("the certificate for %v needs to be renewed: %s", state.hosts, err)
		return true
	}

	// Thirty days, which is a third of a certificate's life and long enough
	// that a name broken today is noticed and fixed before anything expires.
	if time.Now().Add(30 * 24 * time.Hour).After(expiry) {
		log.Warningf("the certificate for %v is going to expire within 30 days", state.hosts)
		return true
	}

	return false
}

func (self *manager) requestCertificate(ctx context.Context, state *certificateState) error {
	certificate, err := self.requestAcmeCertificate(ctx, state.hosts, state.challenge)
	if err == nil {
		_, err = validateCertificate(certificate, state.hosts)
		if err != nil {
			log.Errorf("the certificate obtained for %v is not valid: %s", state.hosts, err)
		}
	} else {
		log.Errorf("failed to request a certificate for %v: %s", state.hosts, err)
	}
	if err != nil {
		self.recordFailure(state)
		return err
	}

	if self.settings.SaveCertificate != nil {
		encodedCertificate, encodedKey, encodeErr := encodeCertificate(certificate)
		if encodeErr != nil {
			self.recordFailure(state)
			return encodeErr
		}
		if saveErr := self.settings.SaveCertificate(state.key, encodedCertificate, encodedKey); saveErr != nil {
			// The certificate is usable right now either way; failing to keep
			// it only means obtaining another after a restart. Not a failure
			// worth backing off from, because the name validated.
			log.Warningf("failed to save the certificate for %v, so a new one will be obtained after a restart: %s", state.hosts, saveErr)
		}
	}

	self.certificateMutex.Lock()
	defer self.certificateMutex.Unlock()
	state.certificate = certificate
	state.failures = 0
	state.nextAttempt = time.Time{}
	log.Noticef("obtained a certificate for %v", state.hosts)
	return nil
}

func (self *manager) recordFailure(state *certificateState) {
	self.certificateMutex.Lock()
	defer self.certificateMutex.Unlock()

	state.failures++
	wait := requestBackoff << min(state.failures-1, 16)
	if wait > requestBackoffMax || wait <= 0 {
		wait = requestBackoffMax
	}
	state.nextAttempt = time.Now().Add(wait)
	log.Warningf("not retrying the certificate for %v for %s (%d failures)", state.hosts, wait, state.failures)
}

// decodeCertificate parses a stored certificate. Empty input is not an error:
// that is simply a server that has not obtained one yet.
func decodeCertificate(certificate, privateKey string) (*tls.Certificate, error) {
	if strings.TrimSpace(certificate) == "" || strings.TrimSpace(privateKey) == "" {
		return &tls.Certificate{}, nil
	}
	parsed, err := tls.X509KeyPair([]byte(certificate), []byte(privateKey))
	if err != nil {
		log.Errorf("failed to parse the stored certificate: %s", err)
		return nil, err
	}
	return &parsed, nil
}

// encodeCertificate renders a certificate as the two PEM blobs that go into
// the configuration.
func encodeCertificate(certificate *tls.Certificate) (string, string, error) {
	key, ok := certificate.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return "", "", ErrInvalidCertificate
	}

	var privateKey bytes.Buffer
	if err := pem.Encode(&privateKey, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return "", "", err
	}

	var chain bytes.Buffer
	for _, block := range certificate.Certificate {
		if err := pem.Encode(&chain, &pem.Block{Type: "CERTIFICATE", Bytes: block}); err != nil {
			return "", "", err
		}
	}
	return chain.String(), privateKey.String(), nil
}

func validateCertificate(certificate *tls.Certificate, hosts []string) (time.Time, error) {
	// parse certificate and get the leaf
	var raw []byte
	for _, block := range certificate.Certificate {
		raw = append(raw, block...)
	}
	certificates, err := x509.ParseCertificates(raw)
	if err != nil {
		log.Errorf("failed to parse certificate: %s", err)
		return time.Time{}, ErrInvalidCertificate
	}
	if len(certificates) == 0 {
		log.Errorf("certificate is empty")
		return time.Time{}, ErrInvalidCertificate
	}
	leaf := certificates[0]

	// verify the leaf is not expired and matches the domain name
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		log.Errorf("certificate not valid until %s", leaf.NotBefore)
		return time.Time{}, ErrInvalidCertificate
	}
	if now.After(leaf.NotAfter) {
		log.Errorf("certificate already expired since %s", leaf.NotAfter)
		return time.Time{}, ErrInvalidCertificate
	}
	for _, host := range hosts {
		if err := leaf.VerifyHostname(host); err != nil {
			log.Errorf("certificate does not work for host %q: %s", host, err)
			return time.Time{}, err
		}
	}

	// ensure the leaf corresponds to the private key and matches the certKey type
	switch publicKey := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		rsaPrivateKey, ok := certificate.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			log.Errorf("private key is not rsa")
			return time.Time{}, ErrInvalidCertificate
		}
		if publicKey.N.Cmp(rsaPrivateKey.N) != 0 {
			log.Errorf("private key is invalid for this certificate")
			return time.Time{}, ErrInvalidCertificate
		}
	default:
		log.Errorf("certificate is not rsa")
		return time.Time{}, ErrInvalidCertificate
	}
	return leaf.NotAfter, nil
}

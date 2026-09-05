package autoacme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

func (self *manager) requestAcmeCertificate(ctx context.Context, hosts []string, challenge string) (*tls.Certificate, error) {
	log.Debugf("requesting acme certificate for %s over %s", hosts, challenge)

	solver, err := self.solverFor(challenge)
	if err != nil {
		return nil, err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Errorf("failed to generate private key: %s", err)
		return nil, err
	}

	csr, err := self.createAcmeCertificateRequest(privateKey, hosts)
	if err != nil {
		log.Errorf("failed to create certificate request: %s", err)
		return nil, err
	}

	client, err := self.ensureAcmeClient(ctx)
	if err != nil {
		log.Errorf("failed to ensure acme client: %s", err)
		return nil, err
	}

	order, err := self.verifyAcme(ctx, client, hosts, solver)
	if err != nil {
		log.Errorf("failed to verify with acme: %s", err)
		return nil, err
	}

	certificate, url, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, err
	}

	log.Debugf("successfully requested acme certificate %q", url)
	return &tls.Certificate{
		PrivateKey:  privateKey,
		Certificate: certificate,
	}, nil
}

func (self *manager) createAcmeCertificateRequest(privateKey crypto.Signer, hosts []string) ([]byte, error) {
	return x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: hosts[0],
		},
		DNSNames: hosts,
	}, privateKey)
}

// orderAttempts is how many times an order is tried before giving up until
// the next scheduled attempt.
//
// There used to be no ceiling and no pause. A failure that does not resolve
// itself — a challenge that cannot be answered, a name that does not resolve —
// therefore became a tight loop creating orders as fast as the network
// allowed, which is the surest way to reach a certificate authority's
// failed-validation limit and stay there. Giving up and waiting for the next
// scheduled run costs nothing: the manager runs every five minutes anyway.
const orderAttempts = 3

// orderBackoff is the pause after a failed attempt, doubled each time.
const orderBackoff = 15 * time.Second

func (self *manager) verifyAcme(ctx context.Context, client *acme.Client, hosts []string, solver Solver) (*acme.Order, error) {
	backoff := orderBackoff
	for attempt := 1; ; attempt++ {
		if attempt > 1 {
			log.Warningf("retrying the acme order in %s (attempt %d of %d)", backoff, attempt, orderAttempts)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		if attempt > orderAttempts {
			return nil, fmt.Errorf("autoacme: %w after %d attempts", ErrOrderFailed, orderAttempts)
		}

		order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(hosts...))
		if err != nil {
			log.Errorf("failed to authorize order: %s", err)
			return nil, err
		}
		log.Debugf("authorizing order: %q", order.URI)

		// remove all hanging authorizations to reduce rate limit quotas after we're done.
		defer func(urls []string) {
			go self.cleanupAcme(context.Background(), urls)
		}(order.AuthzURLs)

		// check if there's actually anything we need to do
		switch order.Status {
		case acme.StatusReady:
			// already authorized
			return order, nil
		case acme.StatusPending:
			// normal
		default:
			log.Errorf("invalid new order status %q, order %q", order.Status, order.URI)
			return nil, ErrInvalidOrderStatus
		}

		challenges := make([]Challenge, 0, len(order.AuthzURLs))
		authorizations := make([]*acme.Authorization, 0, len(order.AuthzURLs))
		for _, url := range order.AuthzURLs {
			authorization, err := client.GetAuthorization(ctx, url)
			if err != nil {
				log.Errorf("failed to get authorization %q: %s", url, err)
				return nil, err
			}
			if authorization.Status != acme.StatusPending {
				continue
			}

			// Pick the challenge type this server is configured to answer.
			// The authority offers several; we can only satisfy one.
			var challenge *acme.Challenge
			for _, offered := range authorization.Challenges {
				if offered.Type == solver.Type() {
					challenge = offered
					break
				}
			}
			if challenge == nil {
				log.Errorf("cannot satisfy %q: the certificate authority did not offer a %s challenge", authorization.URI, solver.Type())
				return nil, ErrInvalidChallenges
			}

			challenges = append(challenges, Challenge{
				Domain:    authorization.Identifier.Value,
				Challenge: challenge,
			})
			authorizations = append(authorizations, authorization)
		}

		// respond to all challenges
		if len(challenges) > 0 {
			log.Debugf("fulfilling %d %s challenges for order %q", len(challenges), solver.Type(), order.URI)
			if err := solver.Present(ctx, client, challenges); err != nil {
				log.Warningf("failed to present %s challenges: %s", solver.Type(), err)
				if err := solver.CleanUp(context.WithoutCancel(ctx), challenges); err != nil {
					log.Warningf("failed to clean up %s challenges: %s", solver.Type(), err)
				}
				continue // retry
			}
		}

		// fulfilled, now tell ca, wait for validation result
		var retry bool
		for index, challenge := range challenges {
			if _, err := client.Accept(ctx, challenge.Challenge); err != nil {
				log.Warningf("failed to accept the %s challenge for %q: %s", solver.Type(), challenge.Domain, err)
				retry = true
				break
			}
			if _, err := client.WaitAuthorization(ctx, authorizations[index].URI); err != nil {
				log.Warningf("failed to wait for authorization %q: %s", authorizations[index].URI, err)
				retry = true
				break
			}
		}

		// The responses are no longer needed either way: on success the
		// authority has already validated them, on failure they are stale.
		if len(challenges) > 0 {
			if err := solver.CleanUp(context.WithoutCancel(ctx), challenges); err != nil {
				log.Warningf("failed to clean up %s challenges: %s", solver.Type(), err)
			}
		}
		if retry {
			continue // retry
		}

		// all authorizations are satisfied
		// wait for the ca to update the order status
		order, err = client.WaitOrder(ctx, order.URI)
		if err != nil {
			log.Warningf("failed to wait for order %q: %s", order.URI, err)
			continue // retry
		}
		return order, nil
	}
}

func (self *manager) ensureAcmeClient(ctx context.Context) (*acme.Client, error) {
	self.acmeClientMutex.Lock()
	defer self.acmeClientMutex.Unlock()

	if self.acmeClient != nil {
		return self.acmeClient, nil
	}

	key, err := self.loadOrCreateAccountKey()
	if err != nil {
		return nil, err
	}

	directoryUrl := self.settings.DirectoryURL
	if directoryUrl == "" {
		directoryUrl = LetsEncryptDirectoryURL
	}
	client := &acme.Client{
		DirectoryURL: directoryUrl,
		Key:          key,
		UserAgent:    "teanode",
	}
	account := &acme.Account{Contact: []string{
		"mailto:" + self.settings.ACMEEmail,
	}}
	if _, err := client.Register(ctx, account, func(string) bool {
		return true
	}); err != nil {
		if errors.Is(err, acme.ErrAccountAlreadyExists) {
		} else if err2, ok := err.(*acme.Error); ok && err2.StatusCode == http.StatusConflict {

		} else {
			log.Errorf("failed to register acme account: %s", err)
			return nil, err
		}
	}
	self.acmeClient = client
	return client, nil
}

// cleanupAcme relinquishes all authorizations identified by the elements
// of the provided url slice which are in "pending" state.
// It ignores revocation errors.
//
// cleanupAcme takes no context argument and instead runs with its own
// "detached" context because deactivations are done in a goroutine separate from
// that of the main issuance or renewal flow.
func (self *manager) cleanupAcme(ctx context.Context, urls []string) {
	defer deferutil.Recover()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	client, err := self.ensureAcmeClient(ctxWithTimeout)
	if err != nil {
		return
	}

	for _, url := range urls {
		if authorization, err := client.GetAuthorization(ctxWithTimeout, url); err == nil && authorization.Status == acme.StatusPending {
			_ = client.RevokeAuthorization(ctxWithTimeout, url)
		}
	}
}

// loadOrCreateAccountKey returns the key identifying this server to the
// certificate authority, generating one the first time.
//
// It is kept in the configuration rather than in a file of its own, so that
// backing up the configuration backs up everything that matters. Losing it is
// survivable — the server simply registers again — but registration is rate
// limited, so it is worth keeping.
func (self *manager) loadOrCreateAccountKey() (crypto.Signer, error) {
	if encoded := strings.TrimSpace(self.settings.AccountKey); encoded != "" {
		block, _ := pem.Decode([]byte(encoded))
		if block == nil {
			return nil, ErrInvalidACMEKey
		}
		if !strings.Contains(block.Type, "PRIVATE") {
			return nil, ErrInvalidACMEKey
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("autoacme: cannot parse the account key: %w", err)
		}
		return key, nil
	}

	log.Debugf("generating an ACME account key")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("autoacme: cannot generate an account key: %w", err)
	}

	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("autoacme: cannot encode the account key: %w", err)
	}
	pemEncoded := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}))

	if self.settings.SaveAccountKey != nil {
		if err := self.settings.SaveAccountKey(pemEncoded); err != nil {
			// Not fatal: the certificate can still be obtained, the key is
			// just forgotten at restart.
			log.Warningf("failed to save the ACME account key, so a new account will be registered next time: %s", err)
		}
	}
	self.settings.AccountKey = pemEncoded
	return key, nil
}

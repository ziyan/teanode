package autoacme

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

// A certificate that is present and usable, without the machinery of really
// issuing one: selection only looks at whether there are any bytes.
func stubCertificate() *tls.Certificate {
	return &tls.Certificate{Certificate: [][]byte{{0x30, 0x00}}}
}

func managerWith(t *testing.T, states ...*certificateState) *manager {
	t.Helper()
	return &manager{settings: &Settings{}, certificates: states}
}

// The point of the whole change: a sender connecting to one domain is handed
// that domain's certificate, not the one belonging to whichever domain the
// server happens to be named after.
func TestGetCertificateAnswersTheNameAsked(t *testing.T) {
	t.Parallel()

	server := &certificateState{key: "", hosts: []string{"example.com", "*.example.com"}, certificate: stubCertificate()}
	first := &certificateState{key: "one", hosts: []string{"mx.one.test"}, certificate: stubCertificate()}
	second := &certificateState{key: "two", hosts: []string{"mx.two.test"}, certificate: stubCertificate()}
	manager := managerWith(t, server, first, second)

	tests := []struct {
		name string
		want *certificateState
	}{
		{"mx.one.test", first},
		{"MX.ONE.TEST", first},
		{"mx.two.test", second},
		// The server's own, exactly and through its wildcard.
		{"example.com", server},
		{"mail.example.com", server},
		// A name nobody has a certificate for, and no name at all, both get
		// the first — which is the server's own. Serving a name the sender did
		// not ask for is something almost every sender accepts; refusing the
		// handshake would refuse the mail.
		{"unknown.test", server},
		{"", server},
		// A wildcard covers one label, not two.
		{"a.b.example.com", server},
	}
	for _, test := range tests {
		got, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: test.name})
		if err != nil {
			t.Errorf("GetCertificate(%q): %s", test.name, err)
			continue
		}
		if got != test.want.certificate {
			t.Errorf("GetCertificate(%q) served the certificate for %v, want %v",
				test.name, "?", test.want.hosts)
		}
	}
}

// A domain whose certificate has not been obtained yet is served the server's
// own rather than nothing, because nothing means a failed handshake and a
// refused message.
func TestANameWithNoCertificateFallsBack(t *testing.T) {
	t.Parallel()

	server := &certificateState{hosts: []string{"example.com"}, certificate: stubCertificate()}
	pending := &certificateState{key: "one", hosts: []string{"mx.one.test"}, certificate: &tls.Certificate{}}
	manager := managerWith(t, server, pending)

	got, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: "mx.one.test"})
	if err != nil {
		t.Fatalf("GetCertificate: %s", err)
	}
	if got != server.certificate {
		t.Error("a name with no certificate of its own was not served the server's")
	}
}

// And with nothing at all, the handshake fails with the error it always did
// rather than with an empty certificate, which fails obscurely.
func TestNoCertificateAtAll(t *testing.T) {
	t.Parallel()

	manager := managerWith(t, &certificateState{hosts: []string{"example.com"}, certificate: &tls.Certificate{}})
	if _, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.com"}); !errors.Is(err, ErrNoCertificate) {
		t.Errorf("got %v, want ErrNoCertificate", err)
	}
}

// One domain that cannot be validated must not spend the certificate
// authority's patience on behalf of the others, and must not stop them being
// renewed either.
func TestAFailingCertificateBacksOffAlone(t *testing.T) {
	t.Parallel()

	failing := &certificateState{key: "one", hosts: []string{"mx.one.test"}, certificate: &tls.Certificate{}}
	healthy := &certificateState{key: "two", hosts: []string{"mx.two.test"}, certificate: &tls.Certificate{}}
	manager := managerWith(t, failing, healthy)

	// Both want a certificate to begin with: neither has one.
	if !manager.shouldRequestCertificate(failing) || !manager.shouldRequestCertificate(healthy) {
		t.Fatal("a certificate that has never been obtained should be requested")
	}

	manager.recordFailure(failing)
	if manager.shouldRequestCertificate(failing) {
		t.Error("a certificate that just failed was retried immediately")
	}
	if !manager.shouldRequestCertificate(healthy) {
		t.Error("one certificate failing stopped another from being requested")
	}

	// The wait grows, so a name that is broken rather than briefly unlucky is
	// asked about less and less.
	first := failing.nextAttempt
	manager.recordFailure(failing)
	if !failing.nextAttempt.After(first) {
		t.Error("the wait did not grow after a second failure")
	}

	// And it is capped, rather than doubling into the far future.
	for range 40 {
		manager.recordFailure(failing)
	}
	if wait := time.Until(failing.nextAttempt); wait > requestBackoffMax+time.Minute {
		t.Errorf("the wait grew to %s, past the cap of %s", wait, requestBackoffMax)
	}

	// A success clears it.
	manager.certificateMutex.Lock()
	failing.failures = 0
	failing.nextAttempt = time.Time{}
	manager.certificateMutex.Unlock()
	if !manager.shouldRequestCertificate(failing) {
		t.Error("a certificate that recovered was not requested again")
	}
}

// A certificate is asked for again a month before it expires, is left alone
// until then, and is asked for again immediately if it does not cover the
// names it is supposed to.
func TestRenewalWindow(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		hosts  []string
		expiry time.Duration
		want   bool
	}{
		{"expires tomorrow", []string{"mx.one.test"}, 24 * time.Hour, true},
		{"expires in a fortnight", []string{"mx.one.test"}, 14 * 24 * time.Hour, true},
		{"expires in three months", []string{"mx.one.test"}, 90 * 24 * time.Hour, false},
		// Already expired, which is what a server that was switched off for a
		// season comes back to.
		{"expired last week", []string{"mx.one.test"}, -7 * 24 * time.Hour, true},
		// Covers the wrong name, which happens when a domain is given a name
		// it did not have when the certificate was obtained.
		{"a name it does not cover", []string{"mx.other.test"}, 90 * 24 * time.Hour, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &certificateState{
				key:         "one",
				hosts:       []string{"mx.one.test"},
				certificate: selfSigned(t, test.hosts, test.expiry),
			}
			manager := managerWith(t, state)
			if got := manager.shouldRequestCertificate(state); got != test.want {
				t.Errorf("shouldRequestCertificate() = %v, want %v", got, test.want)
			}
		})
	}
}

// selfSigned builds a certificate the manager can read, so the renewal
// decision can be tested against a real expiry rather than against
// arithmetic.
func selfSigned(t *testing.T, hosts []string, validFor time.Duration) *tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %s", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: hosts[0]},
		DNSNames:     hosts,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %s", err)
	}
	return &tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}
}

// Covers is what stops a second certificate being ordered for a name the
// server's own already serves. The first attempt compared names as strings,
// so a wildcard matched nothing and a duplicate was ordered for two names the
// wildcard beside it already covered.
func TestCovers(t *testing.T) {
	t.Parallel()

	hosts := []string{"example.com", "*.example.com"}
	for _, name := range []string{"example.com", "mx.example.com", "MX.Example.Com"} {
		if !Covers(hosts, name) {
			t.Errorf("Covers(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"mx.example.net",
		// A wildcard is one label, so a name two below it is not covered.
		"a.mx.example.com",
		"",
	} {
		if Covers(hosts, name) {
			t.Errorf("Covers(%q) = true, want false", name)
		}
	}

	// A server whose certificate names no wildcard covers only what it lists.
	exact := []string{"mail.example.com"}
	if Covers(exact, "mx.example.com") {
		t.Error("an exact list covered a name it does not contain")
	}
	if !Covers(exact, "mail.example.com") {
		t.Error("an exact list did not cover the name it contains")
	}
}

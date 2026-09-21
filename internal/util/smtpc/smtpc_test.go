package smtpc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/smtpc"
	"github.com/ziyan/teanode/internal/util/smtpd"
)

type testLocator struct{}

func (self *testLocator) Locate(net.IP) *geoip.Location {
	return nil
}

type testResolver struct{}

func (self *testResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	switch host {
	case "localhost":
		return []net.IPAddr{
			{
				IP:   net.ParseIP("127.0.0.1"),
				Zone: "",
			},
		}, nil
	}
	return nil, fmt.Errorf("host %q not found", host)
}

func (self *testResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	switch addr {
	case "127.0.0.1":
		return []string{"localhost"}, nil
	}
	return nil, fmt.Errorf("ip %q unknown", addr)
}

type testDropper struct{}

func (self *testDropper) Drop(ip net.IP) (bool, error) {
	return false, nil
}

func (self *testDropper) Close() error {
	return nil
}

func generateTestTlsConfig() (*tls.Config, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	publicKey := privateKey.PublicKey
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &publicKey, privateKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{der},
				PrivateKey:  privateKey,
			},
		},
	}, nil
}

var testSecret = []byte("test_secret")
var testEndpoint = "127.0.0.1:32526"
var testMail = []byte("Subject: Test\n\nHello world!\n")

func TestServe(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

	// generate tls config
	tlsConfig, err := generateTestTlsConfig()
	if err != nil {
		t.Fatalf("failed to generate tls config: %s", err)
	}

	// listen
	listener, err := net.Listen("tcp", testEndpoint)
	if err != nil {
		t.Fatalf("cannot create listener: %s", err)
	}
	defer func() { _ = listener.Close() }()

	// handle incoming mail
	var sentEnvelope *mailparse.Envelope
	handle := func(ctx context.Context, envelope *mailparse.Envelope) error {
		t.Logf("received: envelope = %v", envelope)
		sentEnvelope = envelope
		return nil
	}

	// serve in the background
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()

		_ = smtpd.Serve(listener, handle, &testLocator{}, &testResolver{}, &testDropper{}, &smtpd.Settings{
			Outgoing:       false,
			Greeting:       "localhost Test/1.2.3",
			Timeout:        3 * time.Second,
			MaxSize:        1024,
			MaxRecipients:  3,
			TLSConfig:      tlsConfig,
			Secret:         testSecret,
			TrustedSenders: []string{"localhost"},
			Delay:          time.Millisecond,
		})
	}()

	// send mail
	func() {
		conn, err := net.Dial("tcp", testEndpoint)
		if err != nil {
			t.Fatalf("failed to dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		if err := smtpc.Send(context.TODO(), conn, "", "", "sender@localhost", []string{"recipient@localhost"}, testMail, &smtpc.Settings{
			Hello:   "localhost",
			Timeout: 3 * time.Second,
		}); err != nil {
			t.Fatalf("failed to send mail: %s", err)
		}
	}()

	// validate
	if sentEnvelope == nil {
		t.Fatalf("no envelope received")
	}
	if sentEnvelope.RDNS != "localhost" {
		t.Fatalf("wrong rdns: %s", sentEnvelope.RDNS)
	}
	if sentEnvelope.Hello != "localhost" {
		t.Fatalf("wrong hello: %s", sentEnvelope.Hello)
	}
	if sentEnvelope.Sender != "sender@localhost" {
		t.Fatalf("wrong sender: %s", sentEnvelope.Sender)
	}
	if len(sentEnvelope.Recipients) != 1 || sentEnvelope.Recipients[0] != "recipient@localhost" {
		t.Fatalf("wrong recipients: %s", sentEnvelope.Recipients)
	}
	if sentEnvelope.Size != uint64(len(testMail)) {
		t.Fatalf("wrong size: %d != %d", sentEnvelope.Size, len(testMail))
	}

	// try auth
	func() {
		conn, err := net.Dial("tcp", testEndpoint)
		if err != nil {
			t.Fatalf("failed to dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		if err := smtpc.Send(context.TODO(), conn, "username", "password", "sender@localhost", []string{"recipient@localhost"}, testMail, &smtpc.Settings{
			Hello:   "localhost",
			Timeout: 3 * time.Second,
		}); err == nil {
			t.Fatalf("auth should not be allowed: %s", err)
		}
	}()
}

package smtpd_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
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
var testKey = "0123456789abcdef"
var testListenAddr = "127.0.0.1:0"
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
	listener, err := net.Listen("tcp", testListenAddr)
	if err != nil {
		t.Fatalf("cannot create listener: %s", err)
	}
	defer func() { _ = listener.Close() }()
	endpoint := listener.Addr().String()

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
		conn, err := net.Dial("tcp", endpoint)
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
		conn, err := net.Dial("tcp", endpoint)
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

func TestAuth(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

	// generate cred
	credentialId := security.NewULID()
	username, password, err := security.EncodeCredential(credentialId, testKey, testSecret)
	if err != nil {
		t.Fatalf("failed to generate credential: %s", err)
	}

	// generate tls config
	tlsConfig, err := generateTestTlsConfig()
	if err != nil {
		t.Fatalf("failed to generate tls config: %s", err)
	}

	// listen
	listener, err := net.Listen("tcp", testListenAddr)
	if err != nil {
		t.Fatalf("cannot create listener: %s", err)
	}
	defer func() { _ = listener.Close() }()
	endpoint := listener.Addr().String()

	// handle incoming mail
	var sentEnvelope *mailparse.Envelope
	handle := func(ctx context.Context, envelope *mailparse.Envelope) error {
		sentEnvelope = envelope
		return nil
	}

	// serve in the background
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()

		_ = smtpd.Serve(listener, handle, &testLocator{}, &testResolver{}, &testDropper{}, &smtpd.Settings{
			Outgoing:       true,
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

	// send mail with validate credential
	func() {
		conn, err := net.Dial("tcp", endpoint)
		if err != nil {
			t.Fatalf("failed to dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		if err := smtpc.Send(context.TODO(), conn, username, password, "sender@localhost", []string{"recipient@localhost"}, testMail, &smtpc.Settings{
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
	if sentEnvelope.CredentialID != credentialId {
		t.Fatalf("incorrect credential id: %q != %q", sentEnvelope.CredentialID, credentialId)
	}
	if sentEnvelope.CredentialKey != testKey {
		t.Fatalf("incorrect credential id: %q != %q", sentEnvelope.CredentialKey, testKey)
	}

	// send mail with invalidate credential
	func() {
		conn, err := net.Dial("tcp", endpoint)
		if err != nil {
			t.Fatalf("failed to dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		if err := smtpc.Send(context.TODO(), conn, username, password[:len(password)-1]+"!", "sender@localhost", []string{"recipient@localhost"}, testMail, &smtpc.Settings{
			Hello:   "localhost",
			Timeout: 3 * time.Second,
		}); err == nil {
			t.Fatalf("should not pass auth: %s", err)
		}
	}()
}

// A command line that never ends used to be buffered until the read
// deadline, at whatever rate the client sent it. Now it is refused once it
// is longer than any command has a reason to be.
func TestAnEndlessCommandLineIsRefused(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

	listener, err := net.Listen("tcp", testListenAddr)
	if err != nil {
		t.Fatalf("cannot create listener: %s", err)
	}
	defer func() { _ = listener.Close() }()

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		_ = smtpd.Serve(listener, func(ctx context.Context, envelope *mailparse.Envelope) error { return nil }, &testLocator{}, &testResolver{}, &testDropper{}, &smtpd.Settings{
			Greeting:       "localhost Test/1.2.3",
			Timeout:        3 * time.Second,
			MaxSize:        1024,
			MaxRecipients:  3,
			Secret:         testSecret,
			TrustedSenders: []string{"localhost"},
		})
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %s", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("no greeting: %s", err)
	}

	// A megabyte with no newline in it, written until the server stops
	// reading; the write itself may fail once it has.
	chunk := bytes.Repeat([]byte("x"), 64*1024)
	for written := 0; written < 1024*1024; written += len(chunk) {
		if _, err := conn.Write(chunk); err != nil {
			break
		}
	}
	reply, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply to a line too long: %s", err)
	}
	if !strings.HasPrefix(reply, "500 ") {
		t.Errorf("got %q, want a 500", reply)
	}
}

// Past the connection limit a client is told to come back later, rather
// than served, so the memory a connection holds is bounded by a setting
// instead of by how many a stranger opens.
func TestConnectionsPastTheLimitAreRefused(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

	listener, err := net.Listen("tcp", testListenAddr)
	if err != nil {
		t.Fatalf("cannot create listener: %s", err)
	}
	defer func() { _ = listener.Close() }()

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		_ = smtpd.Serve(listener, func(ctx context.Context, envelope *mailparse.Envelope) error { return nil }, &testLocator{}, &testResolver{}, &testDropper{}, &smtpd.Settings{
			Greeting:       "localhost Test/1.2.3",
			Timeout:        3 * time.Second,
			MaxSize:        1024,
			MaxRecipients:  3,
			Secret:         testSecret,
			TrustedSenders: []string{"localhost"},
			MaxConnections: 1,
		})
	}()

	greeting := func() string {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("failed to dial: %s", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reply, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("no greeting: %s", err)
		}
		return reply
	}
	if first := greeting(); !strings.HasPrefix(first, "220 ") {
		t.Fatalf("the first connection was answered %q, want 220", first)
	}
	if second := greeting(); !strings.HasPrefix(second, "421 ") {
		t.Errorf("the second connection was answered %q, want 421", second)
	}
}

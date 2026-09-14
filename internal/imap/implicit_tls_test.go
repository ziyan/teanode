package imap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// A mail program on the implicit-TLS port can sign in.
//
// go-imap decides both "may this connection authenticate" and "should
// STARTTLS be offered" by asserting that the connection is a *tls.Conn. A
// wrapper around an accepted connection defeats the assertion, and a
// connection ceiling put on above the TLS listener was exactly that: every
// connection to port 993 looked like plaintext, so the server answered a
// finished TLS session with STARTTLS and LOGINDISABLED and refused every
// sign-in. It shipped, because what coverage there was exercised 143, where
// the listener is genuinely plaintext and STARTTLS is genuinely right.
//
// This asks the listener what it says about itself, which is the whole of
// the bug: nothing here needs a database, a mailbox or a password.
func TestTheImplicitTLSPortDoesNotAskForSTARTTLS(t *testing.T) {
	t.Parallel()

	certificate := selfSigned(t)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer func() { _ = raw.Close() }()

	// The listener as Serve builds it for port 993: the ceiling first, the
	// encryption over it. Built here rather than reached through Serve so
	// the test needs no database.
	var listener net.Listener = &boundedListener{Listener: raw, held: make(chan struct{}, 4)}
	listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}})

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if handshake, ok := conn.(interface{ Handshake() error }); ok {
			_ = handshake.Handshake()
		}
		// What go-imap asks of the connection it was handed, and the only
		// question this test is about.
		_, isTLS := conn.(*tls.Conn)
		if isTLS {
			_, _ = conn.Write([]byte("TLS\r\n"))
		} else {
			_, _ = conn.Write([]byte("PLAINTEXT\r\n"))
		}
	}()

	client, err := tls.Dial("tcp", raw.Addr().String(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("dial: %s", err)
	}
	defer func() { _ = client.Close() }()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))
	answer := make([]byte, 32)
	read, err := client.Read(answer)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	if said := strings.TrimSpace(string(answer[:read])); said != "TLS" {
		t.Fatalf("the server sees %s on the implicit-TLS port; it must see a *tls.Conn, "+
			"or it offers STARTTLS and LOGINDISABLED inside a finished TLS session", said)
	}
}

// And the ceiling still gives places back, which is what it is for.
func TestTheCeilingReleasesUnderTLS(t *testing.T) {
	t.Parallel()

	held := make(chan struct{}, 1)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer func() { _ = raw.Close() }()
	bounded := &boundedListener{Listener: raw, held: held}

	go func() {
		conn, err := net.Dial("tcp", raw.Addr().String())
		if err == nil {
			_ = conn.Close()
		}
	}()
	accepted, err := bounded.Accept()
	if err != nil {
		t.Fatalf("accept: %s", err)
	}
	if len(held) != 1 {
		t.Fatalf("a served connection holds a place: %d", len(held))
	}
	_ = accepted.Close()
	_ = accepted.Close()
	if len(held) != 0 {
		t.Fatalf("closing gives the place back, once however often it is closed: %d", len(held))
	}
}

// selfSigned is a certificate for the test's own listener.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %s", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "imap.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"imap.test"},
	}
	encoded, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %s", err)
	}
	return tls.Certificate{Certificate: [][]byte{encoded}, PrivateKey: key}
}

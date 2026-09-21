package smtpc_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/smtpc"
)

// TestTLSModes covers what a relay needs and delivering to a stranger's MX
// does not: insisting on encryption, and checking who answered.
//
// Driven against a hand-written server rather than smtpd, because what is
// being tested is how this client behaves when the other end will not do what
// it asked — which a working server cannot demonstrate.
func TestTLSModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		offerTLS    bool
		mode        smtpc.TLSMode
		serverName  string
		wantErr     string
		wantEncrypt bool
	}{
		{
			name:        "opportunistic uses STARTTLS when it is offered",
			offerTLS:    true,
			mode:        smtpc.TLSOpportunistic,
			wantEncrypt: true,
		},
		{
			// The reason this mode exists: most of the internet's mail
			// servers present a certificate for the wrong name, and refusing
			// them would mean not delivering the mail.
			name:     "opportunistic accepts a server that will not encrypt",
			offerTLS: false,
			mode:     smtpc.TLSOpportunistic,
		},
		{
			name:        "required encrypts when it can",
			offerTLS:    true,
			mode:        smtpc.TLSRequired,
			wantEncrypt: true,
		},
		{
			// A relay was configured to encrypt and does not. Continuing
			// would put the password and the message on the wire in the
			// clear, so this is a refusal rather than a fallback.
			name:     "required refuses a server that will not encrypt",
			offerTLS: false,
			mode:     smtpc.TLSRequired,
			wantErr:  "does not offer STARTTLS",
		},
		{
			// The certificate is for "localhost" and is trusted by the test
			// below, so this fails on the name alone. Without the check, the
			// password goes to whoever answered.
			name:       "a certificate for the wrong name is refused",
			offerTLS:   true,
			mode:       smtpc.TLSRequired,
			serverName: "not-the-name.example",
			wantErr:    "not-the-name.example",
		},
		{
			name:        "a certificate for the right name is accepted",
			offerTLS:    true,
			mode:        smtpc.TLSRequired,
			serverName:  "localhost",
			wantEncrypt: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encrypted, err := converse(t, test.offerTLS, test.mode, test.serverName)

			switch {
			case test.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %s", err)
			case test.wantErr != "" && err == nil:
				t.Fatalf("expected an error containing %q", test.wantErr)
			case test.wantErr != "" && !strings.Contains(err.Error(), test.wantErr):
				t.Fatalf("error is %q, want it to mention %q", err, test.wantErr)
			}
			if err == nil && encrypted != test.wantEncrypt {
				t.Errorf("encrypted=%v, want %v", encrypted, test.wantEncrypt)
			}
		})
	}
}

// TestImplicitTLS covers port 465, where the handshake comes before the
// banner and there is no STARTTLS to negotiate.
func TestImplicitTLS(t *testing.T) {
	t.Parallel()

	serverTLS, roots, err := testCertificate()
	if err != nil {
		t.Fatalf("generating a certificate: %s", err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer func() { _ = listener.Close() }()

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		speak(conn, nil)
	}()

	func() {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		if err := smtpc.Send(context.TODO(), conn, "", "", "sender@localhost", []string{"recipient@localhost"},
			[]byte("Subject: hello\r\n\r\nbody\r\n"), &smtpc.Settings{
				Hello:      "localhost",
				Timeout:    3 * time.Second,
				TLS:        smtpc.TLSImplicit,
				ServerName: "localhost",
				RootCAs:    roots,
			}); err != nil {
			t.Fatalf("sending over implicit TLS: %s", err)
		}
	}()
	waitGroup.Wait()
}

// converse runs one exchange against a server that either offers STARTTLS or
// does not, and reports whether the client ended up encrypted.
func converse(t *testing.T, offerTLS bool, mode smtpc.TLSMode, serverName string) (bool, error) {
	t.Helper()

	settings := &smtpc.Settings{
		Hello:      "localhost",
		Timeout:    3 * time.Second,
		TLS:        mode,
		ServerName: serverName,
	}

	var serverTLS *tls.Config
	if offerTLS {
		generated, roots, err := testCertificate()
		if err != nil {
			t.Fatalf("generating a certificate: %s", err)
		}
		serverTLS, settings.RootCAs = generated, roots
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer func() { _ = listener.Close() }()

	encrypted := make(chan bool, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			encrypted <- false
			return
		}
		defer func() { _ = conn.Close() }()
		encrypted <- speak(conn, serverTLS)
	}()

	// Closed as soon as the exchange is over: the server reads until the
	// connection goes away, so waiting for its verdict first would deadlock.
	sendError := func() error {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatalf("dial: %s", err)
		}
		defer func() { _ = conn.Close() }()

		return smtpc.Send(context.TODO(), conn, "", "", "sender@localhost", []string{"recipient@localhost"},
			[]byte("Subject: hello\r\n\r\nbody\r\n"), settings)
	}()

	select {
	case value := <-encrypted:
		return value, sendError
	case <-time.After(5 * time.Second):
		return false, sendError
	}
}

// speak is the smallest SMTP server that can complete a transaction. A nil
// configuration means it will not offer STARTTLS, which is the case the
// required mode has to refuse.
func speak(conn net.Conn, serverTLS *tls.Config) bool {
	offerTLS := serverTLS != nil
	var upgraded bool
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 localhost ready")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return upgraded
		}
		command := strings.ToUpper(strings.TrimSpace(line))

		switch {
		case strings.HasPrefix(command, "EHLO"):
			if offerTLS && !upgraded {
				write("250-localhost")
				write("250 STARTTLS")
			} else {
				write("250 localhost")
			}
		case command == "STARTTLS":
			write("220 go ahead")
			tlsConn := tls.Server(conn, serverTLS)
			if err := tlsConn.Handshake(); err != nil {
				return upgraded
			}
			conn = tlsConn
			reader = bufio.NewReader(tlsConn)
			write = func(line string) { _, _ = tlsConn.Write([]byte(line + "\r\n")) }
			upgraded = true
		case strings.HasPrefix(command, "MAIL"), strings.HasPrefix(command, "RCPT"):
			write("250 ok")
		case command == "DATA":
			write("354 go ahead")
			for {
				body, err := reader.ReadString('\n')
				if err != nil {
					return upgraded
				}
				if strings.TrimSpace(body) == "." {
					break
				}
			}
			write("250 accepted")
		case command == "QUIT":
			write("221 bye")
			return upgraded
		default:
			write("250 ok")
		}
	}
}

// testCertificate returns a server configuration and the pool that trusts it.
//
// The certificate is self-signed, so a client checking it needs to be told to
// trust it — otherwise every verification test would fail on the chain and
// prove nothing about the name.
func testCertificate() (*tls.Config, *x509.CertPool, error) {
	serverTLS, err := generateTestTlsConfig()
	if err != nil {
		return nil, nil, err
	}
	parsed, err := x509.ParseCertificate(serverTLS.Certificates[0].Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return serverTLS, roots, nil
}

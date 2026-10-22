package imap

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// A connection that has not signed in may not make the server buffer much.
//
// The library caps a command at 50 KiB but not the literals inside it, and
// reads each one (up to 4 KiB) before it asks what state the connection is
// in, so thousands of literals to a SEARCH that was going to be refused cost
// the server megabytes per connection. The listener's wrapper cuts the
// connection once it has read more than bytesAllowedBeforeSignIn.
func TestAConnectionThatHasNotSignedInIsCutAfterAFewKilobytes(t *testing.T) {
	t.Parallel()

	server := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{settings: &Settings{}, hub: newHub(), gate: findBoundedConn(conn.NetConn())}, nil, nil
		},
		Caps: goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapLiteralPlus: {}},
	})
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(&boundedListener{Listener: raw}) }()
	defer func() { _ = server.Close() }()

	conn, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("greeting: %s", err)
	}

	payload := strings.Repeat("x", 4096)
	var command bytes.Buffer
	command.WriteString("a1 SEARCH")
	for range 3000 {
		command.WriteString(" TEXT {4096+}\r\n")
		command.WriteString(payload)
	}
	command.WriteString("\r\n")
	// The write may fail halfway because the server has already hung up,
	// which is the point.
	_, _ = conn.Write(command.Bytes())

	// Nothing but a closed connection comes back: the command was never
	// read to its end, so it is never answered.
	if line, err := reader.ReadString('\n'); err == nil {
		t.Fatalf("the server answered %q instead of closing the connection", line)
	}
}

// Once the session says the person has signed in, the allowance is gone.
func TestASignedInConnectionIsNotCut(t *testing.T) {
	t.Parallel()

	client, served := net.Pipe()
	defer func() { _ = client.Close() }()
	bounded := &boundedConn{Conn: served}
	defer func() { _ = bounded.Close() }()

	chunk := make([]byte, 64*1024)
	go func() {
		for range 4 {
			if _, err := client.Write(chunk); err != nil {
				return
			}
		}
	}()

	// Before sign-in: the third read passes the allowance.
	var total int
	var readErr error
	for total <= bytesAllowedBeforeSignIn && readErr == nil {
		var count int
		count, readErr = bounded.Read(make([]byte, 64*1024))
		total += count
	}
	if !errors.Is(readErr, errTooMuchBeforeSignIn) {
		t.Fatalf("read %d bytes before signing in, error %v", total, readErr)
	}

	client2, served2 := net.Pipe()
	defer func() { _ = client2.Close() }()
	signedIn := &boundedConn{Conn: served2}
	defer func() { _ = signedIn.Close() }()
	signedIn.isSignedIn.Store(true)
	go func() {
		for range 4 {
			if _, err := client2.Write(chunk); err != nil {
				return
			}
		}
	}()
	total = 0
	for total < 4*len(chunk) {
		count, err := signedIn.Read(make([]byte, 64*1024))
		if err != nil {
			t.Fatalf("a signed-in connection was cut after %d bytes: %s", total, err)
		}
		total += count
	}
}

// The wrapper is found through the TLS layer of the implicit-TLS port.
func TestFindBoundedConnLooksThroughTheWrappers(t *testing.T) {
	t.Parallel()

	client, served := net.Pipe()
	defer func() { _ = client.Close() }()
	bounded := &boundedConn{Conn: served}
	defer func() { _ = bounded.Close() }()

	if found := findBoundedConn(bounded); found != bounded {
		t.Errorf("a bare wrapper was not found")
	}
	if found := findBoundedConn(tls.Server(bounded, &tls.Config{})); found != bounded {
		t.Errorf("a wrapper under TLS was not found")
	}
	if found := findBoundedConn(client); found != nil {
		t.Errorf("a connection with no wrapper found %v", found)
	}
}

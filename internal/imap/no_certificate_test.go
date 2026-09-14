package imap

import (
	"context"
	"net"
	"strings"
	"testing"
)

// An implicit-TLS listener with no certificate refuses, and says what to do.
//
// Refusing is right: a port 993 that cannot authenticate is worse than one
// that is not there, which is what the old behaviour amounted to. But a
// listener that fails takes the whole server down with it, and what an
// operator saw was "shutting down: imaps listener stopped" three
// milliseconds after "teanode is running", with the reason logged at debug
// and the only warning on screen being about SMTP — which sends them the
// wrong way entirely.
func TestTheImplicitTLSListenerSaysWhatItNeeds(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer func() { _ = listener.Close() }()

	err = Serve(context.Background(), listener, &Settings{ImplicitTLS: true})
	if err == nil {
		t.Fatal("an implicit-TLS listener with no certificate has to refuse")
	}
	// The three ways out, because the reason alone leaves somebody guessing
	// which of them applies to their deployment.
	for _, wanted := range []string{"certificate", "tls.certificateFile", "tls.acme", "listen.imaps"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("the refusal does not mention %q: %s", wanted, err)
		}
	}
}

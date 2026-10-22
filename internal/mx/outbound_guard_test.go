package mx

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// guardedExchange is an exchange whose operator listed the given private
// addresses, which is how a deployment says a webhook inside its own network
// is meant.
func guardedExchange(allowed ...string) *exchange {
	configuration := config.Default()
	configuration.Agent.AllowPrivateAddresses = allowed
	return &exchange{config: config.NewMemoryStore(configuration), settings: &Settings{}}
}

func sendTestWebhook(exchange *exchange, url string) error {
	_, err := exchange.sendWebhook(context.Background(), url, "sender@example.org", "recipient@example.org", []string{"Subject: x\r\n"}, []byte("body"))
	return err
}

// A domain manager names the webhook, so a loopback address must not be
// reached unless the operator listed it.
func TestWebhookToAPrivateAddressIsRefused(test *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
	}))
	defer server.Close()

	err := sendTestWebhook(guardedExchange(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		test.Fatalf("expected the address to be refused, got %v", err)
	}
	if reached {
		test.Fatal("the webhook server was reached")
	}
}

func TestWebhookToAnAllowedPrivateAddressIsDelivered(test *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		gotBody, _ = io.ReadAll(request.Body)
	}))
	defer server.Close()

	if err := sendTestWebhook(guardedExchange("127.0.0.1"), server.URL); err != nil {
		test.Fatal(err)
	}
	if !strings.HasSuffix(string(gotBody), "body") {
		test.Fatalf("the mail was not posted, got %q", gotBody)
	}
}

// A redirect is dialled like any other connection, so a webhook the operator
// allowed cannot be used to bounce the post to an address they did not.
func TestWebhookRedirectToAnotherPrivateAddressIsRefused(test *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		test.Skipf("cannot listen on a second loopback address: %s", err)
	}
	reached := false
	inner := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
	}))
	_ = inner.Listener.Close()
	inner.Listener = listener
	inner.Start()
	defer inner.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, inner.URL, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	err = sendTestWebhook(guardedExchange("127.0.0.1"), redirecting.URL)
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		test.Fatalf("expected the redirect to be refused, got %v", err)
	}
	if reached {
		test.Fatal("the redirect target was reached")
	}
}

// A mail server alias names a host and port the same way a webhook does.
func TestForwardingDialRefusesAPrivateAddressUnlessAllowed(test *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	if _, err := guardedExchange().dialGuarded(context.Background(), "tcp", listener.Addr().String()); err == nil {
		test.Fatal("expected a loopback address to be refused")
	}
	connection, err := guardedExchange("127.0.0.1").dialGuarded(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		test.Fatalf("an allowed address was refused: %s", err)
	}
	_ = connection.Close()
}

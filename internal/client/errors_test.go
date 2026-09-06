package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/api"
)

// The client tells a refused token and a missing thing apart by the message
// the server sends, so the strings it matches have to be the ones the server
// declares. A rename on the server side fails here rather than in the field.
func TestServerErrorStringsMatch(t *testing.T) {
	if serverNotLoggedIn != api.ErrNotLoggedIn.Error() {
		t.Errorf("client matches %q, the server says %q", serverNotLoggedIn, api.ErrNotLoggedIn)
	}
	if serverNotFound != api.ErrNotFound.Error() {
		t.Errorf("client matches %q, the server says %q", serverNotFound, api.ErrNotFound)
	}
}

func TestExecuteClassifiesErrors(t *testing.T) {
	answer := `{"errors":[{"message":"api: not found"}]}`
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(status)
		_, _ = response.Write([]byte(answer))
	}))
	defer server.Close()

	connection, err := New(Options{URL: server.URL, Token: "tnt_x"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	err = connection.Execute(ctx, `query { GetDomain(domainId: "x") { id } }`, nil, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("a not-found message should be ErrNotFound, got %v", err)
	}

	answer = `{"errors":[{"message":"api: not logged in"}]}`
	err = connection.Execute(ctx, `query { ListDomains { id } }`, nil, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a not-logged-in message should be ErrUnauthorized, got %v", err)
	}

	// The server adds detail after the value; the detail is kept.
	answer = `{"errors":[{"message":"api: not found: no such layout"}]}`
	err = connection.Execute(ctx, `query { ListDomains { id } }`, nil, nil)
	if !errors.Is(err, ErrNotFound) || err.Error() != "client: not found: api: not found: no such layout" {
		t.Errorf("a not-found message with detail should be ErrNotFound and keep the detail, got %v", err)
	}

	answer, status = `unauthorized`, http.StatusUnauthorized
	err = connection.Execute(ctx, `query { ListDomains { id } }`, nil, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("HTTP 401 should be ErrUnauthorized, got %v", err)
	}

	// Some other message stays what the server said.
	answer, status = `{"errors":[{"message":"api: invalid domain"}]}`, http.StatusOK
	err = connection.Execute(ctx, `query { ListDomains { id } }`, nil, nil)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized) || err == nil || err.Error() != "api: invalid domain" {
		t.Errorf("got %v", err)
	}
}

func TestReadOnlyRefusesBeforeSending(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = response.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	connection, err := New(Options{URL: server.URL, Token: "tnt_x", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	err = connection.Execute(ctx, `mutation { DeleteDomain(domainId: "x") }`, nil, nil)
	var refused *ReadOnlyError
	if !errors.As(err, &refused) {
		t.Fatalf("a mutation should be refused, got %v", err)
	}
	if requests != 0 {
		t.Errorf("the refusal should happen before anything is sent; the server saw %d requests", requests)
	}

	if err := connection.Execute(ctx, `query { ListDomains { id } }`, nil, nil); err != nil {
		t.Errorf("a query should go through: %s", err)
	}
	if requests != 1 {
		t.Errorf("the query should have been sent, the server saw %d requests", requests)
	}
}

func TestUnreachableIsConnectionError(t *testing.T) {
	connection, err := New(Options{URL: "http://127.0.0.1:1", Token: "tnt_x"})
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Execute(context.Background(), `query { ListDomains { id } }`, nil, nil)
	var unreachable *ConnectionError
	if !errors.As(err, &unreachable) {
		t.Errorf("a refused connection should be a ConnectionError, got %v", err)
	}
}

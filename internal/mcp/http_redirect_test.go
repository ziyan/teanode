package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A connected server must not be able to aim this server at another host,
// internal ones included, by answering with a redirect.
func TestHTTPTransportDoesNotFollowARedirectToAnotherHost(test *testing.T) {
	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
		http.Error(response, "other host text", http.StatusNotFound)
	}))
	defer other.Close()
	connected := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, other.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer connected.Close()

	transport := NewHTTPTransport(&HTTPSettings{URL: connected.URL})
	identifier := int64(1)
	_, err := transport.Call(context.Background(), &Request{ID: &identifier, Method: "initialize"})
	if err == nil || !strings.Contains(err.Error(), "redirected") {
		test.Fatalf("expected the redirect to be refused, got %v", err)
	}
	if reached {
		test.Fatal("the other host was reached")
	}
	if strings.Contains(err.Error(), "other host text") {
		test.Fatal("the other host's answer was returned")
	}
}

func TestHTTPTransportFollowsARedirectOnTheSameHost(test *testing.T) {
	connected := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/moved" {
			http.Redirect(response, request, "/mcp", http.StatusTemporaryRedirect)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer connected.Close()

	transport := NewHTTPTransport(&HTTPSettings{URL: connected.URL + "/moved"})
	identifier := int64(1)
	response, err := transport.Call(context.Background(), &Request{ID: &identifier, Method: "initialize"})
	if err != nil {
		test.Fatal(err)
	}
	if response.ID == nil || *response.ID != 1 {
		test.Fatalf("unexpected response %+v", response)
	}
}

package apioauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/mcp"
)

// serve puts the discovery documents behind a real HTTP server, so the tests
// below ask for them the way a program would.
func serve(test *testing.T) *httptest.Server {
	test.Helper()
	component, err := New(config.NewMemoryStore(&config.Configuration{}), nil, nil)
	if err != nil {
		test.Fatalf("New: %s", err)
	}
	router := mux.NewRouter().StrictSlash(true)
	if err := component.AddRoutes(router); err != nil {
		test.Fatalf("AddRoutes: %s", err)
	}
	server := httptest.NewServer(router)
	test.Cleanup(server.Close)
	return server
}

func get[T any](test *testing.T, address string) T {
	test.Helper()
	response, err := http.Get(address)
	if err != nil {
		test.Fatalf("GET %s: %s", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		test.Fatalf("GET %s: %d", address, response.StatusCode)
	}
	// The bug this whole change exists to fix was a document that came back
	// as a web page with a 200, so the type is worth asserting rather than
	// assuming.
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		test.Fatalf("GET %s answered %q, not JSON", address, contentType)
	}
	var document T
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		test.Fatalf("GET %s: %s", address, err)
	}
	return document
}

// Both addresses answer, and with the same document.
//
// Clients disagree about which to ask for: wellKnown in
// internal/mcp/oauth.go builds the suffixed and the prefixed form and tries
// them in turn. Serving one and not the other works with some programs and,
// with the rest, looks like a document that would not parse.
func TestBothProtectedResourceAddressesAnswerTheSame(test *testing.T) {
	server := serve(test)

	plain := get[ProtectedResourceMetadata](test, server.URL+api.PathOAuthProtectedResource)
	suffixed := get[ProtectedResourceMetadata](test, server.URL+api.PathOAuthProtectedResourceMCP)

	if plain.Resource != suffixed.Resource {
		test.Errorf("the two addresses name different resources: %q and %q", plain.Resource, suffixed.Resource)
	}
	if plain.Resource != server.URL+api.PathAgentMCP {
		test.Errorf("the resource is %q, not the MCP endpoint", plain.Resource)
	}
	if len(plain.AuthorizationServers) != 1 || plain.AuthorizationServers[0] != server.URL {
		test.Errorf("the authorization servers are %v, not just this one", plain.AuthorizationServers)
	}
	if len(plain.BearerMethodsSupported) != 1 || plain.BearerMethodsSupported[0] != "header" {
		test.Errorf("bearer methods are %v; a token in an address is a token in a log", plain.BearerMethodsSupported)
	}
}

// The issuer is the address the document was asked for.
//
// A conforming client refuses a document whose issuer disagrees with the
// address it used, which is what sameIssuer in internal/mcp/oauth.go does.
// Serving a fixed address from a setting would fail that check on every other
// address this server answers on.
func TestTheIssuerIsTheAddressItWasAskedFor(test *testing.T) {
	server := serve(test)

	metadata := get[AuthorizationServerMetadata](test, server.URL+api.PathOAuthAuthorizationServer)

	if metadata.Issuer != server.URL {
		test.Errorf("the issuer is %q but the document was fetched from %q", metadata.Issuer, server.URL)
	}
	for name, endpoint := range map[string]string{
		"authorization": metadata.AuthorizationEndpoint,
		"token":         metadata.TokenEndpoint,
		"registration":  metadata.RegistrationEndpoint,
		"revocation":    metadata.RevocationEndpoint,
	} {
		if !strings.HasPrefix(endpoint, server.URL+"/") {
			test.Errorf("the %s endpoint is %q, which is not on this server", name, endpoint)
		}
	}
}

// Only the method that hashes.
//
// Offering "plain" beside S256 lets a client pick the one that protects
// nothing, and a client will pick whatever is first.
func TestOnlyTheHashingChallengeMethodIsOffered(test *testing.T) {
	server := serve(test)

	metadata := get[AuthorizationServerMetadata](test, server.URL+api.PathOAuthAuthorizationServer)

	if len(metadata.CodeChallengeMethodsSupported) != 1 || metadata.CodeChallengeMethodsSupported[0] != "S256" {
		test.Errorf("the challenge methods are %v, which should be S256 alone", metadata.CodeChallengeMethodsSupported)
	}
}

// The client half of this repository reads what the server half writes.
//
// internal/mcp/oauth.go was written months earlier and against servers
// somebody else wrote. Pointing it at these documents checks the two halves
// against each other rather than against a guess about what the documents
// should say, which is the only check here that could catch a shape both
// sides would otherwise agree to get wrong.
func TestOurOwnClientDiscoversUs(test *testing.T) {
	server := serve(test)

	metadata, err := mcp.Discover(context.Background(), &mcp.OAuthSettings{
		ServerURL: server.URL + api.PathAgentMCP,
	})
	if err != nil {
		test.Fatalf("our own client could not discover us: %s", err)
	}
	if metadata.Issuer != server.URL {
		test.Errorf("discovered the issuer %q, not %q", metadata.Issuer, server.URL)
	}
	if metadata.AuthorizationEndpoint != server.URL+api.PathOAuthAuthorize {
		test.Errorf("discovered the authorization endpoint %q", metadata.AuthorizationEndpoint)
	}
	if metadata.TokenEndpoint != server.URL+api.PathOAuthToken {
		test.Errorf("discovered the token endpoint %q", metadata.TokenEndpoint)
	}
	// Without this a program has no way to introduce itself, and somebody
	// has to paste a token after all, which is the thing being removed.
	if metadata.RegistrationEndpoint != server.URL+api.PathOAuthRegister {
		test.Errorf("discovered the registration endpoint %q", metadata.RegistrationEndpoint)
	}
}

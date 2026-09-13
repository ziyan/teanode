package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server that publishes no client id has one registered for it, and the
// flow is begun with that one. This is the shape a brokerage and several
// other public servers use: discovery, then registration, then PKCE.
func TestBeginRegistersAClientWhenThereIsNone(t *testing.T) {
	var registered int
	var asked map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_, _ = writer.Write([]byte(`{"authorization_servers":["` + baseOf(request) + `/mcp"]}`))
		case "/.well-known/oauth-authorization-server/mcp":
			// The path-aware form the specification asks for.
			_, _ = writer.Write([]byte(`{"issuer":"` + baseOf(request) + `/mcp","authorization_endpoint":"` + baseOf(request) + `/authorize","token_endpoint":"` + baseOf(request) + `/token","registration_endpoint":"` + baseOf(request) + `/register"}`))
		case "/register":
			registered++
			_ = json.NewDecoder(request.Body).Decode(&asked)
			_, _ = writer.Write([]byte(`{"client_id":"registered-1"}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// A client of the test server's own: the guarded one this package
	// uses by default refuses a loopback address, which is the point of it.
	settings := &OAuthSettings{Client: server.Client(), ServerURL: server.URL + "/mcp", RedirectURL: "https://mail.example/agent?connect=broker", ClientName: "TeaNode", Scopes: []string{"internal"}}
	authorization, err := Begin(context.Background(), settings)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if registered != 1 || authorization.ClientID != "registered-1" {
		t.Fatalf("one client registered and used: %d %q", registered, authorization.ClientID)
	}
	if !strings.Contains(authorization.URL, "client_id=registered-1") || !strings.Contains(authorization.URL, "code_challenge_method=S256") {
		t.Fatalf("the authorization address carries the client and the challenge: %s", authorization.URL)
	}
	if uris, _ := asked["redirect_uris"].([]any); len(uris) != 1 || uris[0] != settings.RedirectURL {
		t.Fatalf("it registers the address the code comes back to: %v", asked["redirect_uris"])
	}
	// A configured client id is used as it is, and nothing is registered.
	registered = 0
	authorization, err = Begin(context.Background(), &OAuthSettings{Client: server.Client(), ServerURL: server.URL + "/mcp", RedirectURL: "https://mail.example/x", ClientID: "by-hand"})
	if err != nil || registered != 0 || authorization.ClientID != "by-hand" {
		t.Fatalf("a configured client is left alone: %d %q %v", registered, authorization.ClientID, err)
	}
}

// A server that publishes neither a client id nor a way to register one
// says so, naming the setting the operator has to fill in.
func TestBeginSaysWhenThereIsNoWayToGetAClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/.well-known/oauth-authorization-server" {
			_, _ = writer.Write([]byte(`{"authorization_endpoint":"https://x.example/a","token_endpoint":"https://x.example/t"}`))
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	_, err := Begin(context.Background(), &OAuthSettings{Client: server.Client(), ServerURL: server.URL + "/mcp", RedirectURL: "https://mail.example/x"})
	if err == nil || !strings.Contains(err.Error(), "oauth.clientId") {
		t.Fatalf("it names the setting to fill in: %v", err)
	}
}

func TestWellKnownTriesBothShapes(t *testing.T) {
	got := wellKnown("https://agent.example.com/mcp/trading", "oauth-authorization-server")
	want := []string{"https://agent.example.com/.well-known/oauth-authorization-server/mcp/trading", "https://agent.example.com/mcp/trading/.well-known/oauth-authorization-server"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("both shapes, the specification's first: %v", got)
	}
	if got := wellKnown("https://example.com", "oauth-authorization-server"); len(got) != 1 || got[0] != "https://example.com/.well-known/oauth-authorization-server" {
		t.Fatalf("an issuer with no path has one shape: %v", got)
	}
}

func baseOf(request *http.Request) string {
	return "http://" + request.Host
}

// Where a person's credentials may be sent, and which addresses this server
// will follow a connected server's word to.
//
// The endpoints come out of a document the far end writes, and what goes to
// them is the authorization code, the PKCE verifier and the client secret.
func TestWhereCredentialsMayBeSent(t *testing.T) {
	t.Parallel()

	if err := usableEndpoint("https://auth.example.com/token"); err != nil {
		t.Fatalf("an ordinary https endpoint: %s", err)
	}
	if err := usableEndpoint("http://auth.example.com/token"); err == nil {
		t.Fatal("plain http across the network is refused")
	}
	// Except on this machine, where nothing else can read it -- an operator
	// running a connected server beside this one.
	for _, address := range []string{"http://127.0.0.1:9000/token", "http://localhost:9000/token", "http://[::1]:9000/token"} {
		if err := usableEndpoint(address); err != nil {
			t.Fatalf("%s is on this machine: %s", address, err)
		}
	}
	if err := usableEndpoint("ftp://auth.example.com/token"); err == nil {
		t.Fatal("and nothing else is an address to send a credential to")
	}

	// A document has to name itself: fetched from one origin and claiming
	// another, it is somebody else's metadata.
	if !sameIssuer("https://auth.example.com", "https://auth.example.com/.well-known/oauth-authorization-server") {
		t.Fatal("its own origin")
	}
	if sameIssuer("https://auth.example.com", "https://elsewhere.test/.well-known/oauth-authorization-server") {
		t.Fatal("somebody else's")
	}

	// Whether the server named by the far end is followed with the guard on
	// depends on where the operator put the server itself.
	if !declaredPrivately("http://127.0.0.1:3000/mcp") {
		t.Fatal("a server on this machine is a private deployment")
	}
	if !declaredPrivately("http://10.1.2.3:3000/mcp") {
		t.Fatal("and so is one on a private network")
	}
	if declaredPrivately("https://example.com/mcp") {
		t.Fatal("a server out on the internet is not a private deployment")
	}
}

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeServer answers the three methods a client uses, over HTTP with a
// session id, and refuses without the credential it was given.
func fakeServer(t *testing.T, credential string, streaming bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if credential != "" && request.Header.Get("Authorization") != "Bearer "+credential {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if request.Method == http.MethodDelete {
			writer.WriteHeader(http.StatusOK)
			return
		}
		var message Request
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		if message.ID == nil {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		if message.Method != "initialize" && request.Header.Get("Mcp-Session-Id") != "session-1" {
			http.Error(writer, "no session", 400)
			return
		}
		var result any
		switch message.Method {
		case "initialize":
			writer.Header().Set("Mcp-Session-Id", "session-1")
			result = map[string]any{"protocolVersion": ProtocolVersion, "serverInfo": map[string]any{"name": "tracker", "version": "1.0"}}
		case "tools/list":
			var params struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.Cursor == "" {
				result = map[string]any{"tools": []map[string]any{{"name": "track", "description": "Track a parcel.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"number": map[string]any{"type": "string"}}}}}, "nextCursor": "page2"}
			} else {
				result = map[string]any{"tools": []map[string]any{{"name": "cancel", "description": "Cancel a shipment.", "inputSchema": map[string]any{"type": "object"}}}}
			}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(message.Params, &params)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "parcel " + string(params.Arguments) + " is in transit"}}}
		default:
			response := Response{JSONRPC: "2.0", ID: message.ID, Error: &Error{Code: -32601, Message: "no such method"}}
			_ = json.NewEncoder(writer).Encode(response)
			return
		}
		encoded, _ := json.Marshal(Response{JSONRPC: "2.0", ID: message.ID, Result: mustMarshal(result)})
		if streaming {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("event: message\ndata: " + string(encoded) + "\n\n"))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(encoded)
	}))
}

func mustMarshal(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func TestHTTPClientSpeaksTheProtocol(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		server := fakeServer(t, "t-1", streaming)
		transport := NewHTTPTransport(&HTTPSettings{URL: server.URL, Headers: func() (http.Header, error) {
			return http.Header{"Authorization": {"Bearer t-1"}}, nil
		}})
		client := NewClient(transport)
		information, err := client.Initialize(context.Background(), "test", "0")
		if err != nil {
			t.Fatalf("streaming %v: Initialize: %s", streaming, err)
		}
		if information.Name != "tracker" {
			t.Fatalf("server %+v", information)
		}
		tools, err := client.ListTools(context.Background())
		if err != nil {
			t.Fatalf("ListTools: %s", err)
		}
		if len(tools) != 2 || tools[0].Name != "track" || tools[1].Name != "cancel" {
			t.Fatalf("tools %+v", tools)
		}
		result, err := client.CallTool(context.Background(), "track", json.RawMessage(`{"number":"42"}`))
		if err != nil {
			t.Fatalf("CallTool: %s", err)
		}
		if !strings.Contains(result.Text(), `parcel {"number":"42"} is in transit`) {
			t.Fatalf("result %q", result.Text())
		}
		if _, err := client.call(context.Background(), "nothing", nil); err == nil {
			t.Fatal("an unknown method should fail")
		}
		_ = client.Close()
		server.Close()
	}
	// Without the credential, the server refuses and the client says so.
	server := fakeServer(t, "t-1", false)
	defer server.Close()
	client := NewClient(NewHTTPTransport(&HTTPSettings{URL: server.URL}))
	if _, err := client.Initialize(context.Background(), "test", "0"); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// The stdio transport is tested against this very binary: run with the
// variable set, the test process is a server on its standard input and
// output.
func TestMain(m *testing.M) {
	if os.Getenv("TEANODE_TEST_MCP_STDIO") == "1" {
		serveStdio()
		return
	}
	os.Exit(m.Run())
}

func serveStdio() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message Request
		if err := json.Unmarshal([]byte(scanner.Text()), &message); err != nil || message.ID == nil {
			continue
		}
		var result any
		switch message.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": ProtocolVersion, "serverInfo": map[string]any{"name": "local", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "echo", "description": "Echo.", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "echo: " + string(message.Params)}}}
		}
		encoded, _ := json.Marshal(Response{JSONRPC: "2.0", ID: message.ID, Result: mustMarshal(result)})
		fmt.Println(string(encoded))
	}
}

func TestStdioTransportRunsASubprocess(t *testing.T) {
	transport, err := NewStdioTransport(&StdioSettings{Command: os.Args[0], Env: map[string]string{"TEANODE_TEST_MCP_STDIO": "1"}})
	if err != nil {
		t.Fatalf("NewStdioTransport: %s", err)
	}
	client := NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	information, err := client.Initialize(ctx, "test", "0")
	if err != nil {
		t.Fatalf("Initialize: %s", err)
	}
	if information.Name != "local" {
		t.Fatalf("server %+v", information)
	}
	tools, err := client.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools %+v %v", tools, err)
	}
	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"a":1}`))
	if err != nil || !strings.Contains(result.Text(), `"a":1`) {
		t.Fatalf("result %v %v", result, err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}
}

func TestOAuthDiscoversBeginsAndExchanges(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(writer).Encode(map[string]any{"authorization_servers": []string{server.URL + "/auth"}})
		case "/auth/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(writer).Encode(map[string]any{"issuer": server.URL + "/auth", "authorization_endpoint": server.URL + "/auth/authorize", "token_endpoint": server.URL + "/auth/token"})
		case "/auth/token":
			_ = request.ParseForm()
			if request.Form.Get("grant_type") == "authorization_code" {
				if request.Form.Get("code") != "code-1" || request.Form.Get("code_verifier") == "" {
					http.Error(writer, "bad code", 400)
					return
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
				return
			}
			if request.Form.Get("refresh_token") != "refresh-1" {
				http.Error(writer, "bad refresh", 400)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "access-2", "expires_in": 60})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	settings := &OAuthSettings{ServerURL: server.URL + "/mcp", ClientID: "teanode", RedirectURL: "https://mail.example.com/agent/connect", Scopes: []string{"tools"}}
	authorization, err := Begin(context.Background(), settings)
	if err != nil {
		t.Fatalf("Begin: %s", err)
	}
	for _, want := range []string{"/auth/authorize?", "code_challenge_method=S256", "client_id=teanode", "state=" + authorization.State, "scope=tools", "resource="} {
		if !strings.Contains(authorization.URL, want) {
			t.Fatalf("the authorization URL lacks %q: %s", want, authorization.URL)
		}
	}
	tokens, err := Exchange(context.Background(), settings, "code-1", authorization.Verifier)
	if err != nil {
		t.Fatalf("Exchange: %s", err)
	}
	if tokens.AccessToken != "access-1" || tokens.RefreshToken != "refresh-1" || tokens.Expired(time.Now()) {
		t.Fatalf("tokens %+v", tokens)
	}
	refreshed, err := Refresh(context.Background(), settings, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %s", err)
	}
	if refreshed.AccessToken != "access-2" || refreshed.RefreshToken != "refresh-1" || !refreshed.Expired(time.Now().Add(2*time.Minute)) {
		t.Fatalf("refreshed %+v", refreshed)
	}
}

package mcpserve

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/mcp"
)

// catalog is a Tools with two tools in it, one of which always fails.
type catalog struct {
	called    string
	arguments string
}

func (self *catalog) List(ctx context.Context) ([]mcp.Tool, error) {
	return []mcp.Tool{
		{
			Name:        "weather",
			Description: "what it is doing outside",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		},
		{Name: "broken", Description: "always fails", InputSchema: map[string]any{"type": "object"}},
	}, nil
}

func (self *catalog) Call(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	self.called, self.arguments = name, string(arguments)
	if name == "broken" {
		return "", errors.New("the roof fell in")
	}
	return "raining in " + name, nil
}

// pipe carries the client's messages straight into the server, so that the
// two halves of the protocol are tested against each other rather than
// each against my idea of the other.
type pipe struct {
	server *Server
}

func (self *pipe) Call(ctx context.Context, request *mcp.Request) (*mcp.Response, error) {
	response := self.server.Handle(ctx, request)
	if response == nil {
		return nil, errors.New("the server said nothing to a request that has an id")
	}
	return response, nil
}

func (self *pipe) Notify(ctx context.Context, notification *mcp.Request) error {
	if response := self.server.Handle(ctx, notification); response != nil {
		return errors.New("the server answered a notification")
	}
	return nil
}

func (self *pipe) Close() error { return nil }

// The client this repository already has, talking to the server this
// package adds: the handshake, the catalog and a call, with nothing
// hand-rolled in between. If these two ever disagree about the protocol,
// this is the test that says so.
func TestTheClientInThisRepositoryCanUseThisServer(t *testing.T) {
	tools := &catalog{}
	client := mcp.NewClient(&pipe{server: New("teanode", "v1.2.3", tools)})

	information, err := client.Initialize(context.Background(), "test", "0")
	if err != nil {
		t.Fatalf("initialize: %s", err)
	}
	if information.Name != "teanode" || information.Version != "v1.2.3" {
		t.Fatalf("the server named itself %q %q", information.Name, information.Version)
	}

	listed, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("tools/list: %s", err)
	}
	if len(listed) != 2 || listed[0].Name != "weather" {
		t.Fatalf("the catalog came back as %+v", listed)
	}
	if listed[0].InputSchema["type"] != "object" {
		t.Fatalf("the schema did not survive: %+v", listed[0].InputSchema)
	}

	result, err := client.CallTool(context.Background(), "weather", json.RawMessage(`{"city":"Lisbon"}`))
	if err != nil {
		t.Fatalf("tools/call: %s", err)
	}
	if result.IsError {
		t.Fatalf("the call was marked failed: %+v", result)
	}
	if !strings.Contains(result.Text(), "raining in weather") {
		t.Fatalf("the answer was %q", result.Text())
	}
	if tools.arguments != `{"city":"Lisbon"}` {
		t.Fatalf("the tool was given %s", tools.arguments)
	}
}

// A tool that fails is an answer the model can read, not a broken call.
// Getting this the other way round -- a JSON-RPC error -- makes a harness
// report that the server is down when in fact the roof fell in.
func TestAToolThatFailsIsAnAnswerAndNotAnError(t *testing.T) {
	client := mcp.NewClient(&pipe{server: New("teanode", "v1", &catalog{})})
	if _, err := client.Initialize(context.Background(), "test", "0"); err != nil {
		t.Fatalf("initialize: %s", err)
	}

	result, err := client.CallTool(context.Background(), "broken", nil)
	if err != nil {
		t.Fatalf("the failure was carried as a transport error: %s", err)
	}
	if !result.IsError {
		t.Fatal("a tool that failed was reported as having worked")
	}
	if !strings.Contains(result.Text(), "the roof fell in") {
		t.Fatalf("the reason was lost: %q", result.Text())
	}
}

// A notification is told to us, not asked of us, and answering one puts a
// response on the wire that the client is not waiting for. Over a stream
// that is a message nobody reads; over standard input and output it is a
// line that desynchronises everything after it.
func TestANotificationIsNotAnswered(t *testing.T) {
	server := New("teanode", "v1", &catalog{})
	for _, method := range []string{"notifications/initialized", "notifications/cancelled", "notifications/something/later"} {
		if response := server.Handle(context.Background(), &mcp.Request{JSONRPC: "2.0", Method: method}); response != nil {
			t.Fatalf("%s was answered with %+v", method, response)
		}
	}
}

func TestAnUnknownMethodIsRefusedByNumber(t *testing.T) {
	server := New("teanode", "v1", &catalog{})
	id := int64(7)
	response := server.Handle(context.Background(), &mcp.Request{JSONRPC: "2.0", ID: &id, Method: "resources/list"})
	if response == nil || response.Error == nil {
		t.Fatalf("an unknown method was not refused: %+v", response)
	}
	if response.Error.Code != codeMethodNotFound {
		t.Fatalf("refused with code %d rather than %d", response.Error.Code, codeMethodNotFound)
	}
	if *response.ID != id {
		t.Fatalf("the refusal came back under id %d", *response.ID)
	}
}

func TestACallWithoutANameIsRefused(t *testing.T) {
	server := New("teanode", "v1", &catalog{})
	id := int64(1)
	response := server.Handle(context.Background(), &mcp.Request{
		JSONRPC: "2.0", ID: &id, Method: "tools/call",
		Params: json.RawMessage(`{"arguments":{}}`),
	})
	if response == nil || response.Error == nil || response.Error.Code != codeInvalidParams {
		t.Fatalf("a nameless call was answered with %+v", response)
	}
}

// The handshake says which version will be spoken, whatever was asked for,
// so that a client built against a later protocol finds out here rather
// than by a call failing later.
func TestTheHandshakeSaysWhichVersionIsSpoken(t *testing.T) {
	server := New("teanode", "v1", &catalog{})
	id := int64(1)
	response := server.Handle(context.Background(), &mcp.Request{
		JSONRPC: "2.0", ID: &id, Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2099-01-01","capabilities":{},"clientInfo":{"name":"later","version":"9"}}`),
	})
	if response == nil || response.Error != nil {
		t.Fatalf("the handshake failed: %+v", response)
	}
	var result struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("reading the result: %s", err)
	}
	if result.ProtocolVersion != mcp.ProtocolVersion {
		t.Fatalf("answered %q rather than %q", result.ProtocolVersion, mcp.ProtocolVersion)
	}
	if _, offered := result.Capabilities["tools"]; !offered {
		t.Fatalf("the server did not offer tools: %+v", result.Capabilities)
	}
}

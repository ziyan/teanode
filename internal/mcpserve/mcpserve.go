// Package mcpserve answers the Model Context Protocol, the other way round
// from internal/mcp: that package calls out to servers somebody else runs,
// and this one lets somebody else's program -- a coding harness, an editor
// -- call in and use what this server can do.
//
// Only the tool half of the protocol is answered: tools/list and
// tools/call, after the initialize handshake. Not prompts, not resources,
// not sampling. That is the half a harness wants and the half this server
// has something to put in.
//
// Nothing here knows about HTTP, about a database, or about what a tool
// is: it takes one decoded JSON-RPC message and gives back the message to
// send in reply, and asks a Tools for the catalog and for the work. A
// transport wraps it -- the HTTP one in internal/api/v1api/apimcp, the
// standard input and output one in the command line -- and both get the
// same protocol.
package mcpserve

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/mcp"
)

// The JSON-RPC error codes this server answers with. They are the
// protocol's own, not ours, and a client reads them by number.
const (
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Tools is what the server offers and how it runs one.
//
// Call returns what the tool said. An error from Call is the tool failing,
// which the protocol carries as a successful response holding isError --
// not as a JSON-RPC error, which means the call never happened. The
// difference matters to the model on the other end: a failed call is
// something it can read and try differently, and a broken transport is
// not.
type Tools interface {
	List(ctx context.Context) ([]mcp.Tool, error)
	Call(ctx context.Context, name string, arguments json.RawMessage) (string, error)
}

// Server answers one client's messages.
type Server struct {
	name    string
	version string
	tools   Tools
}

// New builds a server that says it is called name, at version, offering
// what tools holds.
func New(name, version string, tools Tools) *Server {
	return &Server{name: name, version: version, tools: tools}
}

// Handle answers one message.
//
// A notification -- a message with no id -- gets no answer, and this
// returns nil for one. The transport must send nothing at all in that
// case rather than sending a response with a null id, which a strict
// client refuses.
func (self *Server) Handle(ctx context.Context, request *mcp.Request) *mcp.Response {
	if request == nil {
		return nil
	}
	if request.JSONRPC != "" && request.JSONRPC != "2.0" {
		return self.failure(request, codeInvalidRequest, "this server speaks JSON-RPC 2.0")
	}
	switch request.Method {
	case "initialize":
		return self.initialize(request)
	case "notifications/initialized", "notifications/cancelled":
		// Said to us, not asked of us.
		return nil
	case "ping":
		// The protocol's keep-alive: an empty result is the whole answer.
		return self.success(request, map[string]any{})
	case "tools/list":
		return self.list(ctx, request)
	case "tools/call":
		return self.call(ctx, request)
	}
	if request.ID == nil {
		// An unknown notification is not an error. A client that sends a
		// notification we have not heard of is a client speaking a later
		// version of the protocol, and the right answer is silence.
		return nil
	}
	return self.failure(request, codeMethodNotFound, fmt.Sprintf("this server has no method %q", request.Method))
}

// initialize is the handshake. The version asked for is answered with the
// version spoken rather than echoed: a client asking for a version this
// server does not speak is told what it will get, and decides.
func (self *Server) initialize(request *mcp.Request) *mcp.Response {
	return self.success(request, map[string]any{
		"protocolVersion": mcp.ProtocolVersion,
		"capabilities": map[string]any{
			// listChanged says the catalog can change under a client and
			// that it will be told. It can -- a person connecting another
			// server adds tools -- but nothing here sends the
			// notification yet, so it would be a promise this does not
			// keep.
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{"name": self.name, "version": self.version},
	})
}

func (self *Server) list(ctx context.Context, request *mcp.Request) *mcp.Response {
	catalog, err := self.tools.List(ctx)
	if err != nil {
		return self.failure(request, codeInternal, err.Error())
	}
	if catalog == nil {
		catalog = []mcp.Tool{}
	}
	// No cursor: the whole catalog goes in one page. It is tens of tools,
	// not thousands, and a client that asked for a page it cannot get is
	// worse served by pagination than by the list.
	return self.success(request, map[string]any{"tools": catalog})
}

func (self *Server) call(ctx context.Context, request *mcp.Request) *mcp.Response {
	var parameters struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &parameters); err != nil {
			return self.failure(request, codeInvalidParams, "the parameters of tools/call are not an object")
		}
	}
	if parameters.Name == "" {
		return self.failure(request, codeInvalidParams, "tools/call needs the name of a tool")
	}
	if len(parameters.Arguments) == 0 {
		parameters.Arguments = json.RawMessage(`{}`)
	}
	text, err := self.tools.Call(ctx, parameters.Name, parameters.Arguments)
	if err != nil {
		// The tool failed, which is an answer, not a broken call.
		return self.success(request, &mcp.CallResult{
			Content: []mcp.Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
	}
	return self.success(request, &mcp.CallResult{
		Content: []mcp.Content{{Type: "text", Text: text}},
	})
}

func (self *Server) success(request *mcp.Request, result any) *mcp.Response {
	if request.ID == nil {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return self.failure(request, codeInternal, err.Error())
	}
	return &mcp.Response{JSONRPC: "2.0", ID: request.ID, Result: encoded}
}

func (self *Server) failure(request *mcp.Request, code int, message string) *mcp.Response {
	if request.ID == nil {
		return nil
	}
	return &mcp.Response{
		JSONRPC: "2.0", ID: request.ID,
		Error: &mcp.Error{Code: code, Message: message},
	}
}

// Package mcp is a client for servers that speak the Model Context Protocol:
// the one wire format the model providers, the editors and most services
// have settled on for "here are my tools, call them". Two transports —
// streamable HTTP at a URL, and a subprocess over its standard input and
// output — and OAuth 2.1 with PKCE for the servers that want a person's
// authorization. Only tools are consumed: not prompts, not resources.
//
// The protocol is JSON-RPC 2.0. A session begins with initialize and the
// initialized notification, then tools/list and tools/call. Everything a
// server answers is data the caller must treat as such.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// ProtocolVersion is the version this client speaks.
const ProtocolVersion = "2025-03-26"

// Transport carries JSON-RPC messages to a server and back.
type Transport interface {
	// Call sends a request and waits for its response.
	Call(ctx context.Context, request *Request) (*Response, error)

	// Notify sends a notification, which has no response.
	Notify(ctx context.Context, notification *Request) error

	// Close ends the session.
	Close() error
}

// Request is a JSON-RPC request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is what a server answers when a call fails.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (self *Error) Error() string {
	return fmt.Sprintf("mcp: %s (code %d)", self.Message, self.Code)
}

// ErrUnauthorized says the server wants a credential this client lacks.
var ErrUnauthorized = errors.New("mcp: unauthorized")

// Tool is one tool a server offers.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Content is one part of a tool's answer.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// CallResult is what a tool answered.
type CallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Text is the answer's text parts joined; a non-text part is named.
func (self *CallResult) Text() string {
	text := ""
	for _, part := range self.Content {
		switch part.Type {
		case "text":
			text += part.Text + "\n"
		default:
			text += fmt.Sprintf("[%s content, %s]\n", part.Type, part.MimeType)
		}
	}
	return text
}

// ServerInformation is what a server said about itself.
type ServerInformation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Client is a session with one server.
type Client struct {
	transport Transport
	next      atomic.Int64
	mutex     sync.Mutex
	server    ServerInformation
	ready     bool
}

// Gone says the transport underneath has ended -- a server on somebody's
// computer whose connection went -- so that a cached client is dropped
// rather than handed out to fail. A transport that cannot say is taken as
// still there.
func (self *Client) Gone() bool {
	if gone, ok := self.transport.(interface{ Gone() bool }); ok {
		return gone.Gone()
	}
	return false
}

// NewClient wraps a transport; Initialize makes it a session.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

// Initialize begins the session and learns the server's name.
func (self *Client) Initialize(ctx context.Context, clientName, clientVersion string) (*ServerInformation, error) {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	})
	response, err := self.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}
	var result struct {
		ProtocolVersion string            `json:"protocolVersion"`
		ServerInfo      ServerInformation `json:"serverInfo"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("mcp: reading the initialize result: %w", err)
	}
	if err := self.transport.Notify(ctx, &Request{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return nil, err
	}
	self.mutex.Lock()
	self.server = result.ServerInfo
	self.ready = true
	self.mutex.Unlock()
	return &result.ServerInfo, nil
}

// Server is what the server said about itself, after Initialize.
func (self *Client) Server() ServerInformation {
	if self == nil {
		return ServerInformation{}
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.server
}

// ListTools is every tool the server offers, across its pages.
func (self *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var tools []Tool
	cursor := ""
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		encoded, _ := json.Marshal(params)
		response, err := self.call(ctx, "tools/list", encoded)
		if err != nil {
			return nil, err
		}
		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			return nil, fmt.Errorf("mcp: reading the tool list: %w", err)
		}
		tools = append(tools, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return tools, nil
}

// CallTool runs one tool.
func (self *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*CallResult, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": arguments})
	response, err := self.call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	var result CallResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("mcp: reading the tool's answer: %w", err)
	}
	return &result, nil
}

// Close ends the session.
func (self *Client) Close() error {
	if self == nil {
		return nil
	}
	return self.transport.Close()
}

// ErrNoSession is what every call answers when there is no session behind it.
var ErrNoSession = errors.New("mcp: there is no session with this server")

func (self *Client) call(ctx context.Context, method string, params json.RawMessage) (*Response, error) {
	// A session that was never made, or one discovery closed when the server
	// stopped answering. A caller holding the client from a moment ago finds
	// nothing here, and a nil dereference inside a goroutine is a crash of
	// the whole program rather than a failed tool call.
	if self == nil {
		return nil, ErrNoSession
	}
	id := self.next.Add(1)
	response, err := self.transport.Call(ctx, &Request{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	return response, nil
}

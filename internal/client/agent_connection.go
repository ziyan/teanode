package client

import (
	"context"
	"time"
)

// A person's connections to the servers the operator declared.

// AgentServer is a declared server as the person sees it.
type AgentServer struct {
	Name            string     `json:"name"`
	Transport       string     `json:"transport"`
	Auth            string     `json:"auth"`
	Headless        bool       `json:"headless"`
	Enabled         bool       `json:"enabled"`
	Status          string     `json:"status"`
	LastError       string     `json:"lastError"`
	LastConnectedAt *time.Time `json:"lastConnectedAt"`
	Tools           int        `json:"tools"`
}

const serverFields = `{ name transport auth headless enabled status lastError lastConnectedAt tools }`

// The documents.
const (
	DocumentListAgentServers      = `query { ListAgentServers ` + serverFields + ` }`
	DocumentConnectAgentServer    = `mutation ($server: String!, $credential: String) { ConnectAgentServer(server: $server, credential: $credential) ` + serverFields + ` }`
	DocumentDisconnectAgentServer = `mutation ($server: String!) { DisconnectAgentServer(server: $server) ` + serverFields + ` }`
	DocumentBeginAgentServerOAuth = `mutation ($server: String!, $redirectUrl: String!) { BeginAgentServerOAuth(server: $server, redirectUrl: $redirectUrl) }`

	documentFinishAgentServerOAuth = `mutation ($server: String!, $code: String!, $state: String!) {
  FinishAgentServerOAuth(server: $server, code: $code, state: $state) { name status lastError tools }
}`
)

// FinishAgentServerOAuth hands back what the authorization came back with.
func FinishAgentServerOAuth(ctx context.Context, connection *Client, server, code, state string) (*AgentServer, error) {
	var result struct {
		FinishAgentServerOAuth *AgentServer `json:"FinishAgentServerOAuth"`
	}
	if err := connection.Execute(ctx, documentFinishAgentServerOAuth, map[string]any{"server": server, "code": code, "state": state}, &result); err != nil {
		return nil, err
	}
	return result.FinishAgentServerOAuth, nil
}

// ListAgentServers is the declared servers and where the person stands.
func ListAgentServers(ctx context.Context, connection *Client) ([]*AgentServer, error) {
	var result struct {
		ListAgentServers []*AgentServer `json:"ListAgentServers"`
	}
	if err := connection.Execute(ctx, DocumentListAgentServers, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentServers, nil
}

// ConnectAgentServer connects a server with the person's credential.
func ConnectAgentServer(ctx context.Context, connection *Client, server, credential string) (*AgentServer, error) {
	var result struct {
		ConnectAgentServer *AgentServer `json:"ConnectAgentServer"`
	}
	variables := map[string]any{"server": server}
	if credential != "" {
		variables["credential"] = credential
	}
	if err := connection.Execute(ctx, DocumentConnectAgentServer, variables, &result); err != nil {
		return nil, err
	}
	return result.ConnectAgentServer, nil
}

// DisconnectAgentServer forgets the person's credential for a server.
func DisconnectAgentServer(ctx context.Context, connection *Client, server string) (*AgentServer, error) {
	var result struct {
		DisconnectAgentServer *AgentServer `json:"DisconnectAgentServer"`
	}
	if err := connection.Execute(ctx, DocumentDisconnectAgentServer, map[string]any{"server": server}, &result); err != nil {
		return nil, err
	}
	return result.DisconnectAgentServer, nil
}

// BeginAgentServerOAuth is the address to send the person to.
func BeginAgentServerOAuth(ctx context.Context, connection *Client, server, redirectURL string) (string, error) {
	var result struct {
		BeginAgentServerOAuth string `json:"BeginAgentServerOAuth"`
	}
	if err := connection.Execute(ctx, DocumentBeginAgentServerOAuth, map[string]any{"server": server, "redirectUrl": redirectURL}, &result); err != nil {
		return "", err
	}
	return result.BeginAgentServerOAuth, nil
}

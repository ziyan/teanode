package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mcp"
	"github.com/ziyan/teanode/internal/models"
)

// A person's connections to the servers the operator declared: which
// servers there are and where the person stands with each, a credential
// given, an authorization begun and finished, a disconnect. Secrets are
// sealed on the way in and never come back out.

// AgentConnectionQuery reads them.
type AgentConnectionQuery interface {
	// The connected servers the operator declared, and where the caller
	// stands with each. Needs agent:use.
	ListAgentServers(ctx context.Context) ([]*AgentServerView, error)
}

// AgentConnectionMutation changes them.
type AgentConnectionMutation interface {
	// Connect a server that takes the person's own credential; the server
	// is tried at once and the row says whether it answered. Needs
	// agent:use.
	ConnectAgentServer(ctx context.Context, arguments ConnectAgentServerArguments) (*AgentServerView, error)

	// Forget the caller's credential or authorization for a server. Needs
	// agent:use.
	DisconnectAgentServer(ctx context.Context, arguments DisconnectAgentServerArguments) (*AgentServerView, error)

	// Begin an authorization: the address to send the person to. The
	// redirect is where the code comes back, which must be a page of this
	// server. Needs agent:use.
	BeginAgentServerOAuth(ctx context.Context, arguments BeginAgentServerOAuthArguments) (string, error)

	// Finish an authorization with the code and state the person came
	// back with. Needs agent:use.
	FinishAgentServerOAuth(ctx context.Context, arguments FinishAgentServerOAuthArguments) (*AgentServerView, error)
}

// AgentServerView is one declared server as the person sees it.
type AgentServerView struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Auth      string `json:"auth"`
	Headless  bool   `json:"headless"`
	Enabled   bool   `json:"enabled"`

	// Status is the person's connection: connected, pending, error,
	// disconnected, or empty when the server needs no connection.
	Status          string     `json:"status"`
	LastError       string     `json:"lastError,omitempty"`
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"`

	// Tools is how many the server offered when last probed; zero when
	// unknown.
	Tools int `json:"tools"`
}

// ConnectAgentServerArguments name the server and the credential.
type ConnectAgentServerArguments struct {
	Server     string `json:"server"`
	Credential string `json:"credential" graphapi:"nullable"`
}

// DisconnectAgentServerArguments name the server.
type DisconnectAgentServerArguments struct {
	Server string `json:"server"`
}

// BeginAgentServerOAuthArguments name the server and where to come back.
type BeginAgentServerOAuthArguments struct {
	Server      string `json:"server"`
	RedirectURL string `json:"redirectUrl"`
}

// FinishAgentServerOAuthArguments carry what the person came back with.
type FinishAgentServerOAuthArguments struct {
	Server string `json:"server"`
	Code   string `json:"code"`
	State  string `json:"state"`
}

// pendingAuthorization is what a flow keeps between begin and finish,
// sealed.
type pendingAuthorization struct {
	State       string `json:"state"`
	Verifier    string `json:"verifier"`
	RedirectURL string `json:"redirectUrl"`
	// ClientID is the client the flow was begun with, kept because a
	// server that publishes none has one registered for it, and finishing
	// must use the same one -- including on another instance.
	ClientID string `json:"clientId,omitempty"`
}

func (self *graph) serverView(server *config.AgentMCPServer, connection *models.AgentConnection) *AgentServerView {
	view := &AgentServerView{Name: server.Name, Transport: server.ResolvedTransport(), Auth: server.ResolvedAuth(), Headless: server.Headless, Enabled: server.IsEnabled()}
	if connection != nil {
		view.Status = string(connection.Status)
		view.LastError = connection.LastError
		view.LastConnectedAt = connection.LastConnectedAt
	}
	return view
}

func (self *graph) ListAgentServers(ctx context.Context) ([]*AgentServerView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	if !agent.FeatureAllowed(configuration, "connectedServers") {
		return []*AgentServerView{}, nil
	}
	connections, err := self.transaction(ctx).ListAgentConnections(found.ID)
	if err != nil {
		return nil, err
	}
	byServer := map[string]*models.AgentConnection{}
	for _, connection := range connections {
		byServer[connection.ServerName] = connection
	}
	views := make([]*AgentServerView, 0, len(configuration.Agent.MCP.Servers))
	for index := range configuration.Agent.MCP.Servers {
		server := &configuration.Agent.MCP.Servers[index]
		views = append(views, self.serverView(server, byServer[server.Name]))
	}
	return views, nil
}

// requireServer is a declared server by name, and the worker.
func (self *graph) requireServer(name string) (*agent.Agent, *config.AgentMCPServer, error) {
	worker := self.agentWorker()
	if worker == nil || !agent.FeatureAllowed(self.config.Current(), "connectedServers") {
		return nil, nil, agent.ErrUnavailable
	}
	server := worker.Server(strings.TrimSpace(name))
	if server == nil {
		return nil, nil, api.ErrNotFound
	}
	return worker, server, nil
}

func (self *graph) ConnectAgentServer(ctx context.Context, arguments ConnectAgentServerArguments) (*AgentServerView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, server, err := self.requireServer(arguments.Server)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	connection := &models.AgentConnection{AgentID: found.ID, ServerName: server.Name, Status: models.ConnectionConnected}
	switch server.ResolvedAuth() {
	case config.AgentMCPAuthUser:
		credential := strings.TrimSpace(arguments.Credential)
		if credential == "" {
			return nil, fmt.Errorf("%w: this server takes your own credential", api.ErrInvalidArguments)
		}
		sealed, err := worker.SealSecret(credential)
		if err != nil {
			return nil, err
		}
		connection.Credential = sealed
	case config.AgentMCPAuthOAuth:
		return nil, fmt.Errorf("%w: this server is connected by authorizing it; begin that instead", api.ErrInvalidArguments)
	default:
		// Nothing of the person's is needed; the row only records that
		// they looked.
	}
	// Tried at once, so a wrong credential is a message now rather than a
	// silent absence of tools later.
	stored, err := tx.PutAgentConnection(connection)
	if err != nil {
		return nil, translateError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tools, probeErr := worker.Probe(ctx, server, found.ID)
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		if probeErr != nil {
			stored.Status = models.ConnectionError
			stored.LastError = probeErr.Error()
		} else {
			now := time.Now()
			stored.Status = models.ConnectionConnected
			stored.LastError = ""
			stored.LastConnectedAt = &now
		}
		_, err := tx.PutAgentConnection(stored)
		return err
	}); err != nil {
		return nil, err
	}
	view := self.serverView(server, stored)
	view.Tools = tools
	log.Noticef("%s connected the server %q for their agent", operatorName(ctx), server.Name)
	return view, nil
}

func (self *graph) DisconnectAgentServer(ctx context.Context, arguments DisconnectAgentServerArguments) (*AgentServerView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, server, err := self.requireServer(arguments.Server)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := tx.DeleteAgentConnection(found.ID, server.Name); err != nil {
		return nil, err
	}
	worker.ForgetConnection(server.Name, found.ID)
	return self.serverView(server, &models.AgentConnection{Status: models.ConnectionDisconnected}), nil
}

func (self *graph) BeginAgentServerOAuth(ctx context.Context, arguments BeginAgentServerOAuthArguments) (string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return "", err
	}
	worker, server, err := self.requireServer(arguments.Server)
	if err != nil {
		return "", err
	}
	if server.ResolvedAuth() != config.AgentMCPAuthOAuth {
		return "", fmt.Errorf("%w: this server is not connected by authorizing it", api.ErrInvalidArguments)
	}
	redirect := strings.TrimSpace(arguments.RedirectURL)
	if redirect == "" {
		return "", fmt.Errorf("%w: a redirect address is needed", api.ErrInvalidArguments)
	}
	authorization, err := mcp.Begin(ctx, worker.OAuthSettings(server, redirect))
	if err != nil {
		return "", err
	}
	pending, _ := json.Marshal(pendingAuthorization{State: authorization.State, Verifier: authorization.Verifier, RedirectURL: redirect, ClientID: authorization.ClientID})
	sealed, err := worker.SealSecret(string(pending))
	if err != nil {
		return "", err
	}
	tx := self.transaction(ctx)
	existing, err := tx.GetAgentConnection(found.ID, server.Name)
	if err != nil {
		return "", err
	}
	connection := &models.AgentConnection{AgentID: found.ID, ServerName: server.Name, Status: models.ConnectionPending, Pending: sealed}
	if existing != nil {
		connection.Tokens = existing.Tokens
	}
	if _, err := tx.PutAgentConnection(connection); err != nil {
		return "", translateError(err)
	}
	return authorization.URL, nil
}

func (self *graph) FinishAgentServerOAuth(ctx context.Context, arguments FinishAgentServerOAuthArguments) (*AgentServerView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, server, err := self.requireServer(arguments.Server)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	connection, err := tx.GetAgentConnection(found.ID, server.Name)
	if err != nil {
		return nil, err
	}
	if connection == nil || connection.Status != models.ConnectionPending {
		return nil, fmt.Errorf("%w: no authorization is under way for %s", api.ErrInvalidArguments, server.Name)
	}
	opened, err := worker.OpenSecret(connection.Pending)
	if err != nil {
		return nil, err
	}
	var pending pendingAuthorization
	if err := json.Unmarshal([]byte(opened), &pending); err != nil {
		return nil, err
	}
	if pending.State == "" || pending.State != arguments.State {
		return nil, fmt.Errorf("%w: the authorization did not come back as it left", api.ErrInvalidArguments)
	}
	settings := worker.OAuthSettings(server, pending.RedirectURL)
	if pending.ClientID != "" {
		settings.ClientID = pending.ClientID
	}
	tokens, err := mcp.Exchange(ctx, settings, arguments.Code, pending.Verifier)
	if err != nil {
		connection.Status = models.ConnectionError
		connection.LastError = err.Error()
		connection.Pending = ""
		_, _ = tx.PutAgentConnection(connection)
		return self.serverView(server, connection), nil
	}
	encoded, _ := json.Marshal(tokens)
	sealed, err := worker.SealSecret(string(encoded))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	connection.Status = models.ConnectionConnected
	connection.Tokens = sealed
	connection.Pending = ""
	connection.LastError = ""
	connection.LastConnectedAt = &now
	stored, err := tx.PutAgentConnection(connection)
	if err != nil {
		return nil, translateError(err)
	}
	worker.ForgetConnection(server.Name, found.ID)
	log.Noticef("%s authorized the server %q for their agent", operatorName(ctx), server.Name)
	return self.serverView(server, stored), nil
}

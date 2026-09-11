// Package connected is the tool for the servers that speak the Model
// Context Protocol: what the operator has declared, what this person has
// connected, and the two steps of setting one up. The tools those servers
// offer arrive namespaced beside it; this is how they get there.
package connected

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "connected_server", Family: tools.FamilyServers, Risk: tools.RiskWrite,
				Description: "Servers speaking the Model Context Protocol (MCP), whose tools become yours: list what is declared and where this person stands with each, declare a new one at a URL, connect this person to one, or disconnect. Declaring changes the server's settings and needs the person to manage this server; connecting is theirs alone. A server reached over a command rather than a URL is the operator's to declare in the configuration, not here.",
				Parameters: tools.Object(map[string]any{
					"action":     tools.EnumProperty("what to do", "list", "declare", "connect", "disconnect"),
					"name":       tools.StringProperty("the server's short name, which namespaces its tools"),
					"url":        tools.StringProperty("for declare: the server's endpoint"),
					"auth":       tools.EnumProperty("for declare: how people authenticate, none by default", "none", "static", "user", "oauth"),
					"scopes":     tools.ArrayProperty("for declare with oauth: the scopes to ask for", tools.StringProperty("a scope")),
					"headless":   tools.BooleanProperty("for declare: whether runs with nobody present may use its read-only tools; false by default"),
					"read_only":  tools.ArrayProperty("for declare: the tools that only read, which need no confirmation", tools.StringProperty("a tool name")),
					"disabled":   tools.ArrayProperty("for declare: tools never offered", tools.StringProperty("a tool name")),
					"credential": tools.StringProperty("for connect to a server that takes the person's own credential"),
				}, "action"),
				Guidance: "connected_server: list first, because the operator may already have declared what is wanted. Declaring only says where a server is; the person still has to connect. A server that authorizes gives back an address only they can open, so hand it to them and stop -- you cannot sign in for them. Leave read_only empty unless you know which of the server's tools only read: everything not named there asks the person before it runs, which is the safe way round.",
				Preview: func(arguments json.RawMessage) string {
					var call request
					if err := json.Unmarshal(arguments, &call); err != nil {
						return "Connected servers: " + strings.TrimSpace(string(arguments))
					}
					switch call.Action {
					case "declare":
						return fmt.Sprintf("Declare the connected server %q at %s, and change this server's settings to keep it", call.Name, call.URL)
					case "disconnect":
						return fmt.Sprintf("Forget this person's connection to %q", call.Name)
					}
					return "Connected servers: " + strings.TrimSpace(string(arguments))
				},
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call request
					if err := json.Unmarshal(arguments, &call); err != nil {
						return tools.RiskWrite
					}
					switch call.Action {
					case "list":
						return tools.RiskRead
					case "declare":
						// It changes the whole server's settings, as
						// settings_update does, so it asks like one.
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Run: runConnected,
			},
		}
	})
}

type request struct {
	Action     string   `json:"action"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Auth       string   `json:"auth"`
	Scopes     []string `json:"scopes"`
	Headless   bool     `json:"headless"`
	ReadOnly   []string `json:"read_only"`
	Disabled   []string `json:"disabled"`
	Credential string   `json:"credential"`
}

// serverView is a declared server as the API lists it.
type serverView struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Auth      string `json:"auth"`
	Headless  bool   `json:"headless"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
	LastError string `json:"lastError,omitempty"`
}

const documentList = `query { ListAgentServers { name transport auth headless enabled status lastError } }`

func runConnected(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[request](call)
	if err != nil {
		return nil, err
	}
	run := tools.MustRun(ctx)
	switch arguments.Action {
	case "list":
		servers, err := listServers(ctx)
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(map[string]any{"servers": servers})

	case "declare":
		name := strings.TrimSpace(arguments.Name)
		address := strings.TrimSpace(arguments.URL)
		if name == "" || address == "" {
			return nil, fmt.Errorf("declaring a server needs a name and a url")
		}
		if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
			return nil, fmt.Errorf("a server's url is http or https; a server reached over a command is declared in the configuration by the operator")
		}
		declared, err := declare(ctx, &arguments, name, address)
		if err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(map[string]any{"declared": declared, "do_this_next": fmt.Sprintf("The server is declared. Nobody is connected to it yet: call connected_server with action connect and name %q, and give the person what it answers.", name)})
		if err != nil {
			return nil, err
		}
		result.Note = "declared the connected server " + name
		return result, nil

	case "connect":
		name := strings.TrimSpace(arguments.Name)
		if name == "" {
			return nil, fmt.Errorf("connecting needs the server's name; list gives them")
		}
		found, err := serverNamed(ctx, name)
		if err != nil {
			return nil, err
		}
		if found.Auth == "oauth" {
			address, err := beginOAuth(ctx, name)
			if err != nil {
				return nil, err
			}
			result, err := tools.JSONResult(map[string]any{
				"authorize": address,
				"do_this_next": "Give this address to the person and stop. They open it themselves and sign in; the dashboard finishes the connection when they come back. " +
					"You cannot sign in for them, and you must not ask them for the password.",
			})
			if err != nil {
				return nil, err
			}
			result.Note = "began authorizing " + name
			return result, nil
		}
		connected, err := connect(ctx, name, arguments.Credential)
		if err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(map[string]any{"connected": connected})
		if err != nil {
			return nil, err
		}
		result.Note = "connected " + name
		return result, nil

	case "disconnect":
		name := strings.TrimSpace(arguments.Name)
		if name == "" {
			return nil, fmt.Errorf("disconnecting needs the server's name")
		}
		var answer struct {
			DisconnectAgentServer *serverView `json:"DisconnectAgentServer"`
		}
		if err := run.Operations().Execute(ctx, `mutation ($server: String!) { DisconnectAgentServer(server: $server) { name status } }`, map[string]any{"server": name}, &answer); err != nil {
			return nil, err
		}
		result := tools.TextResult("forgot this person's connection to %s", name)
		result.Note = "disconnected " + name
		return result, nil
	}
	return nil, fmt.Errorf("%q is not an action of connected_server", arguments.Action)
}

func listServers(ctx context.Context) ([]*serverView, error) {
	var answer struct {
		ListAgentServers []*serverView `json:"ListAgentServers"`
	}
	if err := tools.MustRun(ctx).Operations().Execute(ctx, documentList, nil, &answer); err != nil {
		return nil, err
	}
	return answer.ListAgentServers, nil
}

func serverNamed(ctx context.Context, name string) (*serverView, error) {
	servers, err := listServers(ctx)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		if strings.EqualFold(server.Name, name) {
			return server, nil
		}
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no connected server is declared; declare one first")
	}
	return nil, fmt.Errorf("there is no connected server called %q; there are %s", name, strings.Join(names, ", "))
}

// carriedOver is every declared server but the one named, each with all
// of its fields, because the settings take the whole list and what is left
// out is left behind: an omitted enabled reads as false, which would
// switch off every other server on the way past. Secrets are kept by the
// name, and an environment value left empty keeps the stored one.
func carriedOver(configuration *config.Configuration, except string) []map[string]any {
	servers := []map[string]any{}
	for index := range configuration.Agent.MCP.Servers {
		existing := &configuration.Agent.MCP.Servers[index]
		if strings.EqualFold(existing.Name, except) {
			continue
		}
		environment := make([]string, 0, len(existing.Env))
		for _, variable := range existing.Env {
			environment = append(environment, variable.Name+"=")
		}
		servers = append(servers, map[string]any{
			"name": existing.Name, "transport": existing.Transport, "url": existing.URL,
			"command": existing.Command, "args": existing.Args, "workingDir": existing.WorkingDir,
			"env": environment, "auth": existing.Auth, "oauthClientId": existing.OAuth.ClientID,
			"oauthScopes": existing.OAuth.Scopes, "oauthAuthorizationUrl": existing.OAuth.AuthorizationURL,
			"oauthTokenUrl": existing.OAuth.TokenURL, "headless": existing.Headless,
			"readOnly": existing.ReadOnly, "disabled": existing.Disabled,
			"timeout": existing.Timeout.String(), "enabled": existing.IsEnabled(),
			"previousName": existing.Name,
		})
	}
	return servers
}

// declare adds the server to the agent settings, keeping every other one.
func declare(ctx context.Context, arguments *request, name, address string) (map[string]any, error) {
	run := tools.MustRun(ctx)
	servers := carriedOver(run.Configuration(), name)
	auth := strings.TrimSpace(arguments.Auth)
	if auth == "" {
		auth = "none"
	}
	added := map[string]any{
		"name": name, "transport": "http", "url": address, "auth": auth,
		"oauthScopes": arguments.Scopes, "headless": arguments.Headless,
		"readOnly": arguments.ReadOnly, "disabled": arguments.Disabled,
		"enabled": true,
	}
	servers = append(servers, added)
	var answer map[string]any
	if err := run.Operations().Execute(ctx, `mutation ($values: AgentParametersInput!) { UpdateSettings(agent: $values) { agent { enabled } } }`, map[string]any{"values": map[string]any{"mcpServers": servers}}, &answer); err != nil {
		return nil, err
	}
	return added, nil
}

func connect(ctx context.Context, name, credential string) (*serverView, error) {
	variables := map[string]any{"server": name}
	if strings.TrimSpace(credential) != "" {
		variables["credential"] = credential
	}
	var answer struct {
		ConnectAgentServer *serverView `json:"ConnectAgentServer"`
	}
	if err := tools.MustRun(ctx).Operations().Execute(ctx, `mutation ($server: String!, $credential: String) { ConnectAgentServer(server: $server, credential: $credential) { name status lastError } }`, variables, &answer); err != nil {
		return nil, err
	}
	return answer.ConnectAgentServer, nil
}

// beginOAuth asks for the address the person opens. The address the code
// comes back to is the dashboard's own, built the way a shared file's link
// is: the name passkeys are bound to when the operator set one, the
// server's own name otherwise.
func beginOAuth(ctx context.Context, name string) (string, error) {
	run := tools.MustRun(ctx)
	configuration := run.Configuration()
	host := strings.TrimSpace(configuration.Passkey.RelyingPartyID)
	if host == "" {
		host = strings.TrimSpace(configuration.Server.Name)
	}
	if host == "" {
		return "", fmt.Errorf("this server has no name to bring the authorization back to; the person can connect it on the Agent page instead")
	}
	redirect := "https://" + host + "/agent?connect=" + name
	var answer struct {
		BeginAgentServerOAuth string `json:"BeginAgentServerOAuth"`
	}
	if err := run.Operations().Execute(ctx, `mutation ($server: String!, $redirectUrl: String!) { BeginAgentServerOAuth(server: $server, redirectUrl: $redirectUrl) }`, map[string]any{"server": name, "redirectUrl": redirect}, &answer); err != nil {
		return "", err
	}
	return answer.BeginAgentServerOAuth, nil
}

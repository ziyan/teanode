package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// A reach is the computer one of a person's skills or connected servers
// makes its requests through, rather than through this server: how a service
// that answers only inside one network is reached at all. The person sets one
// per skill and per server; the agent may name another computer for a single
// call, and is asked first when it does.

type AgentReachQuery interface {
	// Every skill and connected server that can go through a computer, with
	// the reach the caller set for it. Needs agent:use.
	ListAgentReaches(ctx context.Context) ([]*AgentReachView, error)
}

type AgentReachMutation interface {
	// Set the reach of one skill or server; an empty computer name puts it
	// back through this server. Needs agent:use.
	SetAgentReach(ctx context.Context, arguments SetAgentReachArguments) (*AgentReachView, error)
}

// AgentReachView is one skill or server and the computer it goes through.
type AgentReachView struct {
	// Kind is "skill" or "server".
	Kind string `json:"kind"`
	Name string `json:"name"`

	// ComputerName is the computer its requests go through, or empty for
	// through this server.
	ComputerName string `json:"computerName"`

	// IsOnComputer says a server runs as a command on the person's computer
	// rather than being called over HTTP: its reach says which computer it
	// runs on, and it has no "through this server" to go back to.
	IsOnComputer bool `json:"isOnComputer"`
}

type SetAgentReachArguments struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	ComputerName string `json:"computerName"`
}

func (self *graph) ListAgentReaches(ctx context.Context) ([]*AgentReachView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return []*AgentReachView{}, nil
		}
		return nil, err
	}
	tx := self.transaction(ctx)
	reaches, err := tx.ListAgentReaches(found.ID)
	if err != nil {
		return nil, err
	}
	set := map[string]string{}
	for _, reach := range reaches {
		set[reach.Kind+"\x00"+strings.ToLower(reach.Name)] = reach.ComputerName
	}
	views := []*AgentReachView{}
	installed, err := tx.ListAgentSkills()
	if err != nil {
		return nil, err
	}
	for _, skill := range installed {
		if !skill.Enabled {
			continue
		}
		views = append(views, &AgentReachView{
			Kind: models.AgentReachSkill, Name: skill.Name,
			ComputerName: set[models.AgentReachSkill+"\x00"+strings.ToLower(skill.Name)],
		})
	}
	for _, server := range reachableServers(self.config.Current()) {
		views = append(views, &AgentReachView{
			Kind: models.AgentReachServer, Name: server.Name,
			ComputerName: set[models.AgentReachServer+"\x00"+strings.ToLower(server.Name)],
			IsOnComputer: server.ResolvedTransport() == config.AgentMCPTransportStdio,
		})
	}
	return views, nil
}

func (self *graph) SetAgentReach(ctx context.Context, arguments SetAgentReachArguments) (*AgentReachView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(arguments.Name)
	view := &AgentReachView{Kind: arguments.Kind, Name: name, ComputerName: strings.TrimSpace(arguments.ComputerName)}
	switch arguments.Kind {
	case models.AgentReachSkill:
		skill, err := self.transaction(ctx).GetAgentSkill(name)
		if err != nil {
			return nil, err
		}
		if skill == nil {
			return nil, fmt.Errorf("%w: no skill called %q is installed", api.ErrNotFound, name)
		}
		view.Name = skill.Name
	case models.AgentReachServer:
		var server *config.AgentMCPServer
		for _, candidate := range reachableServers(self.config.Current()) {
			if strings.EqualFold(candidate.Name, name) {
				server = candidate
			}
		}
		if server == nil {
			return nil, fmt.Errorf("%w: no connected server called %q can go through a computer", api.ErrNotFound, name)
		}
		view.Name = server.Name
		view.IsOnComputer = server.ResolvedTransport() == config.AgentMCPTransportStdio
	default:
		return nil, fmt.Errorf("%w: %q is neither a skill nor a server", api.ErrInvalidArguments, arguments.Kind)
	}
	if err := self.transaction(ctx).PutAgentReach(&models.AgentReach{
		AgentID: found.ID, Kind: view.Kind, Name: view.Name, ComputerName: view.ComputerName,
	}); err != nil {
		return nil, translateError(err)
	}
	// A server's sessions were opened through the old reach; the next call
	// opens one through the new.
	if view.Kind == models.AgentReachServer {
		if worker := self.agentWorker(); worker != nil {
			worker.ForgetConnection(view.Name, found.ID)
		}
	}
	return view, nil
}

// reachableServers are the declared servers that can go through a computer:
// one called over HTTP, and one that runs as a command on the person's
// computer. One that runs as a command here has nothing to go through.
func reachableServers(configuration *config.Configuration) []*config.AgentMCPServer {
	var servers []*config.AgentMCPServer
	for index := range configuration.Agent.MCP.Servers {
		server := &configuration.Agent.MCP.Servers[index]
		if !server.IsEnabled() {
			continue
		}
		if server.ResolvedTransport() == config.AgentMCPTransportStdio && server.ResolvedLocation() != config.AgentMCPLocationComputer {
			continue
		}
		servers = append(servers, server)
	}
	return servers
}

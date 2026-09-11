package server

import (
	"fmt"
	"github.com/ziyan/teanode/internal/api"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
)

// openAgent builds the model registry, or returns nil when the agent is
// off. Nil is the whole of "no agent": nothing downstream is constructed,
// and no client exists that could reach a model service — the same rule
// the other optional integrations follow.
func openAgent(configuration *config.Configuration) (*llm.Registry, error) {
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		return nil, fmt.Errorf("cannot set up the agent: %w", err)
	}
	if registry == nil {
		return nil, nil
	}
	providers := 0
	for _, provider := range configuration.Agent.Providers {
		if provider.IsEnabled() {
			providers++
		}
	}
	log.Noticef("agent: enabled, %d provider(s), default model %s", providers, configuration.Agent.Models.Default)
	return registry, nil
}

// openAgentWorker builds the worker when the agent is on and tells the
// exchange about it. With the agent off nothing is built.
func (self *server) openAgentWorker(configuration *config.Configuration) error {
	registry, err := openAgent(configuration)
	if err != nil {
		return err
	}
	if registry == nil {
		return nil
	}
	self.agentRegistry = registry
	self.agentWorker = agent.New(&agent.Settings{
		Database:      self.database,
		Storage:       self.storage,
		Registry:      registry,
		Exchange:      self.exchange,
		Configuration: self.store.Current,
		Instance:      self.instance,
	})
	self.exchange.SetAgentHook(self.agentWorker)
	self.agentWorker.Start()
	self.onClose(self.agentWorker.Stop)
	return nil
}

// agentService is the worker as the API sees it: nil when agents are off,
// rather than an interface holding a nil pointer, which is not nil.
func (self *server) agentService() api.AgentService {
	if self.agentWorker == nil {
		return nil
	}
	return self.agentWorker
}

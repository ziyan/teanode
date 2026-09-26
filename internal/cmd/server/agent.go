package server

import (
	"fmt"
	"github.com/ziyan/teanode/internal/api"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/channel"
	"github.com/ziyan/teanode/internal/channel/discord"
	"github.com/ziyan/teanode/internal/channel/telegram"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/scheduling"
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
	self.keepSignIns(registry)
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
	// The chat apps: one bot per person and app, run on one instance.
	channels := channel.New(&channel.Settings{
		Worker: self.agentWorker, Database: self.database, Storage: self.storage,
		Configuration: self.store.Current, Instance: self.instance,
		Openers: map[models.AgentChannelKind]channel.Opener{models.AgentChannelTelegram: telegram.Open, models.AgentChannelDiscord: discord.Open},
	})
	channels.Start()
	self.onClose(channels.Stop)
	return nil
}

// keepSignIns ties the signed-in providers' refresh tokens to the
// configuration both ways: a token the service rotates is written back, so
// the sign-in survives a restart, and a token the configuration is given --
// a new sign-in from the dashboard -- is adopted by the running provider.
func (self *server) keepSignIns(registry *llm.Registry) {
	registry.KeepRefreshTokens(func(provider, refreshToken string) {
		err := self.store.Update(func(configuration *config.Configuration) error {
			for index := range configuration.Agent.Providers {
				if configuration.Agent.Providers[index].Name == provider {
					configuration.Agent.Providers[index].RefreshToken = refreshToken
				}
			}
			return nil
		})
		if err != nil {
			log.Warningf("the %s provider was given a new refresh token and it could not be kept; it will need signing in again after a restart: %s", provider, err)
			return
		}
		log.Noticef("kept the new refresh token of the %s provider", provider)
	})
	self.onClose(self.store.Subscribe(func(configuration *config.Configuration) {
		for _, provider := range configuration.Agent.Providers {
			if provider.Kind == config.AgentProviderKindCodex {
				registry.AdoptRefreshToken(provider.Name, provider.RefreshToken)
			}
		}
	}))
}

// agentService is the worker as the API sees it: nil when agents are off,
// rather than an interface holding a nil pointer, which is not nil.
func (self *server) agentService() api.AgentService {
	if self.agentWorker == nil {
		return nil
	}
	return self.agentWorker
}

// openScheduler starts the worker that reads invitations arriving as mail,
// and tells the exchange to note them.
//
// Always, unlike the agent: an invitation needs no language model and no
// opting in. Somebody who was sent a meeting request wants it in their
// calendar whether or not they have ever thought about an agent.
func (self *server) openScheduler() {
	self.scheduler = scheduling.New(self.database, self.storage, scheduling.Settings{
		Instance: self.instance,
	})
	self.exchange.SetCalendarHook(self.scheduler)
	self.scheduler.Start()
	self.onClose(self.scheduler.Stop)
}

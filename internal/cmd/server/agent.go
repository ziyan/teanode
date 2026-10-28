package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/channel"
	"github.com/ziyan/teanode/internal/channel/discord"
	"github.com/ziyan/teanode/internal/channel/telegram"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
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

// openChatHub starts what carries a chat to its members. Always, unlike the
// agent: two people in a circle can write to each other on a server with no
// language model, and need the same live feed, read marks and phone
// notifications.
func (self *server) openChatHub() {
	self.chatHub = chat.New(&chat.Settings{
		Database:      self.database,
		Configuration: self.store.Current,
		Instance:      self.instance,
	})
	self.chatHub.Start()
	self.onClose(self.chatHub.Stop)
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
	self.keepPlanUsage(registry)
	self.agentWorker = agent.New(&agent.Settings{
		Database: self.database,
		Storage:  self.storage,
		Registry: registry,
		Exchange: self.exchange,
		Configuration: func() *config.Configuration {
			current := *self.store.Current()
			current.Services = current.Services.Within(configuration.Services)
			return &current
		},
		UpdateConfiguration: self.store.Update,
		Instance:            self.instance,
		// What the agent shares with the chat hub: who is looking at the
		// dashboard or the app, and the phones' sender.
		Presence:   self.chatHub.Presence(),
		Phones:     self.chatHub.Phones(),
		ChatEvents: self.chatHub,
	})
	if self.exchange != nil {
		self.exchange.SetAgentHook(self.agentWorker)
	}
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
// the sign-in survives a restart and the other instances hear of it, and a
// token the configuration is given -- a new sign-in from the dashboard, or a
// rotation another instance made -- is adopted by the running provider.
func (self *server) keepSignIns(registry *llm.Registry) {
	registry.ShareSignIns(&signInStore{configurationStore: self.store, database: self.database})
	self.onClose(self.store.Subscribe(func(configuration *config.Configuration) {
		for _, provider := range configuration.Agent.Providers {
			if provider.Kind == config.AgentProviderKindCodex {
				registry.AdoptRefreshToken(provider.Name, provider.RefreshToken)
			}
		}
	}))
}

// signInLockWait is how long a refresh waits for another instance's.
const signInLockWait = time.Minute

// signInStore is where the instances of one server keep a provider's sign-in
// between them: the refresh token in the configuration, the access token in
// the database beside it, and a database lock that makes one instance refresh
// at a time. See llm.SignInStore.
type signInStore struct {
	configurationStore config.Store
	database           db.Database
}

func (self *signInStore) WhileLocked(ctx context.Context, provider string, work func(context.Context) error) error {
	// A turn's context may have no deadline, and an instance stuck holding
	// the lock should cost the others a failed request, not a hung one.
	waitContext, cancel := context.WithTimeout(ctx, signInLockWait)
	defer cancel()
	release, err := self.database.LockSession(waitContext, db.LockKey("llm-sign-in:"+provider))
	if err != nil {
		return err
	}
	defer release()
	return work(ctx)
}

func (self *signInStore) LoadRefreshToken(_ context.Context, provider string) (string, error) {
	// The copy in memory is as old as the last time the configuration was
	// polled, which is what the lock is there to get past.
	if err := self.configurationStore.Reload(); err != nil {
		return "", err
	}
	declared := self.configurationStore.Current().Agent.Provider(provider)
	if declared == nil {
		return "", nil
	}
	return declared.RefreshToken, nil
}

func (self *signInStore) LoadAccessToken(ctx context.Context, provider string) (*llm.SignInAccessToken, error) {
	var stored *db.AgentProviderAccessToken
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		stored, err = tx.GetAgentProviderAccessToken(provider)
		return err
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &llm.SignInAccessToken{AccessToken: stored.AccessToken, ForRefreshToken: stored.ForRefreshToken, ExpiresAt: stored.ExpiresAt}, nil
}

func (self *signInStore) Save(ctx context.Context, provider, refreshToken string, accessToken *llm.SignInAccessToken) error {
	// Unchanged is the common case where the service does not rotate, and a
	// write wakes every other instance to reload the configuration.
	if declared := self.configurationStore.Current().Agent.Provider(provider); declared == nil || declared.RefreshToken != refreshToken {
		err := self.configurationStore.Update(func(configuration *config.Configuration) error {
			for index := range configuration.Agent.Providers {
				if configuration.Agent.Providers[index].Name == provider {
					configuration.Agent.Providers[index].RefreshToken = refreshToken
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		log.Noticef("kept the new refresh token of the %s provider", provider)
	}
	return self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutAgentProviderAccessToken(&db.AgentProviderAccessToken{
			ProviderName: provider, AccessToken: accessToken.AccessToken,
			ForRefreshToken: accessToken.ForRefreshToken, ExpiresAt: accessToken.ExpiresAt,
		})
	})
}

// keepPlanUsage writes down what each plan says of its allowance, and hands
// back what was written before the restart, so the dashboard shows the last
// reading rather than nothing until the plan next answers.
func (self *server) keepPlanUsage(registry *llm.Registry) {
	var readings map[string][]byte
	err := self.database.Transaction(func(tx db.Transaction) (err error) {
		readings, err = tx.ListAgentPlanUsages()
		return err
	})
	if err != nil {
		log.Warningf("could not read the plans' last usage; each shows nothing until it next answers: %s", err)
	}
	for provider, written := range readings {
		var usage llm.PlanUsage
		if err := json.Unmarshal(written, &usage); err != nil {
			log.Warningf("the %s provider's last plan usage could not be read: %s", provider, err)
			continue
		}
		registry.RestorePlanUsage(provider, &usage)
	}
	registry.KeepPlanUsage(func(provider string, usage *llm.PlanUsage) {
		written, err := json.Marshal(usage)
		if err == nil {
			err = self.database.Transaction(func(tx db.Transaction) error {
				return tx.PutAgentPlanUsage(provider, usage.ObservedAt, written)
			})
		}
		if err != nil {
			log.Warningf("the %s provider's plan usage could not be kept; a restart will show none until it next answers: %s", provider, err)
		}
	})
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
	if !self.store.Current().Services.IsEnabled(models.ServiceCalendar) {
		return
	}
	self.scheduler = scheduling.New(self.database, self.storage, scheduling.Settings{
		Instance:          self.instance,
		IsCalendarEnabled: func() bool { return self.store.Current().Services.IsEnabled(models.ServiceCalendar) },
		IsMailEnabled:     func() bool { return self.store.Current().Services.IsEnabled(models.ServiceMailboxes) },
	})
	if self.exchange != nil {
		self.exchange.SetCalendarHook(self.scheduler)
	}
	self.scheduler.Start()
	self.onClose(self.scheduler.Stop)
}

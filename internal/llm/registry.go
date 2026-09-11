package llm

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// modelsCacheLifetime is how long a provider's model list is kept. Five
// minutes: long enough that a settings page opened twice does not ask
// twice, short enough that a model pulled into a local server appears
// before anyone wonders why it has not.
const modelsCacheLifetime = 5 * time.Minute

// Registry holds one client per enabled provider and answers the question
// the rest of the server asks: which provider and model does this kind of
// work.
type Registry struct {
	configuration config.Agent
	providers     map[string]*providerEntry

	cacheMutex sync.Mutex
	cache      map[string]cachedModels
}

type providerEntry struct {
	configuration config.AgentProvider
	provider      Provider
}

type cachedModels struct {
	models    []ModelInformation
	expiresAt time.Time
}

// ProviderModel is one model, named the way the configuration names it.
type ProviderModel struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Name          string `json:"name"` // "provider:model"
	ContextLength int    `json:"contextLength,omitempty"`
}

// Open builds the registry, or returns nil when the agent is disabled. A
// nil registry is the whole of "no model": nothing downstream is
// constructed, and no client exists that could dial anything.
func Open(configuration *config.Agent) (*Registry, error) {
	if configuration == nil || !configuration.Enabled {
		return nil, nil
	}
	registry := &Registry{
		configuration: *configuration,
		providers:     map[string]*providerEntry{},
		cache:         map[string]cachedModels{},
	}
	timeout := configuration.Limits.RequestTimeout.Duration()
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	for _, declared := range configuration.Providers {
		if !declared.IsEnabled() {
			continue
		}
		provider, err := NewProvider(declared.Kind, declared.BaseURL, declared.APIKey, timeout)
		if err != nil {
			return nil, fmt.Errorf("llm: provider %q: %w", declared.Name, err)
		}
		registry.providers[declared.Name] = &providerEntry{configuration: declared, provider: provider}
	}
	if len(registry.providers) == 0 {
		return nil, fmt.Errorf("llm: the agent is enabled but no provider is")
	}
	return registry, nil
}

// Configuration is what the registry was built from.
func (self *Registry) Configuration() *config.Agent {
	return &self.configuration
}

// Limits is the operator's limits.
func (self *Registry) Limits() config.AgentLimits {
	return self.configuration.Limits
}

// resolve turns "provider:model" into a client and a model name.
func (self *Registry) resolve(name string) (*providerEntry, string, error) {
	providerName, model := config.SplitModelName(name)
	if providerName == "" || model == "" {
		return nil, "", fmt.Errorf("llm: %q is not written provider:model", name)
	}
	entry := self.providers[providerName]
	if entry == nil {
		return nil, "", fmt.Errorf("llm: provider %q is not declared or not enabled", providerName)
	}
	if !entry.configuration.Models.Admits(model) {
		return nil, "", fmt.Errorf("llm: provider %q does not admit model %q", providerName, model)
	}
	return entry, model, nil
}

// ForWork is the provider and model assigned to a kind of work.
func (self *Registry) ForWork(work config.AgentWork) (Provider, string, error) {
	name := self.configuration.Models.ForWork(work)
	if name == "" {
		return nil, "", fmt.Errorf("llm: no model is assigned to %s", work)
	}
	entry, model, err := self.resolve(name)
	if err != nil {
		return nil, "", err
	}
	return entry.provider, model, nil
}

// ForModel is the provider behind an explicit "provider:model", for the
// one place a person's own choice is honoured.
func (self *Registry) ForModel(name string) (Provider, string, error) {
	entry, model, err := self.resolve(name)
	if err != nil {
		return nil, "", err
	}
	return entry.provider, model, nil
}

// Embedding is the embedder and model for search by meaning, or an error
// saying there is none.
func (self *Registry) Embedding() (Embedder, string, error) {
	name := self.configuration.Models.Embedding
	if name == "" {
		return nil, "", fmt.Errorf("llm: no embedding model is configured")
	}
	entry, model, err := self.resolve(name)
	if err != nil {
		return nil, "", err
	}
	embedder, ok := entry.provider.(Embedder)
	if !ok {
		return nil, "", fmt.Errorf("llm: provider %q cannot embed", entry.configuration.Name)
	}
	return embedder, model, nil
}

// Pricing is what a provider charges, for the usage view.
func (self *Registry) Pricing(provider string) config.AgentPricing {
	if entry := self.providers[provider]; entry != nil {
		return entry.configuration.Pricing
	}
	return config.AgentPricing{}
}

// ListModels is every enabled provider's models through its filter,
// cached. A provider that cannot be reached contributes nothing and the
// error is logged; the settings page has Test for the specific answer.
func (self *Registry) ListModels(ctx context.Context) []ProviderModel {
	var models []ProviderModel
	for name, entry := range self.providers {
		listed, err := self.listProvider(ctx, name, entry, false)
		if err != nil {
			log.Warningf("cannot list the models of provider %q: %s", name, err)
			continue
		}
		models = append(models, listed...)
	}
	sort.Slice(models, func(left, right int) bool { return models[left].Name < models[right].Name })
	return models
}

// TestProvider asks one provider for its models now, ignoring the cache,
// which is what the settings page's Test button does.
func (self *Registry) TestProvider(ctx context.Context, name string) ([]ProviderModel, error) {
	entry := self.providers[name]
	if entry == nil {
		return nil, fmt.Errorf("llm: provider %q is not declared or not enabled", name)
	}
	return self.listProvider(ctx, name, entry, true)
}

func (self *Registry) listProvider(ctx context.Context, name string, entry *providerEntry, fresh bool) ([]ProviderModel, error) {
	self.cacheMutex.Lock()
	cached, ok := self.cache[name]
	self.cacheMutex.Unlock()
	var listed []ModelInformation
	if ok && !fresh && time.Now().Before(cached.expiresAt) {
		listed = cached.models
	} else {
		var err error
		listed, err = entry.provider.ListModels(ctx)
		if err != nil {
			return nil, err
		}
		self.cacheMutex.Lock()
		self.cache[name] = cachedModels{models: listed, expiresAt: time.Now().Add(modelsCacheLifetime)}
		self.cacheMutex.Unlock()
	}
	return FilterModels(name, &entry.configuration.Models, listed), nil
}

// FilterModels applies a provider's allow and deny filter and names each
// model the way the configuration does.
func FilterModels(provider string, filter *config.AgentProviderModels, listed []ModelInformation) []ProviderModel {
	models := make([]ProviderModel, 0, len(listed))
	for _, model := range listed {
		if !filter.Admits(model.ID) {
			continue
		}
		models = append(models, ProviderModel{
			Provider:      provider,
			Model:         model.ID,
			Name:          provider + ":" + model.ID,
			ContextLength: model.ContextLength,
		})
	}
	return models
}

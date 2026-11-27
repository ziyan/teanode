package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// AgentModelEntryMutation adds, changes and removes the models a provider
// offers one at a time, the way the provider's page does, without sending
// the whole agent section back.
type AgentModelEntryMutation interface {
	// Add a model to a provider, or change the entry with the same id.
	// Answers with the entry as it is now. Refused when the result would
	// not be a valid configuration: a window under a thousand tokens, a
	// negative price, or switching off a model a role uses. Needs
	// server:manage, as saving the agent settings does.
	SaveAgentModelEntry(ctx context.Context, arguments SaveAgentModelEntryArguments) (*AgentModelEntry, error)

	// Remove a model from a provider. Refused while a role uses it, and
	// the refusal names the role. Needs server:manage.
	DeleteAgentModelEntry(ctx context.Context, arguments DeleteAgentModelEntryArguments) (bool, error)
}

// SaveAgentModelEntryArguments name the provider and give the entry.
type SaveAgentModelEntryArguments struct {
	ProviderName string          `json:"providerName"`
	Entry        AgentModelEntry `json:"entry"`
}

// DeleteAgentModelEntryArguments name the provider and the model's id.
type DeleteAgentModelEntryArguments struct {
	ProviderName string `json:"providerName"`
	Model        string `json:"model"`
}

func (self *graph) SaveAgentModelEntry(ctx context.Context, arguments SaveAgentModelEntryArguments) (*AgentModelEntry, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	providerName := strings.TrimSpace(arguments.ProviderName)
	entry := arguments.Entry.configEntry()
	if entry.Model == "" {
		return nil, fmt.Errorf("%w: name the model by the id its provider knows it by", api.ErrInvalidArguments)
	}
	if err := self.config.Update(func(configuration *config.Configuration) error {
		provider := configuration.Agent.Provider(providerName)
		if provider == nil {
			return fmt.Errorf("%w: no provider called %q is declared", api.ErrNotFound, providerName)
		}
		for index := range provider.ModelEntries {
			if provider.ModelEntries[index].Model == entry.Model {
				provider.ModelEntries[index] = entry
				return nil
			}
		}
		provider.ModelEntries = append(provider.ModelEntries, entry)
		return nil
	}); err != nil {
		return nil, err
	}
	agent := &self.config.Current().Agent
	provider := agent.Provider(providerName)
	log.Noticef("%s saved the model %s of the %s provider", api.ContextAuthenticatedUsername(ctx), entry.Model, providerName)
	for _, saved := range modelEntrySettings(agent, provider) {
		if saved.Model == entry.Model {
			self.withReportedContextWindow(providerName, saved)
			return saved, nil
		}
	}
	return nil, fmt.Errorf("the model %s was saved but cannot be read back", entry.Model)
}

func (self *graph) DeleteAgentModelEntry(ctx context.Context, arguments DeleteAgentModelEntryArguments) (bool, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return false, err
	}
	providerName, model := strings.TrimSpace(arguments.ProviderName), strings.TrimSpace(arguments.Model)
	if err := self.config.Update(func(configuration *config.Configuration) error {
		provider := configuration.Agent.Provider(providerName)
		if provider == nil {
			return fmt.Errorf("%w: no provider called %q is declared", api.ErrNotFound, providerName)
		}
		index := -1
		for position := range provider.ModelEntries {
			if provider.ModelEntries[position].Model == model {
				index = position
			}
		}
		if index < 0 {
			return fmt.Errorf("%w: provider %q has no model %q", api.ErrNotFound, providerName, model)
		}
		if roles := configuration.Agent.RolesUsing(providerName, model); len(roles) > 0 {
			return fmt.Errorf("%w: %s:%s is still used for %s; choose another model for that first", api.ErrInvalidArguments, providerName, model, strings.Join(roles, ", "))
		}
		provider.ModelEntries = append(provider.ModelEntries[:index:index], provider.ModelEntries[index+1:]...)
		return nil
	}); err != nil {
		return false, err
	}
	log.Noticef("%s removed the model %s of the %s provider", api.ContextAuthenticatedUsername(ctx), model, providerName)
	return true, nil
}

// withReportedContextWindows adds to each provider's models the window the
// provider last listed them with, from what the registry kept. A provider
// that has not been asked since the server started adds nothing, and a model
// reads 0.
func (self *graph) withReportedContextWindows(settings *AgentSettings) {
	if settings == nil {
		return
	}
	for _, provider := range settings.Providers {
		for _, entry := range provider.Models {
			self.withReportedContextWindow(provider.Name, entry)
		}
	}
}

func (self *graph) withReportedContextWindow(providerName string, entry *AgentModelEntry) {
	worker := self.agentWorker()
	if worker == nil || entry == nil {
		return
	}
	entry.ReportedContextWindow = worker.ReportedContextLengths(providerName)[entry.Model]
}

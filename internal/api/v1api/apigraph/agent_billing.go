package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Model prices that follow the provider's bill. An openai provider with a
// billing key is checked once a day: what the bill says each model and
// kind of token costs is set beside the configured price, and the price is
// changed where it is missing or differs by more than five percent.

// AgentBillingQuery reads what the checks did.
type AgentBillingQuery interface {
	// What the last check of each openai provider that has a billing key
	// compared and changed, and why it could not read the bill if it
	// could not. A provider not yet checked is listed with an empty
	// time. Needs server:manage.
	AgentBillingChecks(ctx context.Context) ([]*models.AgentBillingCheck, error)
}

// AgentBillingMutation runs a check now.
type AgentBillingMutation interface {
	// Compare the prices of each openai provider that has a billing key
	// with its bill now instead of at the next daily check, change the
	// ones that differ, and tell the operators of any change. Needs
	// server:manage.
	CheckAgentProviderPrices(ctx context.Context) ([]*models.AgentBillingCheck, error)
}

func (self *graph) AgentBillingChecks(ctx context.Context) ([]*models.AgentBillingCheck, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	checks := []*models.AgentBillingCheck{}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		for _, provider := range self.config.Current().Agent.Providers {
			if provider.Kind != config.AgentProviderKindOpenAI || provider.BillingAPIKey == "" {
				continue
			}
			check, err := tx.GetAgentBillingCheck(provider.Name)
			if err != nil {
				return err
			}
			if check == nil {
				check = &models.AgentBillingCheck{ProviderName: provider.Name, Compared: []models.AgentBilledPrice{}, Changed: []models.AgentPriceChange{}}
			}
			checks = append(checks, check)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return checks, nil
}

func (self *graph) CheckAgentProviderPrices(ctx context.Context) ([]*models.AgentBillingCheck, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	checks, err := worker.CheckProviderPrices(ctx, true)
	if err != nil {
		return nil, err
	}
	if checks == nil {
		checks = []*models.AgentBillingCheck{}
	}
	return checks, nil
}

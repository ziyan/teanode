package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// isFinanceOffered says the operator offers at least one provider people
// can link an institution through.
func isFinanceOffered(configuration *config.Configuration) bool {
	for _, providerKind := range config.AgentFinanceProviders {
		if configuration.Agent.Finance.Offers(providerKind) {
			return true
		}
	}
	return false
}

// hasFinanceData says the agent holds finance data already: a finance
// source or an asset. Its person keeps the finance tool for reading what is
// stored after the operator stops offering providers.
func (self *Agent) hasFinanceData(ctx context.Context, agentId string) bool {
	hasData := false
	if self.settings.Database == nil {
		return false
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		sources, err := tx.ListAgentSources(agentId)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if source.Kind == models.SourceFinance {
				hasData = true
				return nil
			}
		}
		assets, err := tx.ListAssets(agentId)
		hasData = len(assets) > 0
		return err
	}); err != nil {
		log.Warningf("cannot tell whether agent %q holds finance data: %s", agentId, err)
	}
	return hasData
}

// withoutFinanceTools is the offered tools less the finance family, unless
// the server offers a provider or the person already holds finance data:
// on a server that offers none, the tool could only say so.
func (self *Agent) withoutFinanceTools(ctx context.Context, configuration *config.Configuration, agentId string, offered []*tools.Tool) []*tools.Tool {
	if isFinanceOffered(configuration) {
		return offered
	}
	hasFinanceTool := false
	for _, tool := range offered {
		hasFinanceTool = hasFinanceTool || tool.Family == tools.FamilyFinance
	}
	if !hasFinanceTool || self.hasFinanceData(ctx, agentId) {
		return offered
	}
	kept := offered[:0:0]
	for _, tool := range offered {
		if tool.Family != tools.FamilyFinance {
			kept = append(kept, tool)
		}
	}
	return kept
}

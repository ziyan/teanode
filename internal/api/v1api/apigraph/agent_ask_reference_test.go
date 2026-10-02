package apigraph

import (
	"context"
	"errors"
	"testing"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A turn pointing at a finance transaction is refused unless the
// transaction is the asking agent's own, as a file that is not its own
// is; the owner's passes the check and goes on to the agent, which here
// has no model to answer with.
func TestAskAgentRefusesAnotherAgentsFinanceTransaction(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	arguments := AskAgentArguments{
		Message:    "What is this charge?",
		References: []models.AgentReference{{FinanceTransactionID: transactions[0].ID}},
	}
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.AskAgent(ctx, arguments); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person's transaction answered %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.AskAgent(ctx, arguments); !errors.Is(err, agentpackage.ErrUnavailable) {
			test.Errorf("the owner's transaction answered %v", err)
		}
	})
}

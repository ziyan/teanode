package agent_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A budget said in money: the day is priced at each model's own
// provider's prices, the person's own limit beats the server's default,
// and a run over it is deferred with the amount named in the currency
// the operator chose.
func TestBudgetInMoney(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Currency = "eur"
	configuration.Agent.Providers = []config.AgentProvider{
		// A million prompt tokens costs 2, a million completion 10.
		{Name: "one", Kind: "openai", BaseURL: "http://x.example", APIKey: "k", Pricing: config.AgentPricing{Input: 2, Output: 10}},
		{Name: "two", Kind: "openai", BaseURL: "http://y.example", APIKey: "k", Pricing: config.AgentPricing{Input: 1, Output: 1}},
	}
	// No token budget at all: money is the only cap.
	configuration.Agent.Limits.DailyTokensPerAgent = 0
	configuration.Agent.Limits.DailyCostPerAgent = 5

	var owner *models.User
	var found *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// A million prompt tokens on each provider: 2 + 1 = 3 of the 5.
		for _, model := range []string{"one:thinker", "two:thinker"} {
			if err := tx.PutAgentUsage(&db.AgentUsage{AgentID: found.ID, Model: model, Kind: "ask", At: time.Now(), Values: []uint64{1_000_000, 0, 0, 0, 1}}); err != nil {
				t.Fatalf("PutAgentUsage: %s", err)
			}
		}
		budget, err := agent.CheckBudget(tx, configuration, found, owner, time.Now())
		if err != nil {
			t.Fatalf("CheckBudget: %s", err)
		}
		if budget.Cost < 2.99 || budget.Cost > 3.01 {
			t.Fatalf("each model is priced by its own provider: %v", budget.Cost)
		}
		if budget.CostLimit != 5 || budget.Currency != "EUR" {
			t.Fatalf("the operator's limit and currency: %v %q", budget.CostLimit, budget.Currency)
		}
		if budget.Exhausted() {
			t.Fatal("three of five is not the whole budget")
		}
		if err := agent.RequireBudget(tx, configuration, found, owner, time.Now()); err != nil {
			t.Fatalf("a run inside the budget: %s", err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// Another million on the dearer one takes it past five.
		if err := tx.PutAgentUsage(&db.AgentUsage{AgentID: found.ID, Model: "one:thinker", Kind: "ask", At: time.Now(), Values: []uint64{0, 300_000, 0, 0, 1}}); err != nil {
			t.Fatalf("PutAgentUsage: %s", err)
		}
		err := agent.RequireBudget(tx, configuration, found, owner, time.Now())
		deferral, ok := err.(*agent.Deferral)
		if !ok {
			t.Fatalf("expected a deferral, got %v", err)
		}
		if deferral.Reason != "the daily budget of 5.00 EUR is used up" {
			t.Fatalf("the reason names the money and the currency: %q", deferral.Reason)
		}
	})

	// The person's own limit is the one that counts.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		updated, err := tx.UpdateAgent(found.ID, func(agent *models.Agent) error {
			agent.DailyCost = 50
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		budget, err := agent.CheckBudget(tx, configuration, updated, owner, time.Now())
		if err != nil {
			t.Fatalf("CheckBudget: %s", err)
		}
		if budget.CostLimit != 50 || budget.Exhausted() {
			t.Fatalf("their own limit, with room in it: %+v", budget)
		}
	})

	// Nothing priced and nothing capped is no budget at all.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		bare := config.Default()
		bare.Agent.Enabled = true
		bare.Agent.Limits.DailyTokensPerAgent = 0
		budget, err := agent.CheckBudget(tx, bare, found, owner, time.Now())
		if err != nil {
			t.Fatalf("CheckBudget: %s", err)
		}
		if budget.Exhausted() || budget.Currency != "USD" {
			t.Fatalf("no cap, and dollars unless the operator says otherwise: %+v", budget)
		}
	})
}

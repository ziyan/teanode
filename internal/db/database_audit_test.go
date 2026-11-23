package db_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

func auditEventsOf(t *testing.T, tx db.Transaction, resourceType models.AuditResourceType, resourceId string) []*models.AuditEvent {
	t.Helper()
	events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(resourceType), ResourceID: resourceId})
	if err != nil {
		t.Fatalf("ListAuditEvents: %s", err)
	}
	return events
}

// A dream finishing writes no row; a person's change writes one with only
// what changed.
func TestAgentAuditSkipsBookkeeping(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "audit-bookkeeping")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		baseline := len(auditEventsOf(t, tx, models.AuditResourceAgent, fixture.agentId))
		now := time.Now()
		if _, err := tx.UpdateAgent(fixture.agentId, func(agent *models.Agent) error {
			agent.DreamedAt = &now
			agent.DecayedAt = &now
			agent.DreamBootstrap = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		if _, err := tx.UpdateAgent(fixture.agentId, func(agent *models.Agent) error {
			agent.Categories = []models.AgentCategory{}
			agent.Confirm = []string{}
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		if have := len(auditEventsOf(t, tx, models.AuditResourceAgent, fixture.agentId)); have != baseline {
			t.Fatalf("bookkeeping and an empty list for a nil one wrote %d rows", have-baseline)
		}

		if _, err := tx.UpdateAgent(fixture.agentId, func(agent *models.Agent) error {
			agent.Name = "Renamed Agent"
			later := now.Add(time.Hour)
			agent.DreamedAt = &later
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		events := auditEventsOf(t, tx, models.AuditResourceAgent, fixture.agentId)
		if len(events) != baseline+1 {
			t.Fatalf("a rename wrote %d rows, want 1", len(events)-baseline)
		}
		var recorded map[string]any
		if err := json.Unmarshal(events[0].After, &recorded); err != nil {
			t.Fatalf("the recorded row is not JSON: %s", err)
		}
		if len(recorded) != 1 || recorded["name"] != "Renamed Agent" {
			t.Errorf("the row records %s, want only the name", events[0].After)
		}
	})
}

// Reordering the rules is one audit row for the agent, not one per rule.
func TestSpendingRuleReorderIsOneAuditRow(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "audit-reorder")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		categoryId := spendingCategoryIdsByName(t, tx, fixture.agentId)[finance.SpendingCategoryOther]
		ruleIds := []string{}
		for index, matchText := range []string{"first shop", "second shop", "third shop"} {
			created, err := tx.CreateSpendingRule(&models.SpendingRule{
				AgentID: fixture.agentId, MatchText: matchText, SpendingCategoryID: categoryId, RulePriority: index + 1,
			})
			if err != nil {
				t.Fatalf("CreateSpendingRule: %s", err)
			}
			ruleIds = append(ruleIds, created.ID)
		}
		baseline := len(auditEventsOf(t, tx, models.AuditResourceAgent, fixture.agentId))
		if err := tx.SetSpendingRulePriorities(fixture.agentId, map[string]int{ruleIds[0]: 3, ruleIds[1]: 1, ruleIds[2]: 2}); err != nil {
			t.Fatalf("SetSpendingRulePriorities: %s", err)
		}
		if have := len(auditEventsOf(t, tx, models.AuditResourceAgent, fixture.agentId)); have != baseline+1 {
			t.Errorf("a reorder of three rules wrote %d agent rows, want 1", have-baseline)
		}
		for _, ruleId := range ruleIds {
			for _, event := range auditEventsOf(t, tx, models.AuditResourceSpendingRule, ruleId) {
				if event.Action == models.AuditActionUpdate {
					t.Errorf("a reorder wrote an update row for rule %s", ruleId)
				}
			}
		}
	})
}

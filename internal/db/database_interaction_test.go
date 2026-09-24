package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Two runs whose providers both numbered their first call "call_1" raise
// two cards, and each is found, answered and closed on its own.
func TestCardsWithTheSameCallIdInTwoRunsAreTwoCards(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	var agentId, conversationId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "card-owner"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		for _, runId := range []string{"run-first", "run-second"} {
			if _, err := tx.CreateAgentInteraction(&models.AgentInteraction{AgentID: agent.ID, ConversationID: conversation.ID, RunID: runId, CallID: "call_1",
				InteractionKind: models.InteractionQuestion, InteractionText: "Asked in " + runId}); err != nil {
				t.Fatal(err)
			}
		}
		first, err := tx.GetAgentInteractionByCall(agent.ID, "run-first", "call_1")
		if err != nil || first == nil || first.InteractionText != "Asked in run-first" {
			t.Fatalf("the first run's card: %+v %v", first, err)
		}
		if isClaimed, err := tx.ClaimAgentInteraction(first.ID, "yes"); err != nil || !isClaimed {
			t.Fatalf("claimed: %v %v", isClaimed, err)
		}
		second, _ := tx.GetAgentInteractionByCall(agent.ID, "run-second", "call_1")
		if second == nil || second.ResolvedAt != nil {
			t.Fatalf("the second run's card is still open: %+v", second)
		}
		open, _ := tx.ListOpenAgentInteractions(conversation.ID)
		if len(open) != 1 || open[0].RunID != "run-second" {
			t.Fatalf("one card open: %+v", open)
		}
		agentId, conversationId = agent.ID, conversation.ID
	})
	// On its own: a failed insert ends the transaction it is in.
	if err := database.Transaction(func(tx db.Transaction) error {
		_, err := tx.CreateAgentInteraction(&models.AgentInteraction{AgentID: agentId, ConversationID: conversationId, RunID: "run-first", CallID: "call_1", InteractionKind: models.InteractionQuestion})
		return err
	}); err == nil {
		t.Fatal("a run raises one card per call")
	}
}

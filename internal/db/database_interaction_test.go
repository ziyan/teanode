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

// What waits for the person is counted across every conversation of their
// agent, newest first, and nothing of another agent's or already answered.
func TestWaitingCardsAreCountedAcrossConversations(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agents := []*models.Agent{}
		for _, username := range []string{"waiting-owner", "waiting-other"} {
			owner, err := tx.CreateUser(&models.User{Username: username})
			if err != nil {
				t.Fatal(err)
			}
			agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			agents = append(agents, agent)
		}
		raise := func(agent *models.Agent, runId, text string) *models.AgentInteraction {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			created, err := tx.CreateAgentInteraction(&models.AgentInteraction{AgentID: agent.ID, ConversationID: conversation.ID, RunID: runId, CallID: "call_1",
				InteractionKind: models.InteractionQuestion, InteractionText: text})
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Millisecond)
			return created
		}
		raise(agents[0], "run-old", "Older question")
		answered := raise(agents[0], "run-answered", "Answered question")
		raise(agents[0], "run-new", "Newer question")
		raise(agents[1], "run-other", "Somebody else's")
		if _, err := tx.ClaimAgentInteraction(answered.ID, "yes"); err != nil {
			t.Fatal(err)
		}
		waiting, total, err := tx.ListAgentWaitingInteractions(agents[0].ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(waiting) != 1 || waiting[0].InteractionText != "Newer question" {
			t.Fatalf("two waiting, the newest first: %d %+v", total, waiting)
		}
		if _, total, _ := tx.ListAgentWaitingInteractions(agents[0].ID, 0); total != 2 {
			t.Fatalf("counting alone still counts: %d", total)
		}
	})
}

// A deleted conversation's cards go with it, so the person is not told a
// card waits that cannot be found or answered. One left from before that
// did, with no conversation, is not counted either.
func TestCardsOfADeletedConversationAreNotWaiting(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "deleted-owner"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		deleted, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		kept, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		raise := func(conversationId, runId, text string) *models.AgentInteraction {
			created, err := tx.CreateAgentInteraction(&models.AgentInteraction{AgentID: agent.ID, ConversationID: conversationId, RunID: runId, CallID: "call_1",
				InteractionKind: models.InteractionApproval, InteractionText: text})
			if err != nil {
				t.Fatal(err)
			}
			return created
		}
		gone := raise(deleted.ID, "run-deleted", "In the deleted conversation")
		raise(kept.ID, "run-kept", "In the kept conversation")
		raise("no-such-conversation", "run-orphaned", "Left from before")

		if err := tx.DeleteAgentConversation(deleted.ID); err != nil {
			t.Fatal(err)
		}
		if found, err := tx.GetAgentInteractionByCall(agent.ID, gone.RunID, gone.CallID); err != nil || found != nil {
			t.Fatalf("the card went with its conversation: %+v %v", found, err)
		}
		waiting, total, err := tx.ListAgentWaitingInteractions(agent.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(waiting) != 1 || waiting[0].ConversationID != kept.ID {
			t.Fatalf("only the kept conversation's card waits: %d %+v", total, waiting)
		}
	})
}

// Scavenging old run conversations takes their cards too, and leaves the
// cards of a conversation it keeps.
func TestCardsOfAScavengedConversationAreNotWaiting(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "scavenged-owner"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		conversationIds := map[models.AgentConversationKind]string{}
		for _, kind := range []models.AgentConversationKind{models.AgentConversationRun, models.AgentConversationNamed} {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: kind, LastAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.CreateAgentInteraction(&models.AgentInteraction{AgentID: agent.ID, ConversationID: conversation.ID, RunID: "run-" + string(kind), CallID: "call_1",
				InteractionKind: models.InteractionQuestion, InteractionText: "Which one?"}); err != nil {
				t.Fatal(err)
			}
			conversationIds[kind] = conversation.ID
		}

		scavengedCount, err := tx.ScavengeAgentConversations(models.AgentConversationRun, time.Now().Add(time.Minute))
		if err != nil || scavengedCount != 1 {
			t.Fatalf("the run conversation is scavenged: %d %v", scavengedCount, err)
		}
		if found, err := tx.GetAgentInteractionByCall(agent.ID, "run-"+string(models.AgentConversationRun), "call_1"); err != nil || found != nil {
			t.Fatalf("the card went with its conversation: %+v %v", found, err)
		}
		waiting, total, err := tx.ListAgentWaitingInteractions(agent.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(waiting) != 1 || waiting[0].ConversationID != conversationIds[models.AgentConversationNamed] {
			t.Fatalf("only the named conversation's card waits: %d %+v", total, waiting)
		}
	})
}

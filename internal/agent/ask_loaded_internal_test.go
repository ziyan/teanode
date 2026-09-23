package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A tool the conversation called in an earlier turn is still in the round.
//
// A tool loaded through tool_search used to go back behind it when the turn
// ended. The next turn's model read its own call in the history, took the
// tool to be there, found only the tools loaded from the start, and used
// one of those for the job instead, over and over.
func TestAToolCalledEarlierInTheConversationIsStillOffered(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, rounds := offeringProvider()
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	ask := func(message string) []string {
		t.Helper()
		before := len(rounds())
		asked, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: message, Surface: "cli", Short: true})
		if err != nil {
			t.Fatalf("Ask: %s", err)
		}
		<-asked.done
		after := rounds()
		if len(after) == before {
			t.Fatal("the turn asked the model nothing")
		}
		return after[before]
	}

	// Deferred to begin with, so that the rest says something.
	if offered := toolsNamed(ask("hello")); offered["knowledge"] {
		t.Fatalf("knowledge is behind tool_search in a short round: %v", offered)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, message := range []*models.AgentMessage{
			{Role: "user", Content: "keep up with my notes"},
			{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "call_1", Name: "knowledge", Arguments: `{"action":"sources"}`}}},
			{Role: "tool", ToolCallID: "call_1", Name: "knowledge", Content: `{"sources":[]}`},
			{Role: "assistant", Content: "Allow the folder, then tell me."},
		} {
			message.ConversationID = conversation.ID
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})
	if offered := toolsNamed(ask("done, add it now")); !offered["knowledge"] {
		t.Errorf("knowledge, called earlier in the conversation, is offered in the next turn: %v", offered)
	}
}

package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A search finds a chat by what was said in it, and does not read what a
// run said, nor another agent's chats.
func TestConversationSearchReadsOnlyTheAgentsOwnChats(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var agentId string
	var side, run, theirs *models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		owner := dbtest.CreateUser(test, database, "chat-owner")
		other := dbtest.CreateUser(test, database, "other-owner")
		mine, err := tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		if err != nil {
			return err
		}
		someone, err := tx.CreateAgent(&models.Agent{UserID: other, Enabled: true, Name: "another agent"})
		if err != nil {
			return err
		}
		agentId = mine.ID
		create := func(agent string, kind models.AgentConversationKind, said string) (*models.AgentConversation, error) {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent, Kind: kind, Title: "a chat", LastAt: time.Now()})
			if err != nil {
				return nil, err
			}
			_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: said})
			return conversation, err
		}
		if side, err = create(mine.ID, models.AgentConversationNamed, "what is the tide table for saturday"); err != nil {
			return err
		}
		if run, err = create(mine.ID, models.AgentConversationRun, "the tide table was read overnight"); err != nil {
			return err
		}
		theirs, err = create(someone.ID, models.AgentConversationNamed, "their own tide table")
		return err
	}); err != nil {
		test.Fatalf("setting up: %s", err)
	}

	var found []*models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		var err error
		found, err = tx.SearchAgentConversations(agentId, "tide table", 50)
		return err
	}); err != nil {
		test.Fatalf("SearchAgentConversations: %s", err)
	}
	if len(found) != 1 || found[0].ID != side.ID {
		ids := []string{}
		for _, conversation := range found {
			ids = append(ids, conversation.ID)
		}
		test.Errorf("only the side chat is found, not the run %s or the other agent's %s: %v", run.ID, theirs.ID, ids)
	}
}

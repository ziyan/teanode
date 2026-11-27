package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The oversight queries join a conversation to its owner, leave chats out,
// bound the range on the last activity, and tell a turn in flight from one cut
// or gone quiet.
func TestOversightConversationsAreFilteredAndPaged(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	now := time.Now()
	var mainID, namedID, runID, chatID, agentID string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "oversight-owner", Name: "Oversight Owner"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		agentID = agent.ID
		make := func(kind models.AgentConversationKind, jobKind, title string, lastAt time.Time) string {
			created, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: kind, JobKind: jobKind, Title: title})
			if err != nil {
				test.Fatalf("CreateAgentConversation: %s", err)
			}
			if _, err := tx.UpdateAgentConversation(created.ID, func(each *models.AgentConversation) error {
				each.LastAt = lastAt
				return nil
			}); err != nil {
				test.Fatalf("UpdateAgentConversation: %s", err)
			}
			return created.ID
		}
		mainID = make(models.AgentConversationMain, "", "Main", now.Add(-time.Hour))
		namedID = make(models.AgentConversationNamed, "", "Named", now.Add(-72*time.Hour))
		runID = make(models.AgentConversationRun, "triage", "Run", now.Add(-3*time.Hour))
		chatID = make(models.AgentConversationChat, "", "Chat", now)
		cutAt := now
		for _, turn := range []*models.AgentTurn{
			{RunID: "oversight-live", ConversationID: mainID, Instance: "one", StartedAt: now, HeartbeatAt: now},
			{RunID: "oversight-cut", ConversationID: namedID, Instance: "one", StartedAt: now, HeartbeatAt: now, CutAt: &cutAt},
		} {
			if err := tx.PutAgentTurn(turn); err != nil {
				test.Fatalf("PutAgentTurn: %s", err)
			}
		}
	})

	idsOf := func(filter *db.OversightFilter, options *db.Options) []string {
		test.Helper()
		var ids []string
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			found, err := tx.ListOversightConversations(filter, options)
			if err != nil {
				test.Fatalf("ListOversightConversations: %s", err)
			}
			for _, each := range found {
				if each.OwnerUsername != "oversight-owner" || each.OwnerName != "Oversight Owner" {
					test.Errorf("the owner is %q %q", each.OwnerUsername, each.OwnerName)
				}
				if each.IsRunning != (each.Conversation.ID == mainID) {
					test.Errorf("%s is running: %v", each.Conversation.ID, each.IsRunning)
				}
				ids = append(ids, each.Conversation.ID)
			}
			count, err := tx.CountOversightConversations(filter)
			if err != nil {
				test.Fatalf("CountOversightConversations: %s", err)
			}
			if options == nil && int(count) != len(ids) {
				test.Errorf("counted %d, listed %d", count, len(ids))
			}
		})
		return ids
	}
	same := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for index := range got {
			if got[index] != want[index] {
				return false
			}
		}
		return true
	}
	liveAfter := now.Add(-90 * time.Second)
	since, until := now.Add(-24*time.Hour), now.Add(-2*time.Hour)

	if got := idsOf(&db.OversightFilter{LiveAfter: liveAfter}, nil); !same(got, mainID, runID, namedID) {
		test.Errorf("everything but chats, newest first: %v", got)
	}
	if got := idsOf(&db.OversightFilter{Kinds: []string{"chat"}}, nil); len(got) != 0 {
		test.Errorf("asking for chats lists %v", got)
	}
	if got := idsOf(&db.OversightFilter{Kinds: []string{"named", "triage"}}, nil); !same(got, runID, namedID) {
		test.Errorf("named and triage: %v", got)
	}
	if got := idsOf(&db.OversightFilter{Since: &since, Until: &until}, nil); !same(got, runID) {
		test.Errorf("the range: %v", got)
	}
	if got := idsOf(&db.OversightFilter{IsRunningOnly: true, LiveAfter: liveAfter}, nil); !same(got, mainID) {
		test.Errorf("running only: %v", got)
	}
	if got := idsOf(&db.OversightFilter{AgentID: agentID, LiveAfter: liveAfter}, &db.Options{Limit: 1, Offset: 1}); !same(got, runID) {
		test.Errorf("the second page: %v", got)
	}
	if got := idsOf(&db.OversightFilter{AgentID: "another-agent"}, nil); len(got) != 0 {
		test.Errorf("another agent: %v", got)
	}
	_ = chatID
}

// A failed note is a failed turn, newest first, but not in a chat between
// people.
func TestFailedAgentTurnsAreListedFromTheirNotes(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var agentID, mainID string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "failed-owner", Name: "Failed Owner"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		agentID = agent.ID
		main, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, Title: "Main"})
		if err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		mainID = main.ID
		chat, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationChat, Title: "Chat"})
		if err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		for _, note := range []*models.AgentMessage{
			models.NewAgentNote(main.ID, models.NoteFailed, "first failure"),
			models.NewAgentNote(main.ID, models.NoteStopped, ""),
			models.NewAgentNote(chat.ID, models.NoteFailed, "in a chat"),
			models.NewAgentNote(main.ID, models.NoteFailed, "second failure"),
		} {
			if _, err := tx.AppendAgentMessage(note); err != nil {
				test.Fatalf("AppendAgentMessage: %s", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		filter := &db.FailedTurnFilter{AgentID: agentID}
		turns, err := tx.ListFailedAgentTurns(filter, &db.Options{Limit: 10})
		if err != nil || len(turns) != 2 {
			test.Fatalf("failed turns: %v %v", turns, err)
		}
		if turns[0].ErrorText != "second failure" || turns[1].ErrorText != "first failure" || turns[0].ConversationID != mainID ||
			turns[0].OwnerUsername != "failed-owner" || turns[0].AgentID != agentID || turns[0].ConversationKind != models.AgentConversationMain {
			test.Errorf("the failed turns are %+v %+v", turns[0], turns[1])
		}
		count, err := tx.CountFailedAgentTurns(filter)
		if err != nil || count != 2 {
			test.Errorf("counted %d: %v", count, err)
		}
		future := time.Now().Add(time.Hour)
		if count, err := tx.CountFailedAgentTurns(&db.FailedTurnFilter{Since: &future}); err != nil || count != 0 {
			test.Errorf("after them counted %d: %v", count, err)
		}
	})
}

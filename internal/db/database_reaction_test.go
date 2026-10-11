package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// reactionWorld is an agent with a conversation of two messages, and
// another agent with one of its own.
type reactionWorld struct {
	agentId, otherAgentId  string
	conversation, theirs   *models.AgentConversation
	asked, answered, their *models.AgentMessage
}

func newReactionWorld(test *testing.T, database db.Database) *reactionWorld {
	test.Helper()
	world := &reactionWorld{}
	owner := dbtest.CreateUser(test, database, "reaction-owner")
	other := dbtest.CreateUser(test, database, "reaction-other")
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		mine, err := tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		someone, err := tx.CreateAgent(&models.Agent{UserID: other, Enabled: true, Name: "another agent"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		world.agentId, world.otherAgentId = mine.ID, someone.ID
		if world.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: mine.ID, Kind: models.AgentConversationNamed, Title: "a chat", LastAt: time.Now()}); err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		if world.asked, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.conversation.ID, Role: "user", Content: "thank you"}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
		if world.answered, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.conversation.ID, Role: "assistant", Content: "any time"}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
		if world.theirs, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: someone.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		if world.their, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.theirs.ID, Role: "user", Content: "hello"}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
	})
	return world
}

// reactionsOf is a message's reactions as a page of its conversation
// reads them.
func reactionsOf(test *testing.T, database db.Database, conversationId, messageId string) []models.AgentMessageReaction {
	test.Helper()
	var found []models.AgentMessageReaction
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(conversationId, nil)
		if err != nil {
			test.Fatalf("ListAgentMessages: %s", err)
		}
		for _, message := range messages {
			if message.ID == messageId {
				found = message.Reactions
			}
		}
	})
	return found
}

// A reactor has one reaction on a message: reacting again replaces it,
// the same emoji again changes nothing, and an empty one takes it away,
// leaving the other reactor's where it was.
func TestAReactionIsSetReplacedAndRemoved(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	world := newReactionWorld(test, database)

	set := func(reactorKind models.AgentReactorKind, reactionEmoji string) bool {
		test.Helper()
		var isChanged bool
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			var err error
			if isChanged, err = tx.SetAgentMessageReaction(world.agentId, world.conversation.ID, world.answered.ID, reactorKind, reactionEmoji); err != nil {
				test.Fatalf("SetAgentMessageReaction: %s", err)
			}
		})
		return isChanged
	}

	if !set(models.ReactorPerson, "👍") || !set(models.ReactorAgent, "🙏") {
		test.Fatal("a first reaction is a change")
	}
	reactions := reactionsOf(test, database, world.conversation.ID, world.answered.ID)
	if len(reactions) != 2 || reactions[0].ReactorKind != models.ReactorPerson || reactions[0].Emoji != "👍" || reactions[1].Emoji != "🙏" || reactions[0].ReactedAt.IsZero() {
		test.Fatalf("both reactions, the person's first: %+v", reactions)
	}
	if set(models.ReactorPerson, "👍") {
		test.Error("the same emoji again is no change")
	}
	if !set(models.ReactorPerson, "❤️") {
		test.Error("another emoji replaces it")
	}
	reactions = reactionsOf(test, database, world.conversation.ID, world.answered.ID)
	if len(reactions) != 2 || reactions[0].Emoji != "❤️" {
		test.Fatalf("replaced, not added: %+v", reactions)
	}
	if !set(models.ReactorPerson, "") {
		test.Error("taking it away is a change")
	}
	if set(models.ReactorPerson, "") {
		test.Error("taking away what is not there is none")
	}
	reactions = reactionsOf(test, database, world.conversation.ID, world.answered.ID)
	if len(reactions) != 1 || reactions[0].ReactorKind != models.ReactorAgent {
		test.Fatalf("only the agent's is left: %+v", reactions)
	}
	if others := reactionsOf(test, database, world.conversation.ID, world.asked.ID); len(others) != 0 {
		test.Errorf("a message nobody reacted to has none: %+v", others)
	}

	// One message read alone carries its reactions too.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		message, err := tx.GetAgentMessage(world.answered.ID)
		if err != nil || message == nil || len(message.Reactions) != 1 || message.ConversationID != world.conversation.ID {
			test.Fatalf("GetAgentMessage: %+v %v", message, err)
		}
		inConversation, err := tx.GetAgentConversationMessage(world.conversation.ID, world.answered.ID)
		if err != nil || inConversation == nil || len(inConversation.Reactions) != 1 {
			test.Fatalf("GetAgentConversationMessage: %+v %v", inConversation, err)
		}
	})
}

// A reaction is only ever put on a message of the agent's own
// conversation, named with the conversation it is in.
func TestAReactionStaysInsideTheAgentsOwnConversations(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	world := newReactionWorld(test, database)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.SetAgentMessageReaction(world.agentId, world.theirs.ID, world.their.ID, models.ReactorPerson, "👍"); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("another agent's message: %v", err)
		}
		if _, err := tx.SetAgentMessageReaction(world.agentId, world.conversation.ID, world.their.ID, models.ReactorPerson, "👍"); !errors.Is(err, db.ErrNotFound) {
			test.Errorf("a message of another conversation: %v", err)
		}
		if _, err := tx.SetAgentMessageReaction(world.agentId, world.conversation.ID, world.answered.ID, "stranger", "👍"); err == nil {
			test.Error("a reactor that is neither the person nor the agent")
		}
	})
	if reactions := reactionsOf(test, database, world.theirs.ID, world.their.ID); len(reactions) != 0 {
		test.Fatalf("nothing was put on their message: %+v", reactions)
	}
}

// Reactions go with their message, and with their conversation.
func TestReactionsGoWithTheirMessageAndConversation(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	world := newReactionWorld(test, database)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		for _, messageId := range []string{world.asked.ID, world.answered.ID} {
			if _, err := tx.SetAgentMessageReaction(world.agentId, world.conversation.ID, messageId, models.ReactorPerson, "👍"); err != nil {
				test.Fatalf("SetAgentMessageReaction: %s", err)
			}
		}
	})
	count := func() string {
		return dbtest.QueryString(test, database, `SELECT count(*)::text FROM "agent_message_reaction" WHERE "conversation_id" = '`+world.conversation.ID+`'`)
	}
	if count() != "2" {
		test.Fatalf("two reactions stored, not %s", count())
	}
	dbtest.Exec(test, database, `DELETE FROM "agent_message" WHERE "id" = '`+world.asked.ID+`'`)
	if count() != "1" {
		test.Fatalf("the deleted message's reaction went with it: %s left", count())
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if err := tx.DeleteAgentConversation(world.conversation.ID); err != nil {
			test.Fatalf("DeleteAgentConversation: %s", err)
		}
	})
	if count() != "0" {
		test.Fatalf("the deleted conversation's reactions went with it: %s left", count())
	}
}

// The person's latest message is theirs, not a turn the agent took on its
// own in their shape.
func TestTheLatestPersonMessageLeavesOutTheAgentsOwnTurns(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	world := newReactionWorld(test, database)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.conversation.ID, Role: "user", Content: models.OwnTurnMarkers[0] + " looking again"}); err != nil {
			test.Fatalf("AppendAgentMessage: %s", err)
		}
		latest, err := tx.LastAgentPersonMessageID(world.conversation.ID)
		if err != nil || latest != world.asked.ID {
			test.Fatalf("LastAgentPersonMessageID = %q, %v; want the person's %q", latest, err, world.asked.ID)
		}
		empty, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.agentId, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		if latest, err := tx.LastAgentPersonMessageID(empty.ID); err != nil || latest != "" {
			test.Fatalf("a conversation nobody wrote in: %q %v", latest, err)
		}
	})
}

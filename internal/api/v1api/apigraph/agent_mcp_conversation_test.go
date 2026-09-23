package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Each program asks in a conversation of its own, the same one every time.
//
// A program's questions used to land in the person's main conversation,
// looking like messages they had typed. A program that names no conversation
// now finds its own, made the first time and found again after, titled with
// its name and kept apart from any other program's.
func TestEachProgramAsksInAConversationOfItsOwn(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	owner := dbtest.CreateUser(test, database, "program-owner")
	var person *models.Agent
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		person, err = tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		return err
	}); err != nil {
		test.Fatalf("creating an agent: %s", err)
	}
	conversationOf := func(caller mcpCaller) *models.AgentConversation {
		test.Helper()
		var found *models.AgentConversation
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			found, err = programConversation(tx, person, caller)
			return err
		}); err != nil {
			test.Fatalf("programConversation: %s", err)
		}
		return found
	}

	assistant := mcpCaller{name: "A coding assistant", isProgramHeld: true, programID: "01assistantregistration000"}
	first := conversationOf(assistant)
	again := conversationOf(assistant)
	if first == nil || again == nil || first.ID != again.ID {
		test.Fatalf("the same program got two conversations: %v and %v", first, again)
	}
	if first.Kind != models.AgentConversationNamed || first.Title != "A coding assistant" || first.TitledBy != programTitledBy {
		test.Errorf("the program's conversation is %+v", first)
	}

	other := conversationOf(mcpCaller{name: "An editor", programID: "01editortoken0000000000000"})
	if other.ID == first.ID {
		test.Error("two programs share a conversation")
	}
}

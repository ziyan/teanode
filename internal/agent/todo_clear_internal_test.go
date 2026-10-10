package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A turn that ends with every step of the task list done clears the list;
// one that ends with a step still open leaves it for the next turn.
func TestATurnEndingWithEveryStepDoneClearsTheTaskList(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	var finished, unfinished *models.AgentConversation
	done := time.Now()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "todo-owner"})
		if err != nil {
			t.Fatal(err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, conversation := range []**models.AgentConversation{&finished, &unfinished} {
			if *conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationNamed}); err != nil {
				t.Fatal(err)
			}
		}
		for _, todo := range []*models.AgentTodo{
			{ConversationID: finished.ID, Text: "read the timetable", DoneAt: &done},
			{ConversationID: finished.ID, Text: "pick a train", DoneAt: &done},
			{ConversationID: unfinished.ID, Text: "read the timetable", DoneAt: &done},
			{ConversationID: unfinished.ID, Text: "book the seat"},
		} {
			if _, err := tx.CreateAgentTodo(todo); err != nil {
				t.Fatal(err)
			}
		}
	})
	worker := &Agent{settings: &Settings{Database: database}}
	for _, conversation := range []*models.AgentConversation{finished, unfinished} {
		run := &AskRun{agent: worker, settings: &AskSettings{Conversation: conversation}}
		run.clearFinishedTodos()
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if todos, err := tx.ListAgentTodos(finished.ID); err != nil || len(todos) != 0 {
			t.Errorf("a list with every step done was kept: %d, %v", len(todos), err)
		}
		if todos, err := tx.ListAgentTodos(unfinished.ID); err != nil || len(todos) != 2 {
			t.Errorf("a list with a step open was not kept whole: %d, %v", len(todos), err)
		}
	})
}

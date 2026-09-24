package todo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/todo"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// todoRun is a run in one conversation, with a database and nothing else.
type todoRun struct {
	tools.Run
	database     db.Database
	conversation *models.AgentConversation
}

func (self *todoRun) Database() db.Database                   { return self.database }
func (self *todoRun) Conversation() *models.AgentConversation { return self.conversation }

type todoAnswer struct {
	Todo []struct {
		ID     string `json:"id"`
		Text   string `json:"text"`
		IsDone bool   `json:"isDone"`
	} `json:"todo"`
	OpenCount      int `json:"openCount"`
	DoneCount      int `json:"doneCount"`
	SucceededCount int `json:"succeededCount"`
	FailedCount    int `json:"failedCount"`
	PrunedCount    int `json:"prunedCount"`
	Results        []struct {
		Op           string `json:"op"`
		ID           string `json:"id"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"results"`
}

// The agent keeps its steps in one call: adds them, finishes one, renames
// another; a change it gets wrong is refused on its own line and the rest
// still happen; prune clears what is finished.
func TestTheAgentKeepsItsStepsInBatches(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	run := &todoRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "todo-owner"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, Title: "planning"}); err != nil {
			t.Fatal(err)
		}
	})
	var todo *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "todo" {
			todo = each
		}
	}
	call := func(arguments string) todoAnswer {
		t.Helper()
		result, err := todo.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		var answer todoAnswer
		if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
			t.Fatalf("%s: %s", result.Content, err)
		}
		return answer
	}

	added := call(`{"action":"batch","items":[{"op":"add","text":"Read the notes"},{"op":"add","text":"Draft   the\nplan"},{"op":"add","text":""}]}`)
	if added.SucceededCount != 2 || added.FailedCount != 1 || len(added.Todo) != 2 || added.Todo[1].Text != "Draft the plan" {
		t.Fatalf("added %+v", added)
	}
	first, second := added.Todo[0].ID, added.Todo[1].ID
	changed := call(`{"action":"batch","items":[{"op":"complete","id":"` + first + `"},{"op":"update","id":"` + second + `","text":"Draft the plan for Monday"},{"op":"complete","id":"no-such-step"}]}`)
	if changed.SucceededCount != 2 || changed.FailedCount != 1 || changed.DoneCount != 1 || changed.OpenCount != 1 || changed.Todo[1].Text != "Draft the plan for Monday" {
		t.Fatalf("changed %+v", changed)
	}
	if changed.Results[2].ErrorMessage == "" {
		t.Fatalf("a step that is not there was not refused: %+v", changed.Results)
	}
	// The list comes back to the agent every round, with a reminder to keep
	// it true.
	overlay := todo.Overlay(tools.WithRun(context.Background(), run))
	if !strings.Contains(overlay, "Draft the plan for Monday") || !strings.Contains(overlay, "Complete each step") || strings.Contains(overlay, "Read the notes") {
		t.Fatalf("the overlay is %q", overlay)
	}
	// Deleting a step that is not there says so.
	if missing := call(`{"action":"batch","items":[{"op":"delete","id":"no-such-step"}]}`); missing.FailedCount != 1 {
		t.Fatalf("deleting a missing step was reported done: %+v", missing)
	}
	pruned := call(`{"action":"prune"}`)
	if pruned.PrunedCount != 1 || len(pruned.Todo) != 1 || pruned.Todo[0].ID != second {
		t.Fatalf("pruned %+v", pruned)
	}
}

package goal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/goal"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// goalRun is a run with nobody present, in a goal's own conversation.
type goalRun struct {
	tools.Run
	database     db.Database
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *goalRun) Database() db.Database                   { return self.database }
func (self *goalRun) Agent() *models.Agent                    { return self.agent }
func (self *goalRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *goalRun) Headless() bool                          { return true }
func (self *goalRun) Configuration() *config.Configuration    { return config.Default() }

// A goal's note is kept whole, however long.
func TestAGoalKeepsItsWholeNote(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &goalRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatal(err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatal(err)
		}
		run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: run.agent.ID, Kind: models.AgentConversationGoal, Goal: "keep the example tidy", GoalState: models.GoalWorking, LastAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	var tool *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "goal" {
			tool = each
		}
	}
	if tool == nil {
		t.Fatal("the goal tool is registered")
	}
	ctx := tools.WithRun(context.Background(), run)

	words := strings.Repeat("a sentence about where things stand. ", 40)
	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(`{"action":"note","status":` + mustQuote(words) + `,"minutes":5}`)})
	if err != nil {
		t.Fatalf("note: %s", err)
	}
	var answer struct {
		GoalStatus string `json:"goalStatus"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil || answer.GoalStatus != strings.TrimSpace(words) {
		t.Fatalf("the note is kept whole: %d of %d characters, %v", len(answer.GoalStatus), len(strings.TrimSpace(words)), err)
	}
}

func mustQuote(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

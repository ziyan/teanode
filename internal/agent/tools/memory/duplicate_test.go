package memory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRun knows what a memory means, as a run with an embedding model
// does: everything about boats is near everything else about boats.
type fakeRun struct {
	tools.Run
	database     db.Database
	agent        *models.Agent
	conversation *models.AgentConversation
	twins        []*models.AgentMemory
	noted        []string
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }
func (self *fakeRun) Offered() []*tools.Tool                  { return nil }

func (self *fakeRun) NoteMemory(_ context.Context, memory *models.AgentMemory) []*models.AgentMemory {
	self.noted = append(self.noted, memory.ID)
	return self.twins
}

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

// A memory that means what one already kept means is written, and said
// to be a likely copy of it; one that means something else is not.
func TestAddingSaysWhenItIsAlreadyRemembered(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	add := func(arguments string) map[string]any {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			t.Fatalf("add: %s", err)
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer
	}

	// Nothing is near it yet.
	first := add(`{"action":"add","title":"Kittiwake","content":"the neighbour's boat, repainted every spring"}`)
	if first["warning"] != nil {
		t.Fatalf("the first of its kind is not a copy of anything: %v", first)
	}
	kept, _ := first["id"].(string)
	if kept == "" {
		t.Fatalf("the memory was kept: %v", first)
	}
	if len(run.noted) != 1 || run.noted[0] != kept {
		t.Fatalf("what it means is worked out as it is written: %v", run.noted)
	}

	// Now one is.
	run.twins = []*models.AgentMemory{{ID: kept, Title: "Kittiwake", Content: "the neighbour's boat, repainted every spring"}}
	second := add(`{"action":"add","title":"The boat next door","content":"Kittiwake gets a coat of paint each spring"}`)
	next, _ := second["do_this_next"].(string)
	if !strings.Contains(next, kept) || !strings.Contains(next, "update") || !strings.Contains(next, "delete") {
		t.Fatalf("it should say, with both ids, to merge them: %v", second)
	}
	already, _ := second["already_remembered"].([]any)
	if len(already) != 1 || already[0].(map[string]any)["id"] != kept {
		t.Fatalf("and say which one: %v", second["already_remembered"])
	}
	// Written all the same: refusing would only be answered with the
	// same call again.
	if second["id"] == nil || second["id"] == kept {
		t.Fatalf("the second is still kept, as its own memory: %v", second)
	}
}

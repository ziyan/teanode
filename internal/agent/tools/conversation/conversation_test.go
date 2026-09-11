package conversation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/conversation"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type fakeRun struct {
	tools.Run
	database     db.Database
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }
func (self *fakeRun) Offered() []*tools.Tool                  { return nil }

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

// The agent finds what was said in another conversation, reads it, and
// cannot read one that is not its own.
func TestConversationSearchesReadsAndRefuses(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	var elsewhere, stranger *models.AgentConversation
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
		if elsewhere, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationNamed, Title: "The regatta", LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "user", Content: "the moorings are booked for the regatta"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "system", Content: "the prompt, which is not read back"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		// Somebody else's.
		other, err := tx.CreateUser(&models.User{Username: "bob", Name: "Bob Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		theirs, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true, Name: "Robin"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if stranger, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: theirs.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "conversation")
	call := func(arguments string) (map[string]any, *tools.Result, error) {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, result, nil
	}

	answer, result, err := call(`{"action":"search","query":"moorings"}`)
	if err != nil {
		t.Fatalf("search: %s", err)
	}
	if !result.Untrusted {
		t.Fatal("what was said is data, not instructions")
	}
	listed, _ := answer["conversations"].([]any)
	if len(listed) != 1 {
		t.Fatalf("the conversation that said it: %v", answer)
	}
	if first := listed[0].(map[string]any); first["conversation_id"] != elsewhere.ID || first["title"] != "The regatta" {
		t.Fatalf("named and identified: %v", first)
	}

	read, _, err := call(`{"action":"read","conversation_id":"` + elsewhere.ID + `"}`)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	said, _ := read["messages"].([]any)
	if len(said) != 1 {
		t.Fatalf("the prompt is not read back, only what was said: %v", said)
	}
	if !strings.Contains(said[0].(map[string]any)["said"].(string), "moorings") {
		t.Fatalf("what was said: %v", said[0])
	}

	if listed, _, err := call(`{"action":"list"}`); err != nil || len(listed["conversations"].([]any)) != 2 {
		t.Fatalf("both of this agent's own: %v %v", listed, err)
	}
	if _, _, err := call(`{"action":"read","conversation_id":"` + stranger.ID + `"}`); err == nil {
		t.Fatal("somebody else's conversation is not there to read")
	}
	if _, _, err := call(`{"action":"search"}`); err == nil {
		t.Fatal("searching needs words")
	}
	if _, _, err := call(`{"action":"burn"}`); err == nil {
		t.Fatal("an unknown action is an error")
	}
}
